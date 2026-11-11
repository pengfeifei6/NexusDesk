// Package taskqueue transports minimal job identities; PostgreSQL owns execution state.
package taskqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/pengfeifei6/NexusDesk/internal/jobs"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/redis/go-redis/v9"
)

const Queue = "nexusdesk:knowledge:v1"
const TaskType = "knowledge:ingest:v1"

var errDelivery = errors.New("job delivery temporarily unavailable")

type Client struct {
	client *asynq.Client
	ping   *redis.Client
}

func options(c config.AsyncConfig) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{Addr: c.RedisAddr, Password: c.RedisPassword, DB: c.RedisDB, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, PoolSize: 8}
}
func NewClient(c config.AsyncConfig) *Client {
	return &Client{client: asynq.NewClient(options(c)), ping: redis.NewClient(&redis.Options{Addr: c.RedisAddr, Password: c.RedisPassword, DB: c.RedisDB, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxRetries: -1, PoolSize: 1})}
}
func (c *Client) Close() { _ = c.client.Close(); _ = c.ping.Close() }
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if c.ping.Ping(ctx).Err() != nil {
		return errDelivery
	}
	return nil
}
func taskID(p jobs.Payload, seq int64) string {
	return fmt.Sprintf("job-%d-run-%d-dispatch-%d", p.JobID, p.RunVersion, seq)
}
func (c *Client) Enqueue(ctx context.Context, p jobs.Payload, seq int64) error {
	b, err := json.Marshal(p)
	if err != nil || seq <= 0 {
		return errDelivery
	}
	if _, err = jobs.ParsePayload(b); err != nil {
		return errDelivery
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err = c.client.EnqueueContext(ctx, asynq.NewTask(TaskType, b), asynq.Queue(Queue), asynq.TaskID(taskID(p, seq)), asynq.MaxRetry(2), asynq.Timeout(180*time.Second), asynq.Retention(time.Hour))
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		return errDelivery
	}
	return nil
}
func retryDelay(n int, _ error, _ *asynq.Task) time.Duration {
	if n == 0 {
		return 5 * time.Second
	}
	return 30 * time.Second
}
func handler(execute func(context.Context, jobs.Payload) error) asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		if t.Type() != TaskType {
			return asynq.SkipRetry
		}
		p, err := jobs.ParsePayload(t.Payload())
		if err != nil {
			return asynq.SkipRetry
		}
		if execute(ctx, p) != nil {
			return errDelivery
		}
		return nil
	}
}

type Server struct {
	server *asynq.Server
	mux    *asynq.ServeMux
}

func NewServer(ctx context.Context, c config.AsyncConfig, execute func(context.Context, jobs.Payload) error) *Server {
	mux := asynq.NewServeMux()
	mux.Handle(TaskType, handler(execute))
	server := asynq.NewServer(options(c), asynq.Config{Concurrency: c.Concurrency, Queues: map[string]int{Queue: 1}, BaseContext: func() context.Context { return ctx }, ShutdownTimeout: 10 * time.Second, RetryDelayFunc: retryDelay, TaskCheckInterval: time.Second, DelayedTaskCheckInterval: time.Second, Logger: safeLogger{}, LogLevel: asynq.WarnLevel})
	return &Server{server: server, mux: mux}
}
func (s *Server) Start() error {
	if s.server.Start(s.mux) != nil {
		return errDelivery
	}
	return nil
}
func (s *Server) Stop()     { s.server.Stop() }
func (s *Server) Shutdown() { s.server.Shutdown() }

// Third-party errors may contain connection details. Never forward their text or task payloads.
type safeLogger struct{}

func (safeLogger) Debug(...interface{}) {}
func (safeLogger) Info(...interface{})  {}
func (safeLogger) Warn(...interface{})  { slog.Warn("queue runtime warning") }
func (safeLogger) Error(...interface{}) { slog.Error("queue runtime operation failed") }
func (safeLogger) Fatal(...interface{}) { slog.Error("queue runtime fatal error") }
