package data

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"simple-ledger.itmo.ru/internal/test"
)

func TestAddBonusPoints(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()
	test.ResetTransactions(t, db)

	model := BalanceModel{DB: db}
	userId := uuid.New()

	transaction, err := model.AddBonusPoints(userId, 100, 30)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}

	if transaction.Id == uuid.Nil {
		t.Error("expected transaction ID to be set")
	}
	if transaction.UserId != userId {
		t.Errorf("expected user_id %v, got %v", userId, transaction.UserId)
	}
	if transaction.Amount != 100 {
		t.Errorf("expected amount 100, got %d", transaction.Amount)
	}
	if transaction.RemainingAmount != 100 {
		t.Errorf("expected remaining_amount 100, got %d", transaction.RemainingAmount)
	}
	if transaction.CreatedAt.IsZero() {
		t.Error("expected created_at to be set")
	}
	if transaction.ExpiresAt.IsZero() {
		t.Error("expected expires_at to be set")
	}
	// Verify expires_at is approximately 30 days from now
	expectedExpiry := time.Now().Add(30 * 24 * time.Hour)
	diff := transaction.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Hour || diff > time.Hour {
		t.Errorf("expected expires_at around %v, got %v", expectedExpiry, transaction.ExpiresAt)
	}
}

func TestGetBalanceWithExpiration(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()
	test.ResetTransactions(t, db)

	model := BalanceModel{DB: db}
	userId := uuid.New()

	// Add multiple transactions with different lifetimes
	_, err := model.AddBonusPoints(userId, 100, 10)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}
	_, err = model.AddBonusPoints(userId, 200, 20)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}
	_, err = model.AddBonusPoints(userId, 50, 365)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}

	balance, expirations, err := model.GetBalanceWithExpiration(userId)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration failed: %v", err)
	}

	// Total balance should be 350
	if balance != 350 {
		t.Errorf("expected balance 350, got %d", balance)
	}

	// Expirations should contain entries for 10 and 20 days (within 30 days window)
	// The 365-day transaction should not appear in expirations
	if len(expirations) < 2 {
		t.Errorf("expected at least 2 expiration entries, got %d", len(expirations))
	}
}

func TestWithdrawFIFO(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()
	test.ResetTransactions(t, db)

	model := BalanceModel{DB: db}
	userId := uuid.New()

	// Add transactions with different expiration times
	// First: 100 points expiring in 5 days
	_, err := model.AddBonusPoints(userId, 100, 5)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}

	// Second: 200 points expiring in 30 days
	_, err = model.AddBonusPoints(userId, 200, 30)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}

	// Withdraw 150 points - should take 100 from first (oldest expiry) and 50 from second
	err = model.WithdrawBonusPoints(userId, 150)
	if err != nil {
		t.Fatalf("WithdrawBonusPoints failed: %v", err)
	}

	// Check remaining balance
	balance, _, err := model.GetBalanceWithExpiration(userId)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration failed: %v", err)
	}

	// Should have 150 remaining (300 - 150)
	if balance != 150 {
		t.Errorf("expected balance 150 after FIFO withdrawal, got %d", balance)
	}
}

func TestWithdrawInsufficient(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()
	test.ResetTransactions(t, db)

	model := BalanceModel{DB: db}
	userId := uuid.New()

	// Add 100 points
	_, err := model.AddBonusPoints(userId, 100, 30)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}

	// Try to withdraw 150 points - should fail
	err = model.WithdrawBonusPoints(userId, 150)
	if err != ErrInsufficientFunds {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}

	// Verify balance unchanged
	balance, _, err := model.GetBalanceWithExpiration(userId)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration failed: %v", err)
	}
	if balance != 100 {
		t.Errorf("expected balance 100 after failed withdrawal, got %d", balance)
	}
}

func TestExpiredExclusion(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()
	test.ResetTransactions(t, db)

	model := BalanceModel{DB: db}
	userId := uuid.New()

	// Insert a transaction that is already expired using raw SQL
	_, err := db.Exec(`
		INSERT INTO transactions (user_id, amount, expires_at, remaining_amount)
		VALUES ($1, $2, NOW() - INTERVAL '1 day', $3)
	`, userId, 100, 100)
	if err != nil {
		t.Fatalf("failed to insert expired transaction: %v", err)
	}

	// Add valid transaction
	_, err = model.AddBonusPoints(userId, 50, 30)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}

	// Check balance - expired points should not be counted
	balance, _, err := model.GetBalanceWithExpiration(userId)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration failed: %v", err)
	}

	// Only the 50 valid points should be counted
	if balance != 50 {
		t.Errorf("expected balance 50 (excluding expired), got %d", balance)
	}

	// Try to withdraw more than valid balance - should fail
	err = model.WithdrawBonusPoints(userId, 100)
	if err != ErrInsufficientFunds {
		t.Errorf("expected ErrInsufficientFunds when trying to use expired points, got %v", err)
	}
}

func TestConcurrentWithdrawals(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()
	test.ResetTransactions(t, db)

	model := BalanceModel{DB: db}
	userId := uuid.New()

	// Add 1000 points
	_, err := model.AddBonusPoints(userId, 1000, 365)
	if err != nil {
		t.Fatalf("AddBonusPoints failed: %v", err)
	}

	// Run 10 concurrent withdrawals of 100 points each
	const numGoroutines = 10
	const withdrawAmount = 100
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := model.WithdrawBonusPoints(userId, withdrawAmount)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Count errors
	errCount := 0
	for range errors {
		errCount++
	}

	// All withdrawals should succeed (1000 / 100 = 10)
	if errCount != 0 {
		t.Errorf("expected all withdrawals to succeed, got %d errors", errCount)
	}

	// Check final balance - should be 0
	balance, _, err := model.GetBalanceWithExpiration(userId)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration failed: %v", err)
	}

	if balance != 0 {
		t.Errorf("expected final balance 0 after concurrent withdrawals, got %d", balance)
	}
}
