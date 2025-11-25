package main

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"simple-ledger.itmo.ru/internal/data"
	"simple-ledger.itmo.ru/internal/validator"
)

const defaultLifetimeDays = 365

type transactionIn struct {
	UserId       string `json:"user_id"`
	Amount       int    `json:"amount"`
	Type         string `json:"type"`
	LifetimeDays *int   `json:"lifetime_days,omitempty"`
}

type balanceResponse struct {
	UserID      string         `json:"user_id"`
	Balance     int            `json:"balance"`
	Expirations map[string]int `json:"expirations"`
}

func (app *application) createTransactionHandler(w http.ResponseWriter, r *http.Request) {
	var trxIn transactionIn
	err := app.readJSON(w, r, &trxIn)
	if err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	id, err := uuid.Parse(trxIn.UserId)

	v := validator.New()
	v.Check(err == nil, "user_id", "must be uuid")
	v.Check(trxIn.Amount > 0, "amount", "must be positive")
	v.Check(validator.IsPermitted(trxIn.Type, "deposit", "withdrawal"), "type", "must be deposit or withdrawal")
	if trxIn.LifetimeDays != nil {
		v.Check(*trxIn.LifetimeDays > 0, "lifetime_days", "must be positive")
	}

	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if trxIn.Type == "deposit" {
		lifetimeDays := defaultLifetimeDays
		if trxIn.LifetimeDays != nil {
			lifetimeDays = *trxIn.LifetimeDays
		}
		trx, err := app.models.Balances.AddBonusPoints(id, trxIn.Amount, lifetimeDays)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
		if err = app.writeJSON(w, http.StatusCreated, trx, nil); err != nil {
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	// withdrawal
	err = app.models.Balances.WithdrawBonusPoints(id, trxIn.Amount)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrInsufficientFunds):
			app.badRequestResponse(w, r, errors.New("insufficient funds"))
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	// Return updated balance after withdrawal
	balance, expirations, err := app.models.Balances.GetBalanceWithExpiration(id)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	resp := balanceResponse{
		UserID:      id.String(),
		Balance:     balance,
		Expirations: expirations,
	}
	if err = app.writeJSON(w, http.StatusOK, resp, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

func (app *application) showUserBalanceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil || id == uuid.Nil {
		app.notFoundResponse(w, r)
		return
	}

	balance, expirations, err := app.models.Balances.GetBalanceWithExpiration(id)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	resp := balanceResponse{
		UserID:      id.String(),
		Balance:     balance,
		Expirations: expirations,
	}
	if err = app.writeJSON(w, http.StatusOK, resp, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}
