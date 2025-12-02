package collector

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// skipIfNoPostgres skips the test if PostgreSQL is not available
func skipIfNoPostgres(t *testing.T) *sql.DB {
	t.Helper()

	dsn := "host=localhost port=5432 user=postgres password=postgres dbname=postgres sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("PostgreSQL not available: %v", err)
	}

	return db
}

func TestCollector_CollectPostgresMetrics(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()

	collector := New(Config{
		DB: db,
	})

	ctx := context.Background()
	metrics, err := collector.CollectPostgres(ctx)
	if err != nil {
		t.Fatalf("CollectPostgres() error = %v", err)
	}

	// Check essential metrics are present
	essentialMetrics := []string{
		"pg_connections_active",
		"pg_connections_idle",
		"pg_connections_total",
		"pg_connections_max",
		"pg_database_size_bytes",
	}

	for _, name := range essentialMetrics {
		if _, ok := metrics[name]; !ok {
			t.Errorf("missing essential metric: %s", name)
		}
	}

	// Validate some metrics have reasonable values
	if metrics["pg_connections_max"] <= 0 {
		t.Errorf("pg_connections_max = %v, want > 0", metrics["pg_connections_max"])
	}

	if metrics["pg_connections_total"] < 0 {
		t.Errorf("pg_connections_total = %v, want >= 0", metrics["pg_connections_total"])
	}
}

func TestCollector_CollectExtendedMetrics(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()

	collector := New(Config{
		DB: db,
	})

	ctx := context.Background()
	metrics, err := collector.CollectPostgresExtended(ctx)
	if err != nil {
		t.Fatalf("CollectPostgresExtended() error = %v", err)
	}

	// Check extended metrics
	extendedMetrics := []string{
		"pg_uptime_seconds",
		"pg_cache_hit_ratio",
		"pg_deadlocks_total",
	}

	for _, name := range extendedMetrics {
		if _, ok := metrics[name]; !ok {
			t.Errorf("missing extended metric: %s", name)
		}
	}

	// Uptime should be positive
	if metrics["pg_uptime_seconds"] <= 0 {
		t.Errorf("pg_uptime_seconds = %v, want > 0", metrics["pg_uptime_seconds"])
	}

	// Cache hit ratio should be between 0 and 1
	if metrics["pg_cache_hit_ratio"] < 0 || metrics["pg_cache_hit_ratio"] > 1 {
		t.Errorf("pg_cache_hit_ratio = %v, want 0-1", metrics["pg_cache_hit_ratio"])
	}
}

func TestCollector_CollectAll(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()

	collector := New(Config{
		DB: db,
	})

	ctx := context.Background()
	metrics, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Should have PostgreSQL metrics
	if _, ok := metrics["pg_connections_total"]; !ok {
		t.Error("missing pg_connections_total in Collect()")
	}

	// Should have system metrics
	if _, ok := metrics["system_memory_total_bytes"]; !ok {
		t.Error("missing system_memory_total_bytes in Collect()")
	}

	if _, ok := metrics["system_load_1m"]; !ok {
		t.Error("missing system_load_1m in Collect()")
	}
}

func TestCollector_SystemMetrics(t *testing.T) {
	collector := New(Config{})

	metrics, err := collector.CollectSystem()
	if err != nil {
		t.Fatalf("CollectSystem() error = %v", err)
	}

	// Check system metrics
	systemMetrics := []string{
		"system_memory_total_bytes",
		"system_memory_available_bytes",
		"system_load_1m",
		"system_load_5m",
		"system_load_15m",
		"system_cpu_count",
	}

	for _, name := range systemMetrics {
		if _, ok := metrics[name]; !ok {
			t.Errorf("missing system metric: %s", name)
		}
	}

	// Memory should be positive
	if metrics["system_memory_total_bytes"] <= 0 {
		t.Errorf("system_memory_total_bytes = %v, want > 0", metrics["system_memory_total_bytes"])
	}

	// CPU count should be positive
	if metrics["system_cpu_count"] <= 0 {
		t.Errorf("system_cpu_count = %v, want > 0", metrics["system_cpu_count"])
	}
}

func TestCollector_DiskMetrics(t *testing.T) {
	collector := New(Config{
		DataDir: "/",
	})

	metrics, err := collector.CollectDisk()
	if err != nil {
		t.Fatalf("CollectDisk() error = %v", err)
	}

	// Check disk metrics
	diskMetrics := []string{
		"system_disk_total_bytes",
		"system_disk_used_bytes",
		"system_disk_available_bytes",
		"system_disk_used_percent",
	}

	for _, name := range diskMetrics {
		if _, ok := metrics[name]; !ok {
			t.Errorf("missing disk metric: %s", name)
		}
	}

	// Disk total should be positive
	if metrics["system_disk_total_bytes"] <= 0 {
		t.Errorf("system_disk_total_bytes = %v, want > 0", metrics["system_disk_total_bytes"])
	}

	// Disk used percent should be 0-100
	if metrics["system_disk_used_percent"] < 0 || metrics["system_disk_used_percent"] > 100 {
		t.Errorf("system_disk_used_percent = %v, want 0-100", metrics["system_disk_used_percent"])
	}
}
