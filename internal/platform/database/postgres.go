// Package database manages the PostgreSQL connection pool lifecycle.
package database

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
)

// Open creates a pool and verifies a real connection within five seconds.
// The caller owns the returned pool and must close it after HTTP shutdown.
func Open(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	connectionURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.Name,
	}
	query := url.Values{}
	query.Set("sslmode", cfg.SSLMode)
	connectionURL.RawQuery = query.Encode()
	poolConfig, err := pgxpool.ParseConfig(connectionURL.String())
	if err != nil {
		// pgx parse errors can contain the full connection URL. Never forward it.
		return nil, errors.New("configure PostgreSQL: invalid connection settings")
	}
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	startupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(startupCtx, poolConfig)
	if err != nil {
		return nil, safeConnectionError(err)
	}
	if err := pool.Ping(startupCtx); err != nil {
		pool.Close()
		return nil, safeConnectionError(err)
	}
	return pool, nil
}

func safeConnectionError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("connect PostgreSQL: %w", context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("connect PostgreSQL: %w", context.DeadlineExceeded)
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "28P01", "28000":
			return errors.New("connect PostgreSQL: authentication failed; check DB_USER and DB_PASSWORD")
		case "3D000":
			return errors.New("connect PostgreSQL: database does not exist; check DB_NAME")
		}
	}
	return errors.New("connect PostgreSQL: connection failed; check DB_HOST, DB_PORT, credentials and DB_SSLMODE")
}
