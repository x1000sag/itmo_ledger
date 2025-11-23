package data

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"time"
)

type Balance struct {
	Id        uuid.UUID `json:"id"`
	Amount    int       `json:"amount"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Transaction struct {
	Id              uuid.UUID `json:"id"`
	UserId          uuid.UUID `json:"user_id"`
	Amount          int       `json:"amount"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	RemainingAmount int       `json:"remaining_amount"`
}

type BalanceModel struct {
	DB *sql.DB
}

// AddBonusPoints adds bonus points for a user with an expiration date
func (m BalanceModel) AddBonusPoints(userId uuid.UUID, amount int, lifetimeDays int) (*Transaction, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	transaction := &Transaction{
		UserId:          userId,
		Amount:          amount,
		RemainingAmount: amount,
	}

	query := `
		INSERT INTO transactions (user_id, amount, expires_at, remaining_amount)
		VALUES ($1, $2, NOW() + $3 * INTERVAL '1 day', $4)
		RETURNING id, created_at, expires_at`

	err := m.DB.QueryRowContext(ctx, query, userId, amount, lifetimeDays, amount).Scan(
		&transaction.Id,
		&transaction.CreatedAt,
		&transaction.ExpiresAt,
	)

	return transaction, err
}

func (m BalanceModel) Insert(balance *Balance) error {
	query := `
		INSERT INTO balances (id, amount)
		VALUES ($1, $2)
		RETURNING id, updated_at, amount`
	args := []any{balance.Id, balance.Amount}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return m.DB.QueryRowContext(ctx, query, args...).Scan(&balance.Id, &balance.UpdatedAt, &balance.Amount)
}

// GetBalanceWithExpiration returns the current balance and upcoming expirations
func (m BalanceModel) GetBalanceWithExpiration(userId uuid.UUID) (int, map[string]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Get total balance
	var totalBalance int
	query := `
		SELECT COALESCE(SUM(remaining_amount), 0)
		FROM transactions
		WHERE user_id = $1 AND expires_at > NOW() AND remaining_amount > 0`

	err := m.DB.QueryRowContext(ctx, query, userId).Scan(&totalBalance)
	if err != nil {
		return 0, nil, err
	}

	// Get expirations for the next 30 days grouped by date
	expirations := make(map[string]int)
	expirationQuery := `
		SELECT DATE(expires_at) as expiry_date, SUM(remaining_amount) as expiring_amount
		FROM transactions
		WHERE user_id = $1 
			AND expires_at > NOW() 
			AND expires_at <= NOW() + INTERVAL '30 days'
			AND remaining_amount > 0
		GROUP BY DATE(expires_at)
		ORDER BY DATE(expires_at)`

	rows, err := m.DB.QueryContext(ctx, expirationQuery, userId)
	if err != nil {
		return totalBalance, expirations, nil // Return balance even if expiration query fails
	}
	defer rows.Close()

	for rows.Next() {
		var expiryDate time.Time
		var expiringAmount int
		if err := rows.Scan(&expiryDate, &expiringAmount); err != nil {
			continue
		}
		expirations[expiryDate.Format("2006-01-02")] = expiringAmount
	}

	return totalBalance, expirations, nil
}

func (m BalanceModel) Get(id uuid.UUID) (*Balance, error) {
	balance := new(Balance)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		SELECT id, updated_at, amount
		FROM balances
		WHERE id = $1`
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&balance.Id,
		&balance.UpdatedAt,
		&balance.Amount,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}

	return balance, nil
}

// WithdrawBonusPoints withdraws bonus points using FIFO (oldest first) with proper locking
func (m BalanceModel) WithdrawBonusPoints(userId uuid.UUID, amount int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a transaction
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock and get available transactions ordered by expiration date (FIFO)
	query := `
		SELECT id, remaining_amount
		FROM transactions
		WHERE user_id = $1 
			AND expires_at > NOW() 
			AND remaining_amount > 0
		ORDER BY expires_at ASC, created_at ASC
		FOR UPDATE`

	rows, err := tx.QueryContext(ctx, query, userId)
	if err != nil {
		return err
	}
	defer rows.Close()

	type txRow struct {
		id              uuid.UUID
		remainingAmount int
	}

	var availableTxs []txRow
	totalAvailable := 0

	for rows.Next() {
		var tx txRow
		if err := rows.Scan(&tx.id, &tx.remainingAmount); err != nil {
			return err
		}
		availableTxs = append(availableTxs, tx)
		totalAvailable += tx.remainingAmount
	}
	rows.Close()

	// Check if we have enough balance
	if totalAvailable < amount {
		return ErrInsufficientFunds
	}

	// Deduct from transactions FIFO
	remainingToDeduct := amount
	updateQuery := `
		UPDATE transactions
		SET remaining_amount = $1
		WHERE id = $2`

	for _, txRow := range availableTxs {
		if remainingToDeduct <= 0 {
			break
		}

		deductFromThis := remainingToDeduct
		if deductFromThis > txRow.remainingAmount {
			deductFromThis = txRow.remainingAmount
		}

		newRemaining := txRow.remainingAmount - deductFromThis
		_, err := tx.ExecContext(ctx, updateQuery, newRemaining, txRow.id)
		if err != nil {
			return err
		}

		remainingToDeduct -= deductFromThis
	}

	return tx.Commit()
}

func (m BalanceModel) Update(balance *Balance) error {
	query := `
		UPDATE balances
		SET amount = $2, updated_at = $3
		WHERE id = $1
		RETURNING updated_at`
	args := []any{
		balance.Id,
		balance.Amount,
		time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&balance.UpdatedAt)
	if err != nil {
		return err
	}
	return nil
}
