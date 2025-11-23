package main

import (
	"github.com/google/uuid"
	"net/http"
	"simple-ledger.itmo.ru/internal/validator"
)

type transactionIn struct {
	UserId       string `json:"user_id"`
	Amount       int    `json:"amount"`
	Type         string `json:"type"`
	LifetimeDays int    `json:"lifetime_days,omitempty"`
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

	if trxIn.Type == "deposit" {
		if trxIn.LifetimeDays == 0 {
			trxIn.LifetimeDays = 365 // Default to 1 year
		}
		v.Check(trxIn.LifetimeDays > 0, "lifetime_days", "must be positive")
	}

	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if trxIn.Type == "deposit" {
		transaction, err := app.models.Balances.AddBonusPoints(id, trxIn.Amount, trxIn.LifetimeDays)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
		err = app.writeJSON(w, http.StatusCreated, transaction, nil)
		if err != nil {
			app.serverErrorResponse(w, r, err)
		}
	} else {
		err := app.models.Balances.WithdrawBonusPoints(id, trxIn.Amount)
		if err != nil {
			if err.Error() == "insufficient funds" {
				app.badRequestResponse(w, r, err)
			} else {
				app.serverErrorResponse(w, r, err)
			}
			return
		}

		// Return the new balance
		balance, expirations, err := app.models.Balances.GetBalanceWithExpiration(id)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}

		response := map[string]any{
			"user_id":     id,
			"balance":     balance,
			"expirations": expirations,
		}
		err = app.writeJSON(w, http.StatusOK, response, nil)
		if err != nil {
			app.serverErrorResponse(w, r, err)
		}
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

	response := map[string]any{
		"user_id":     id,
		"balance":     balance,
		"expirations": expirations,
	}

	if err = app.writeJSON(w, http.StatusOK, response, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}
