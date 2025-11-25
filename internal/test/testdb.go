package test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/lib/pq"
)

// SetupTestDB creates a database connection for tests using TEST_DATABASE_DSN env var.
// It applies migrations and returns the database connection.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set, skipping database tests")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	if err := applyMigrations(db); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	return db
}

// applyMigrations reads and applies migrations from the migrations directory.
// Falls back to inline SQL if migration file is not found.
func applyMigrations(db *sql.DB) error {
	// Try to read migration file from migrations directory
	migrationPath := filepath.Join("migrations", "000002_create_transactions_table.up.sql")

	// Look for migrations in various possible locations
	possiblePaths := []string{
		migrationPath,
		filepath.Join("..", "..", "migrations", "000002_create_transactions_table.up.sql"),
		filepath.Join("..", "..", "..", "migrations", "000002_create_transactions_table.up.sql"),
	}

	var migrationSQL string
	for _, path := range possiblePaths {
		content, err := os.ReadFile(path)
		if err == nil {
			migrationSQL = string(content)
			break
		}
	}

	// Fallback to inline SQL if file not found
	if migrationSQL == "" {
		migrationSQL = `
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
`
	}

	_, err := db.Exec(migrationSQL)
	return err
}

// ResetTransactions clears all data from the transactions table for test isolation.
func ResetTransactions(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec("DELETE FROM transactions")
	if err != nil {
		t.Fatalf("failed to reset transactions table: %v", err)
	}
}
