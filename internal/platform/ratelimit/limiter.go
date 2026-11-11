// Package ratelimit implements a Redis-backed first-request fixed window.
package ratelimit

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	client          *redis.Client
	requests        int
	window, timeout time.Duration
}

func New(client *redis.Client, requests int, window, timeout time.Duration) *Limiter {
	return &Limiter{client: client, requests: requests, window: window, timeout: timeout}
}

// Check consumes one attempt only when within quota. All instances share the
// same Redis clock/window. Denials never extend expiry or increase the counter.
func (l *Limiter) Check(parent context.Context, org, user int64) (bool, time.Duration, error) {
	if org <= 0 || user <= 0 || l == nil || l.client == nil || l.requests < 1 || l.window < time.Millisecond || l.timeout <= 0 {
		return false, 0, errors.New("rate limit unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, l.timeout)
	defer cancel()
	result, err := l.client.Eval(ctx, script, []string{"nexusdesk:ratelimit:v1:ai:" + strconv.FormatInt(org, 10) + ":" + strconv.FormatInt(user, 10)}, l.requests, l.window.Milliseconds()).Int64Slice()
	if err != nil || len(result) != 2 || result[0] < 0 || result[0] > 1 || result[1] < 1 {
		return false, 0, errors.New("rate limit unavailable")
	}
	return result[0] == 1, time.Duration(result[1]) * time.Millisecond, nil
}

const script = `
local count = redis.call('GET', KEYS[1])
if not count then
 redis.call('SET', KEYS[1], 1, 'PX', ARGV[2])
 return {1, tonumber(ARGV[2])}
end
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then
 if ttl == -1 then return redis.error_reply('rate limit invalid expiry') end
 redis.call('SET', KEYS[1], 1, 'PX', ARGV[2])
 return {1, tonumber(ARGV[2])}
end
if tonumber(count) >= tonumber(ARGV[1]) then return {0, ttl} end
redis.call('INCR', KEYS[1])
return {1, ttl}
`
