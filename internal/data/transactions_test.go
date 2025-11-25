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

	m := BalanceModel{DB: db}
	user := uuid.New()

	trx, err := m.AddBonusPoints(user, 150, 15)
	if err != nil {
		t.Fatalf("AddBonusPoints error: %v", err)
	}

	if trx.Amount != 150 {
		t.Errorf("expected amount 150, got %d", trx.Amount)
	}

	if trx.RemainingAmount != 150 {
		t.Errorf("expected remaining_amount 150, got %d", trx.RemainingAmount)
	}

	if trx.UserID != user {
		t.Errorf("expected user_id %v, got %v", user, trx.UserID)
	}

	// Check expiration is approximately 15 days from now (at least 14 days)
	minExpiry := time.Now().Add(14 * 24 * time.Hour)
	maxExpiry := time.Now().Add(16 * 24 * time.Hour)
	if trx.ExpiresAt.Before(minExpiry) || trx.ExpiresAt.After(maxExpiry) {
		t.Errorf("expires_at %v not within expected range [%v, %v]", trx.ExpiresAt, minExpiry, maxExpiry)
	}
}

func TestGetBalanceWithExpiration(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()

	m := BalanceModel{DB: db}
	user := uuid.New()

	// Add two deposits with different lifetimes
	_, err := m.AddBonusPoints(user, 100, 5)
	if err != nil {
		t.Fatalf("AddBonusPoints (5 days): %v", err)
	}

	_, err = m.AddBonusPoints(user, 50, 25)
	if err != nil {
		t.Fatalf("AddBonusPoints (25 days): %v", err)
	}

	bal, exp, err := m.GetBalanceWithExpiration(user)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration: %v", err)
	}

	if bal != 150 {
		t.Errorf("expected balance 150, got %d", bal)
	}

	if len(exp) == 0 {
		t.Errorf("expected expirations not empty")
	}

	// Both expirations should be within 30 day window
	for date, amount := range exp {
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Errorf("failed to parse expiration date %s: %v", date, err)
			continue
		}
		if parsed.Before(time.Now()) {
			t.Errorf("expiration date %s is in the past", date)
		}
		if amount <= 0 {
			t.Errorf("expiration amount should be positive, got %d", amount)
		}
	}
}

func TestWithdrawFIFO(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()

	m := BalanceModel{DB: db}
	user := uuid.New()

	// Add two deposits: first expires in 3 days, second in 10 days
	_, err := m.AddBonusPoints(user, 100, 3) // expires first
	if err != nil {
		t.Fatalf("AddBonusPoints (3 days): %v", err)
	}

	_, err = m.AddBonusPoints(user, 200, 10) // expires later
	if err != nil {
		t.Fatalf("AddBonusPoints (10 days): %v", err)
	}

	// Withdraw 120: should take 100 from first (3-day) and 20 from second (10-day)
	if err := m.WithdrawBonusPoints(user, 120); err != nil {
		t.Fatalf("WithdrawBonusPoints: %v", err)
	}

	bal, _, err := m.GetBalanceWithExpiration(user)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration after withdrawal: %v", err)
	}

	// 100 + 200 - 120 = 180
	if bal != 180 {
		t.Errorf("expected balance 180, got %d", bal)
	}
}

func TestWithdrawInsufficient(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()

	m := BalanceModel{DB: db}
	user := uuid.New()

	_, err := m.AddBonusPoints(user, 70, 5)
	if err != nil {
		t.Fatalf("AddBonusPoints: %v", err)
	}

	// Try to withdraw more than available
	err = m.WithdrawBonusPoints(user, 100)
	if err != ErrInsufficientFunds {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}

	// Balance should remain unchanged
	bal, _, err := m.GetBalanceWithExpiration(user)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration: %v", err)
	}

	if bal != 70 {
		t.Errorf("expected balance unchanged at 70, got %d", bal)
	}
}

func TestExpiredExclusion(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()

	m := BalanceModel{DB: db}
	user := uuid.New()

	// Add active transaction
	_, err := m.AddBonusPoints(user, 200, 10)
	if err != nil {
		t.Fatalf("AddBonusPoints: %v", err)
	}

	// Insert expired transaction directly
	_, err = db.Exec(`
		INSERT INTO transactions (user_id, amount, expires_at, remaining_amount) 
		VALUES ($1, 100, NOW() - INTERVAL '1 day', 100)
	`, user)
	if err != nil {
		t.Fatalf("insert expired transaction: %v", err)
	}

	bal, exp, err := m.GetBalanceWithExpiration(user)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration: %v", err)
	}

	// Should only count active transaction
	if bal != 200 {
		t.Errorf("expected balance 200 (expired excluded), got %d", bal)
	}

	// Ensure no past dates in expirations
	for date := range exp {
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Errorf("failed to parse expiration date %s: %v", date, err)
			continue
		}
		if parsed.Before(time.Now().Truncate(24 * time.Hour)) {
			t.Errorf("found past expiration date in map: %s", date)
		}
	}
}

func TestConcurrentWithdrawals(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()

	m := BalanceModel{DB: db}
	user := uuid.New()

	// Add initial balance
	_, err := m.AddBonusPoints(user, 300, 30)
	if err != nil {
		t.Fatalf("AddBonusPoints: %v", err)
	}

	numGoroutines := 3
	withdrawAmount := 100

	var wg sync.WaitGroup
	errs := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- m.WithdrawBonusPoints(user, withdrawAmount)
		}()
	}

	wg.Wait()
	close(errs)

	// All withdrawals should succeed (3 x 100 = 300, exactly the balance)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent withdraw error: %v", err)
		}
	}

	// Final balance should be 0
	bal, _, err := m.GetBalanceWithExpiration(user)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration: %v", err)
	}

	if bal != 0 {
		t.Errorf("expected balance 0 after concurrent withdrawals, got %d", bal)
	}
}

func TestConcurrentWithdrawalsExceedBalance(t *testing.T) {
	db := test.SetupTestDB(t)
	defer db.Close()

	m := BalanceModel{DB: db}
	user := uuid.New()

	// Add initial balance of 200
	_, err := m.AddBonusPoints(user, 200, 30)
	if err != nil {
		t.Fatalf("AddBonusPoints: %v", err)
	}

	numGoroutines := 3
	withdrawAmount := 100

	var wg sync.WaitGroup
	errs := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- m.WithdrawBonusPoints(user, withdrawAmount)
		}()
	}

	wg.Wait()
	close(errs)

	// Count successes and insufficient funds errors
	successCount := 0
	insufficientCount := 0
	for err := range errs {
		if err == nil {
			successCount++
		} else if err == ErrInsufficientFunds {
			insufficientCount++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// With 200 balance and 3x100 withdrawals, exactly 2 should succeed
	if successCount != 2 {
		t.Errorf("expected 2 successful withdrawals, got %d", successCount)
	}

	if insufficientCount != 1 {
		t.Errorf("expected 1 insufficient funds error, got %d", insufficientCount)
	}

	// Final balance should be 0
	bal, _, err := m.GetBalanceWithExpiration(user)
	if err != nil {
		t.Fatalf("GetBalanceWithExpiration: %v", err)
	}

	if bal != 0 {
		t.Errorf("expected balance 0, got %d", bal)
	}
}
