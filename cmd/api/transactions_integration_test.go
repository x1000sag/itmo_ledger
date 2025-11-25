package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"simple-ledger.itmo.ru/internal/data"
	"simple-ledger.itmo.ru/internal/test"
)

func newTestApplication(t *testing.T) *application {
	t.Helper()
	db := test.SetupTestDB(t)
	// Each test uses unique UUIDs, no need to truncate shared table
	return &application{
		models: data.Models{Balances: data.BalanceModel{DB: db}},
	}
}

func doPost(t *testing.T, srv *httptest.Server, payload string) *http.Response {
	t.Helper()
	return doJSON(t, srv, http.MethodPost, "/v1/transactions", []byte(payload))
}

func getBalanceValue(t *testing.T, srv *httptest.Server, userID string) int {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/users/" + userID + "/balance")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode balance response: %v", err)
	}

	balanceFloat, ok := body["balance"].(float64)
	if !ok {
		t.Fatalf("balance is not a number: %+v", body)
	}
	return int(balanceFloat)
}

func TestFIFOThreeDeposits(t *testing.T) {
	app := newTestApplication(t)
	srv := httptest.NewServer(app.routes())
	defer srv.Close()
	user := uuid.New().String()

	// lifetimes: 5, 10, 25 days
	resp := doPost(t, srv, `{"user_id":"`+user+`","amount":100,"type":"deposit","lifetime_days":5}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first deposit status %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doPost(t, srv, `{"user_id":"`+user+`","amount":150,"type":"deposit","lifetime_days":10}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second deposit status %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doPost(t, srv, `{"user_id":"`+user+`","amount":200,"type":"deposit","lifetime_days":25}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("third deposit status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Withdraw 220 using FIFO:
	// - Consume all 100 from 5-day deposit
	// - Consume 120 from 150 in 10-day deposit, leaving 30
	// - 200 in 25-day deposit remains untouched
	// Total remaining: 30 + 200 = 230
	resp = doPost(t, srv, `{"user_id":"`+user+`","amount":220,"type":"withdrawal"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdraw status %d", resp.StatusCode)
	}
	resp.Body.Close()

	bal := getBalanceValue(t, srv, user)
	if bal != 230 {
		t.Fatalf("expected balance 230 got %d", bal)
	}
}

func TestExpirationWindow30Days(t *testing.T) {
	app := newTestApplication(t)
	srv := httptest.NewServer(app.routes())
	defer srv.Close()
	user := uuid.New().String()

	// inside window (10 days)
	resp := doPost(t, srv, `{"user_id":"`+user+`","amount":80,"type":"deposit","lifetime_days":10}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first deposit status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// outside window (90 days)
	resp = doPost(t, srv, `{"user_id":"`+user+`","amount":120,"type":"deposit","lifetime_days":90}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second deposit status %d", resp.StatusCode)
	}
	resp.Body.Close()

	respGet, err := http.Get(srv.URL + "/v1/users/" + user + "/balance")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	defer respGet.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(respGet.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Check that expirations only contains the 10-day deposit (80 points within 30-day window)
	// The 90-day deposit should not appear in expirations
	expirations, ok := body["expirations"].(map[string]any)
	if !ok {
		t.Fatalf("expirations is not a map: %+v", body)
	}

	// Sum up expirations - should be exactly 80 (only the 10-day deposit)
	sum := 0.0
	for _, v := range expirations {
		if fv, ok := v.(float64); ok {
			sum += fv
		}
	}

	// Expirations should only show 80 (the one within 30 days)
	if sum != 80 {
		t.Fatalf("expected expirations sum 80 (only 10-day deposit), got %.0f", sum)
	}

	// Total balance should be 200
	bal := getBalanceValue(t, srv, user)
	if bal != 200 {
		t.Fatalf("expected total balance 200 got %d", bal)
	}
}
