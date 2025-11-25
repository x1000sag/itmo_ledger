package main

import (
	"errors"
	"github.com/google/uuid"
	"net/http"
	"simple-ledger.itmo.ru/internal/data"
	"simple-ledger.itmo.ru/internal/validator"
	"time"
)


type transactionIn struct {
	UserId string 										`json:"user_id"`
	Amount int    										`json:"amount"`
	Type   string 										`json:"type"`
	Expirations []data.ExpirationInfo `json:"expirations"`
}

func (app *application) createTransactionHandler(w http.ResponseWriter, r *http.Request) {
	var trxIn transactionIn
	err := app.readJSON(w, r, &trxIn)
	if err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	userID, err := uuid.Parse(trxIn.UserId)
	if err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	v.Check(trxIn.Amount > 0, "amount", "must be positive")
	v.Check(validator.IsPermitted(trxIn.Type, "deposit", "withdrawal"), "type", "must be deposit or withdrawal")

	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	switch trxIn.Type {
	case "deposit":

		expiresAt := time.Now().AddDate(0, 0, app.config.pointsLifetimeDays)
		transaction := &data.Transaction{
			ID:        uuid.New(),
			UserID:    userID,
			Amount:    trxIn.Amount,
			Type:      "deposit",
			ExpiresAt: &expiresAt,
			Remaining: trxIn.Amount,
		}

		err = app.models.Transactions.Insert(transaction)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}

		balanceInfo, err := app.models.Transactions.GetBalance(userID)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}

		response := balanceResponse{
			UserID:      balanceInfo.UserID,
			Balance:     balanceInfo.Balance,
			Expirations: balanceInfo.Expirations,
		}

		err = app.writeJSON(w, http.StatusCreated, response, nil)
		if err != nil {
			app.serverErrorResponse(w, r, err)
		}

	case "withdrawal":
		err = app.models.Transactions.Withdraw(userID, trxIn.Amount)
		if err != nil {
			if errors.Is(err, data.ErrInsufficientFunds) {
				app.badRequestResponse(w, r, err)
			} else {
				app.serverErrorResponse(w, r, err)
			}
			return
		}

		balanceInfo, err := app.models.Transactions.GetBalance(userID)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}

		response := balanceResponse{
			UserID:      balanceInfo.UserID,
			Balance:     balanceInfo.Balance,
			Expirations: balanceInfo.Expirations,
		}

		err = app.writeJSON(w, http.StatusOK, response, nil)
		if err != nil {
			app.serverErrorResponse(w, r, err)
		}
	}
}


func (app *application) createNewBalance(w http.ResponseWriter, r *http.Request, balance *data.Balance) {
	err := app.models.Balances.Insert(balance)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
	err = app.writeJSON(w, http.StatusCreated, balance, nil)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
}

func (app *application) updateBalance(w http.ResponseWriter, r *http.Request, balance *data.Balance, trxId transactionIn) {
	if trxId.Type == "withdrawal" && balance.Amount < trxId.Amount {
		app.badRequestResponse(w, r, errors.New("insufficient funds"))
		return
	}

	if trxId.Type == "deposit" {
		balance.Amount += trxId.Amount
	} else {
		balance.Amount -= trxId.Amount
	}
	err := app.models.Balances.Update(balance)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
	err = app.writeJSON(w, http.StatusOK, balance, nil)
	if err != nil {
		app.serverErrorResponse(w, r, err)
	}
	return
}

func (app *application) showUserBalanceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil || id == uuid.Nil {
		app.notFoundResponse(w, r)
		return
	}

	balance, err := app.models.Balances.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	if err = app.writeJSON(w, http.StatusOK, balance, nil); err != nil {
		app.serverErrorResponse(w, r, err)
	}
}
