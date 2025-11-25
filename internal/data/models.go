package data

import (
	"database/sql"
	"errors"
)

var (
	ErrRecordNotFound    = errors.New("record not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

type Models struct {
	Balances BalanceModel
}

func NewModels(db *sql.DB) Models {
	return Models{
		Balances: BalanceModel{DB: db},
	}
}
