// Package collector provides PostgreSQL and system metrics collection.
package collector

import (
	"context"
	"database/sql"
	"fmt"
)

// Config holds collector configuration.
type Config struct {
	DB      *sql.DB
	DataDir string // PostgreSQL data directory for disk metrics
}

// Collector collects metrics from PostgreSQL and the system.
type Collector struct {
	config Config
}

// New creates a new Collector.
func New(config Config) *Collector {
	if config.DataDir == "" {
		config.DataDir = "/var/lib/postgresql"
	}
	return &Collector{config: config}
}

// Collect collects all metrics (PostgreSQL + system).
func (c *Collector) Collect(ctx context.Context) (map[string]float64, error) {
	metrics := make(map[string]float64)

	// Collect PostgreSQL metrics if DB is configured
	if c.config.DB != nil {
		pgMetrics, err := c.CollectPostgres(ctx)
		if err != nil {
			return nil, fmt.Errorf("postgres metrics: %w", err)
		}
		for k, v := range pgMetrics {
			metrics[k] = v
		}

		// Also collect extended metrics
		extMetrics, err := c.CollectPostgresExtended(ctx)
		if err != nil {
			// Extended metrics are optional, just log and continue
			_ = err
		} else {
			for k, v := range extMetrics {
				metrics[k] = v
			}
		}
	}

	// Collect system metrics
	sysMetrics, err := c.CollectSystem()
	if err != nil {
		return nil, fmt.Errorf("system metrics: %w", err)
	}
	for k, v := range sysMetrics {
		metrics[k] = v
	}

	// Collect disk metrics
	diskMetrics, err := c.CollectDisk()
	if err != nil {
		// Disk metrics are optional
		_ = err
	} else {
		for k, v := range diskMetrics {
			metrics[k] = v
		}
	}

	return metrics, nil
}

// CollectPostgres collects essential PostgreSQL metrics.
func (c *Collector) CollectPostgres(ctx context.Context) (map[string]float64, error) {
	metrics := make(map[string]float64)

	// Active connections
	var active float64
	err := c.config.DB.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_stat_activity WHERE state != 'idle' AND pid != pg_backend_pid()").Scan(&active)
	if err != nil {
		return nil, fmt.Errorf("pg_connections_active: %w", err)
	}
	metrics["pg_connections_active"] = active

	// Idle connections
	var idle float64
	err = c.config.DB.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_stat_activity WHERE state = 'idle'").Scan(&idle)
	if err != nil {
		return nil, fmt.Errorf("pg_connections_idle: %w", err)
	}
	metrics["pg_connections_idle"] = idle

	// Total connections
	var total float64
	err = c.config.DB.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_stat_activity WHERE pid != pg_backend_pid()").Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("pg_connections_total: %w", err)
	}
	metrics["pg_connections_total"] = total

	// Max connections
	var maxConn float64
	err = c.config.DB.QueryRowContext(ctx,
		"SELECT setting::float FROM pg_settings WHERE name = 'max_connections'").Scan(&maxConn)
	if err != nil {
		return nil, fmt.Errorf("pg_connections_max: %w", err)
	}
	metrics["pg_connections_max"] = maxConn

	// Available connections (derived)
	metrics["pg_connections_available"] = maxConn - total

	// Database size (sum of all non-template databases)
	var dbSize float64
	err = c.config.DB.QueryRowContext(ctx,
		"SELECT COALESCE(sum(pg_database_size(datname)), 0) FROM pg_database WHERE datistemplate = false").Scan(&dbSize)
	if err != nil {
		return nil, fmt.Errorf("pg_database_size_bytes: %w", err)
	}
	metrics["pg_database_size_bytes"] = dbSize

	return metrics, nil
}

// CollectPostgresExtended collects extended PostgreSQL metrics.
func (c *Collector) CollectPostgresExtended(ctx context.Context) (map[string]float64, error) {
	metrics := make(map[string]float64)

	// Uptime
	var uptime float64
	err := c.config.DB.QueryRowContext(ctx,
		"SELECT EXTRACT(EPOCH FROM (now() - pg_postmaster_start_time()))").Scan(&uptime)
	if err != nil {
		return nil, fmt.Errorf("pg_uptime_seconds: %w", err)
	}
	metrics["pg_uptime_seconds"] = uptime

	// Cache hit ratio
	var cacheHitRatio float64
	err = c.config.DB.QueryRowContext(ctx,
		`SELECT CASE WHEN sum(blks_hit) + sum(blks_read) = 0 THEN 1.0
		 ELSE sum(blks_hit)::float / (sum(blks_hit) + sum(blks_read)) END
		 FROM pg_stat_database`).Scan(&cacheHitRatio)
	if err != nil {
		return nil, fmt.Errorf("pg_cache_hit_ratio: %w", err)
	}
	metrics["pg_cache_hit_ratio"] = cacheHitRatio

	// Deadlocks total
	var deadlocks float64
	err = c.config.DB.QueryRowContext(ctx,
		"SELECT COALESCE(sum(deadlocks), 0) FROM pg_stat_database").Scan(&deadlocks)
	if err != nil {
		return nil, fmt.Errorf("pg_deadlocks_total: %w", err)
	}
	metrics["pg_deadlocks_total"] = deadlocks

	// Oldest transaction age
	var oldestTxn sql.NullFloat64
	err = c.config.DB.QueryRowContext(ctx,
		`SELECT EXTRACT(EPOCH FROM (now() - min(xact_start)))
		 FROM pg_stat_activity WHERE xact_start IS NOT NULL`).Scan(&oldestTxn)
	if err != nil {
		return nil, fmt.Errorf("pg_oldest_transaction_age_seconds: %w", err)
	}
	if oldestTxn.Valid {
		metrics["pg_oldest_transaction_age_seconds"] = oldestTxn.Float64
	} else {
		metrics["pg_oldest_transaction_age_seconds"] = 0
	}

	// Oldest query age
	var oldestQuery sql.NullFloat64
	err = c.config.DB.QueryRowContext(ctx,
		`SELECT EXTRACT(EPOCH FROM (now() - min(query_start)))
		 FROM pg_stat_activity WHERE state = 'active' AND pid != pg_backend_pid()`).Scan(&oldestQuery)
	if err != nil {
		return nil, fmt.Errorf("pg_oldest_query_age_seconds: %w", err)
	}
	if oldestQuery.Valid {
		metrics["pg_oldest_query_age_seconds"] = oldestQuery.Float64
	} else {
		metrics["pg_oldest_query_age_seconds"] = 0
	}

	// Waiting queries (lock waits)
	var waiting float64
	err = c.config.DB.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'").Scan(&waiting)
	if err != nil {
		return nil, fmt.Errorf("pg_waiting_queries: %w", err)
	}
	metrics["pg_waiting_queries"] = waiting

	return metrics, nil
}

// CollectSystem and CollectDisk are implemented in platform-specific files:
// - system_linux.go
// - system_darwin.go
