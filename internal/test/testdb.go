package test

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

// SetupTestDB creates a connection to the test database and runs migrations.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set, skipping database test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	// Run migrations - use IF NOT EXISTS and ignore "already exists" errors
	setup := []string{
		`CREATE TABLE IF NOT EXISTS balances (
			id uuid PRIMARY KEY,
			updated_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
			amount int
		)`,
		`CREATE TABLE IF NOT EXISTS transactions (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id uuid NOT NULL,
			amount int NOT NULL CHECK (amount > 0),
			created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
			expires_at timestamp(0) with time zone NOT NULL,
			remaining_amount int NOT NULL CHECK (remaining_amount >= 0),
			CONSTRAINT remaining_amount_check CHECK (remaining_amount <= amount)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON transactions(user_id)`,
	}

	for _, m := range setup {
		if _, err := db.Exec(m); err != nil {
			// Ignore "already exists" or duplicate constraint errors
			if strings.Contains(err.Error(), "already exists") ||
				strings.Contains(err.Error(), "duplicate key") {
				continue
			}
			t.Fatalf("failed to run setup: %v", err)
		}
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}
