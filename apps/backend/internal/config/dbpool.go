package config

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envDBMaxOpenConns    = "DB_MAX_OPEN_CONNS"
	envDBMaxIdleConns    = "DB_MAX_IDLE_CONNS"
	envDBConnMaxLifetime = "DB_CONN_MAX_LIFETIME"
	envDBConnMaxIdleTime = "DB_CONN_MAX_IDLE_TIME"

	defaultDBMaxOpenConns    = 20
	defaultDBMaxIdleConns    = 10
	defaultDBConnMaxLifetime = 30 * time.Minute
	defaultDBConnMaxIdleTime = 5 * time.Minute
)

// DBPool bounds the Postgres connection pool. database/sql defaults to unlimited open
// connections, so a burst of requests plus three pollers could exhaust the server's
// max_connections and fail unrelated queries mid-operation.
//
//   - DB_MAX_OPEN_CONNS (default 20): hard cap; keep it below the database's per-role limit.
//   - DB_MAX_IDLE_CONNS (default 10): warm connections kept between bursts.
//   - DB_CONN_MAX_LIFETIME (default 30m): recycle so a failover or pooler restart drains.
//   - DB_CONN_MAX_IDLE_TIME (default 5m): close idle connections before a proxy drops them.
type DBPool struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Apply sets the limits on db.
func (p DBPool) Apply(db *sql.DB) {
	db.SetMaxOpenConns(p.MaxOpenConns)
	db.SetMaxIdleConns(p.MaxIdleConns)
	db.SetConnMaxLifetime(p.ConnMaxLifetime)
	db.SetConnMaxIdleTime(p.ConnMaxIdleTime)
}

func loadDBPool() (DBPool, error) {
	pool := DBPool{
		MaxOpenConns:    defaultDBMaxOpenConns,
		MaxIdleConns:    defaultDBMaxIdleConns,
		ConnMaxLifetime: defaultDBConnMaxLifetime,
		ConnMaxIdleTime: defaultDBConnMaxIdleTime,
	}
	var err error
	if pool.MaxOpenConns, err = positiveIntEnv(envDBMaxOpenConns, pool.MaxOpenConns); err != nil {
		return DBPool{}, err
	}
	if pool.MaxIdleConns, err = positiveIntEnv(envDBMaxIdleConns, pool.MaxIdleConns); err != nil {
		return DBPool{}, err
	}
	if pool.MaxIdleConns > pool.MaxOpenConns {
		pool.MaxIdleConns = pool.MaxOpenConns
	}
	if pool.ConnMaxLifetime, err = positiveDurationEnv(envDBConnMaxLifetime, pool.ConnMaxLifetime); err != nil {
		return DBPool{}, err
	}
	if pool.ConnMaxIdleTime, err = positiveDurationEnv(envDBConnMaxIdleTime, pool.ConnMaxIdleTime); err != nil {
		return DBPool{}, err
	}
	return pool, nil
}

func positiveIntEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return value, nil
}

func positiveDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 30m, got %q", name, raw)
	}
	return value, nil
}
