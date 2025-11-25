package test

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	_ "github.com/lib/pq"
)

const defaultDSN = "postgres://user:pass@localhost:5433/ledger?sslmode=disable"

var (
	migrationsApplied bool
	migrationsMu      sync.Mutex
)

// SetupTestDB opens a connection to the test database and applies migrations.
// DSN can be configured via TEST_DATABASE_DSN environment variable.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	if err = db.Ping(); err != nil {
		t.Fatalf("failed to ping database (ensure PostgreSQL is running and accessible via %s): %v", dsn, err)
	}

	applyMigrations(t, db)
	return db
}

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()

	migrationsMu.Lock()
	defer migrationsMu.Unlock()

	if migrationsApplied {
		return
	}

	// Find migrations directory relative to test file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get current file path")
	}

	// Navigate from internal/test to project root
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	migrationsDir := filepath.Join(projectRoot, "migrations")

	// Apply balances migration
	balancesMigration := filepath.Join(migrationsDir, "000001_create_balance_table.up.sql")
	if sqlBytes, err := os.ReadFile(balancesMigration); err == nil {
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			// Ignore if table already exists
			if !strings.Contains(err.Error(), "already exists") {
				t.Logf("warning: balances migration: %v", err)
			}
		}
	}

	// Apply transactions migration with idempotent statements
	transactionsMigration := filepath.Join(migrationsDir, "000002_create_transactions_table.up.sql")
	sqlBytes, err := os.ReadFile(transactionsMigration)
	if err != nil {
		// Fallback inline SQL if migration file not found
		sqlBytes = []byte(`
			CREATE TABLE IF NOT EXISTS transactions (
				id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
				user_id uuid NOT NULL,
				amount int NOT NULL CHECK (amount > 0),
				created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
				expires_at timestamp(0) with time zone NOT NULL,
				remaining_amount int NOT NULL CHECK (remaining_amount >= 0),
				CONSTRAINT remaining_amount_check CHECK (remaining_amount <= amount)
			);
			CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON transactions(user_id);
			CREATE INDEX IF NOT EXISTS idx_transactions_user_expires ON transactions(user_id, expires_at) WHERE remaining_amount > 0;
		`)
	}

	if _, err := db.Exec(string(sqlBytes)); err != nil {
		// Ignore if already exists
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "duplicate key") {
			t.Fatalf("failed to apply transactions migration: %v", err)
		}
	}

	migrationsApplied = true
}

// ResetTransactions truncates the transactions table for test isolation.
func ResetTransactions(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("TRUNCATE transactions"); err != nil {
		t.Fatalf("failed to truncate transactions: %v", err)
	}
}
