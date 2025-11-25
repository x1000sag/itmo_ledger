package data

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"time"
)
type Transaction struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	Amount      int        `json:"amount"`
	Type        string     `json:"type"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Remaining   int        `json:"remaining"`
	Description string     `json:"description,omitempty"`
}
type Balance struct {
	Id        uuid.UUID `json:"id"`
	Amount    int       `json:"amount"`
	UpdatedAt time.Time `json:"updated_at"`
	Expirations []ExpirationInfo `json:"expirations"`
}
type ExpirationInfo struct {
	Date   time.Time `json:"date"`
	Amount int       `json:"amount"`
}
type BalanceModel struct {
	DB *sql.DB
}
type TransactionModel struct {
	DB *sql.DB
}
var (
	ErrInsufficientFunds = errors.New("insufficient funds")
)

func (m TransactionModel) Insert(transaction *Transaction) error {
	query := `
		INSERT INTO transactions (id, user_id, amount, type, created_at, expires_at, remaining, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`

	args := []any{
		transaction.ID,
		transaction.UserID,
		transaction.Amount,
		transaction.Type,
		time.Now(),
		transaction.ExpiresAt,
		transaction.Remaining,
		transaction.Description,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return m.DB.QueryRowContext(ctx, query, args...).Scan(&transaction.ID, &transaction.CreatedAt)
}
func (m TransactionModel) GetBalance(userID uuid.UUID) (*Balance, error) {
	balanceInfo := &Balance{
		UserID:      userID,
		Expirations: []ExpirationInfo{},
	}

	balanceQuery := `
		SELECT COALESCE(SUM(remaining), 0) 
		FROM transactions 
		WHERE user_id = $1 AND type = 'deposit' AND (expires_at IS NULL OR expires_at > NOW())`
	
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, balanceQuery, userID).Scan(&balanceInfo.Balance)
	if err != nil {
		return nil, err
	}
	expirationsQuery := `
		SELECT DATE(expires_at) as expiration_date, SUM(remaining) as amount
		FROM transactions 
		WHERE user_id = $1 AND type = 'deposit' AND expires_at > NOW() AND remaining > 0
		GROUP BY DATE(expires_at)
		ORDER BY expiration_date
		LIMIT 10`

	rows, err := m.DB.QueryContext(ctx, expirationsQuery, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var exp ExpirationInfo
		err := rows.Scan(&exp.Date, &exp.Amount)
		if err != nil {
			return nil, err
		}
		balanceInfo.Expirations = append(balanceInfo.Expirations, exp)
	}

	return balanceInfo, nil
}

func (m TransactionModel) Withdraw(userID uuid.UUID, amount int) error {
	tx, err := m.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("SELECT 1 FROM transactions WHERE user_id = $1 FOR UPDATE", userID)
	if err != nil {
		return err
	}

	rows, err := tx.Query(`
		SELECT id, remaining 
		FROM transactions 
		WHERE user_id = $1 AND type = 'deposit' AND remaining > 0 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at ASC, expires_at ASC`, 
		userID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var deposits []struct {
		ID        uuid.UUID
		Remaining int
	}
	totalAvailable := 0

	for rows.Next() {
		var deposit struct {
			ID        uuid.UUID
			Remaining int
		}
		if err := rows.Scan(&deposit.ID, &deposit.Remaining); err != nil {
			return err
		}
		deposits = append(deposits, deposit)
		totalAvailable += deposit.Remaining
	}

	if totalAvailable < amount {
		return ErrInsufficientFunds
	}

	remainingToDeduct := amount
	for _, deposit := range deposits {
		if remainingToDeduct <= 0 {
			break
		}

		deductAmount := deposit.Remaining
		if deductAmount > remainingToDeduct {
			deductAmount = remainingToDeduct
		}

		_, err = tx.Exec(`
			UPDATE transactions 
			SET remaining = remaining - $1 
			WHERE id = $2`, 
			deductAmount, deposit.ID)
		if err != nil {
			return err
		}

		withdrawalID := uuid.New()
		_, err = tx.Exec(`
			INSERT INTO transactions (id, user_id, amount, type, created_at, remaining, description)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			withdrawalID, userID, -deductAmount, "withdrawal", time.Now(), 0, 
			fmt.Sprintf("Withdrawal from deposit %s", deposit.ID))
		if err != nil {
			return err
		}

		remainingToDeduct -= deductAmount
	}

	return tx.Commit()
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
