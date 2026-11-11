// Package cache stores disposable authorized list snapshots. PostgreSQL owns
// generations, so writes never depend on Redis or post-commit invalidation.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/redis/go-redis/v9"
)

type Options struct {
	TTL, Timeout time.Duration
	MaxBytes     int
}
type RevisionReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Cache struct {
	client                                  *redis.Client
	revisions                               RevisionReader
	options                                 Options
	hits, misses, failures, corrupt, stores atomic.Uint64
}
type Stats struct{ Hits, Misses, Failures, Corrupt, Stores uint64 }

// NewClient is shared by list caching and the AI limiter. It deliberately does
// not ping: an unavailable Redis must not prevent API startup or database reads.
func NewClient(cfg config.AsyncConfig, timeout time.Duration) *redis.Client {
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	return redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, DB: cfg.RedisDB, Password: cfg.RedisPassword,
		DialTimeout: timeout, ReadTimeout: timeout, WriteTimeout: timeout, PoolTimeout: timeout,
		ContextTimeoutEnabled: true, MaxRetries: -1, PoolSize: 10, MaxActiveConns: 10})
}
func New(client *redis.Client, revisions RevisionReader, options Options) *Cache {
	if options.TTL <= 0 {
		options.TTL = 30 * time.Second
	}
	if options.Timeout <= 0 {
		options.Timeout = 100 * time.Millisecond
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 1 << 20
	}
	return &Cache{client: client, revisions: revisions, options: options}
}
func (c *Cache) Stats() Stats {
	return Stats{c.hits.Load(), c.misses.Load(), c.failures.Load(), c.corrupt.Load(), c.stores.Load()}
}
func (c *Cache) ttl() time.Duration {
	return c.options.TTL + time.Duration(rand.Int64N(int64(c.options.TTL/5)+1))
}
func Key(org int64, domain string, revision int64, filters any) string {
	b, _ := json.Marshal(filters)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("nexusdesk:cache:v1:%d:%s:%d:active-members-all:created_at-desc-id-desc:%s", org, domain, revision, hex.EncodeToString(sum[:]))
}
func (c *Cache) Revision(ctx context.Context, org int64, domain string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var version int64
	err := c.revisions.QueryRow(ctx, "SELECT COALESCE((SELECT version FROM cache_revisions WHERE org_id=$1 AND domain=$2),0)", org, domain).Scan(&version)
	return version, err
}

type envelope struct {
	OrgID int64           `json:"org_id"`
	Data  json.RawMessage `json:"data"`
}

// listData starts each JSON field occurrence with fresh elements. Decoding a
// repeated field into an existing []T can otherwise inherit IDs and content
// from earlier elements, unlike the baseline last-RawMessage behavior.
type listData[T any] []T

func (data *listData[T]) UnmarshalJSON(b []byte) error {
	var fresh []T
	if err := json.Unmarshal(b, &fresh); err != nil {
		return err
	}
	*data = fresh
	return nil
}

// decodeList avoids the RawMessage copy while preserving fresh last-field
// replacement, corruption fallback and valid non-nil empty arrays.
func decodeList[T any](b []byte, org int64, maxBytes int) ([]T, bool) {
	var e struct {
		OrgID int64       `json:"org_id"`
		Data  listData[T] `json:"data"`
	}
	if len(b) > maxBytes || json.Unmarshal(b, &e) != nil || e.OrgID != org || e.Data == nil {
		return nil, false
	}
	return e.Data, true
}

func getList[T any](ctx context.Context, c *Cache, key string, org int64) ([]T, bool) {
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	// GETRANGE bounds the bytes read even when Redis contains an oversized value.
	b, err := c.client.GetRange(ctx, key, 0, int64(c.options.MaxBytes)).Bytes()
	if err != nil {
		c.failures.Add(1)
		return nil, false
	}
	if len(b) == 0 {
		c.misses.Add(1)
		return nil, false
	}
	items, ok := decodeList[T](b, org, c.options.MaxBytes)
	if !ok {
		c.corrupt.Add(1)
		return nil, false
	}
	return items, true
}
func (c *Cache) set(ctx context.Context, key string, org int64, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	b, err := json.Marshal(envelope{OrgID: org, Data: data})
	if err != nil || len(b) > c.options.MaxBytes {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	if c.client.Set(ctx, key, b, c.ttl()).Err() != nil {
		c.failures.Add(1)
		return
	}
	c.stores.Add(1)
}

// List must be called only after current database membership authorization and
// filter validation. The captured generation is used for both GET and SET; a
// concurrent commit can never publish a delayed old result under its new key.
func List[T any](ctx context.Context, c *Cache, org int64, domain string, filters any, load func() ([]T, error), valid func(T) bool) ([]T, error) {
	if c == nil || c.client == nil || c.revisions == nil {
		return load()
	}
	revision, err := c.Revision(ctx, org, domain)
	if err != nil {
		c.failures.Add(1)
		return load()
	}
	key := Key(org, domain, revision, filters)
	items, hit := getList[T](ctx, c, key, org)
	if hit {
		good := items != nil
		for _, item := range items {
			if !valid(item) {
				good = false
				break
			}
		}
		if good {
			c.hits.Add(1)
			return items, nil
		}
		c.corrupt.Add(1)
	}
	items, err = load()
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []T{}
	}
	for _, item := range items {
		if !valid(item) {
			return items, nil
		}
	}
	c.set(ctx, key, org, items)
	return items, nil
}
