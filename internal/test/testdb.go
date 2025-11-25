package test

import (
	"database/sql"
	"os"
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

	// Run migrations inline
	migrations := []string{
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

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			t.Fatalf("failed to run migration: %v", err)
		}
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// ResetTransactions truncates the transactions table for clean test state.
func ResetTransactions(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("TRUNCATE TABLE transactions"); err != nil {
		t.Fatalf("failed to truncate transactions: %v", err)
	}
}
