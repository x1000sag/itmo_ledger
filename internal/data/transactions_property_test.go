package data

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
	"simple-ledger.itmo.ru/internal/test"
)

func TestBalanceInvariantProperty(t *testing.T) {
	db := test.SetupTestDB(t)
	test.ResetTransactions(t, db)
	m := BalanceModel{DB: db}
	r := rand.New(rand.NewSource(42)) // Fixed seed for reproducibility

	for seq := 0; seq < 50; seq++ {
		// Reset transactions between sequences
		test.ResetTransactions(t, db)

		user := uuid.New()
		total := 0

		// Random deposits (10 per sequence)
		for i := 0; i < 10; i++ {
			amt := r.Intn(200) + 1                       // 1-200
			lifetime := r.Intn(40) + 1                   // 1-40 days
			_, err := m.AddBonusPoints(user, amt, lifetime)
			if err != nil {
				t.Fatalf("seq %d, deposit %d err: %v", seq, i, err)
			}
			total += amt
		}

		// Withdrawals not exceeding total (random partial withdrawal)
		withdrawTarget := r.Intn(total/2 + 1)
		if withdrawTarget > 0 {
			if err := m.WithdrawBonusPoints(user, withdrawTarget); err != nil {
				t.Fatalf("seq %d withdraw err: %v", seq, err)
			}
			total -= withdrawTarget
		}

		// Verify balance consistency
		bal, _, err := m.GetBalanceWithExpiration(user)
		if err != nil {
			t.Fatalf("seq %d GetBalanceWithExpiration err: %v", seq, err)
		}

		if bal < 0 {
			t.Fatalf("seq %d: negative balance: %d", seq, bal)
		}

		if bal != total {
			t.Fatalf("seq %d: balance mismatch: expected %d, got %d", seq, total, bal)
		}
	}
}
