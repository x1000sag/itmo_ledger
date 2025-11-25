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

type BalanceModel struct {
	DB *sql.DB
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

// Transaction represents a bonus points grant with expiration
type Transaction struct {
	ID              uuid.UUID `json:"id"`
	UserID          uuid.UUID `json:"user_id"`
	Amount          int       `json:"amount"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	RemainingAmount int       `json:"remaining_amount"`
}

// AddBonusPoints creates a new bonus points grant for a user with specified lifetime
func (m BalanceModel) AddBonusPoints(userID uuid.UUID, amount int, lifetimeDays int) (*Transaction, error) {
	query := `
		INSERT INTO transactions (user_id, amount, expires_at, remaining_amount)
		VALUES ($1, $2, NOW() + ($3 || ' days')::interval, $2)
		RETURNING id, user_id, amount, created_at, expires_at, remaining_amount`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	trx := &Transaction{}
	err := m.DB.QueryRowContext(ctx, query, userID, amount, lifetimeDays).Scan(
		&trx.ID,
		&trx.UserID,
		&trx.Amount,
		&trx.CreatedAt,
		&trx.ExpiresAt,
		&trx.RemainingAmount,
	)
	if err != nil {
		return nil, err
	}
	return trx, nil
}

// GetBalanceWithExpiration returns total available balance and upcoming expirations within 30 days
func (m BalanceModel) GetBalanceWithExpiration(userID uuid.UUID) (int, map[string]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Get total balance of non-expired transactions
	balanceQuery := `
		SELECT COALESCE(SUM(remaining_amount), 0)
		FROM transactions
		WHERE user_id = $1 AND expires_at > NOW() AND remaining_amount > 0`

	var balance int
	err := m.DB.QueryRowContext(ctx, balanceQuery, userID).Scan(&balance)
	if err != nil {
		return 0, nil, err
	}

	// Get expirations within 30 days
	expirationQuery := `
		SELECT DATE(expires_at), SUM(remaining_amount)
		FROM transactions
		WHERE user_id = $1 AND expires_at > NOW() AND expires_at <= NOW() + INTERVAL '30 days' AND remaining_amount > 0
		GROUP BY DATE(expires_at)
		ORDER BY DATE(expires_at)`

	rows, err := m.DB.QueryContext(ctx, expirationQuery, userID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	expirations := make(map[string]int)
	for rows.Next() {
		var date time.Time
		var amount int
		if err := rows.Scan(&date, &amount); err != nil {
			return 0, nil, err
		}
		expirations[date.Format("2006-01-02")] = amount
	}

	if err = rows.Err(); err != nil {
		return 0, nil, err
	}

	return balance, expirations, nil
}

// WithdrawBonusPoints deducts points using FIFO ordering (oldest expiring first)
func (m BalanceModel) WithdrawBonusPoints(userID uuid.UUID, amount int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock and get non-expired transactions ordered by expiration (FIFO)
	query := `
		SELECT id, remaining_amount
		FROM transactions
		WHERE user_id = $1 AND expires_at > NOW() AND remaining_amount > 0
		ORDER BY expires_at ASC
		FOR UPDATE`

	rows, err := tx.QueryContext(ctx, query, userID)
	if err != nil {
		return err
	}

	type txRecord struct {
		id        uuid.UUID
		remaining int
	}
	var records []txRecord

	for rows.Next() {
		var r txRecord
		if err := rows.Scan(&r.id, &r.remaining); err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	rows.Close()

	if err = rows.Err(); err != nil {
		return err
	}

	// Calculate total available
	totalAvailable := 0
	for _, r := range records {
		totalAvailable += r.remaining
	}

	if totalAvailable < amount {
		return ErrInsufficientFunds
	}

	// Deduct from transactions in FIFO order
	remaining := amount
	updateQuery := `UPDATE transactions SET remaining_amount = $1 WHERE id = $2`

	for _, r := range records {
		if remaining <= 0 {
			break
		}
		if r.remaining >= remaining {
			// This transaction can cover the remaining amount
			newRemaining := r.remaining - remaining
			if _, err := tx.ExecContext(ctx, updateQuery, newRemaining, r.id); err != nil {
				return err
			}
			remaining = 0
		} else {
			// Use up this transaction completely
			if _, err := tx.ExecContext(ctx, updateQuery, 0, r.id); err != nil {
				return err
			}
			remaining -= r.remaining
		}
	}

	return tx.Commit()
}
