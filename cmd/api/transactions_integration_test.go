package main

import (
	"bytes"
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

	return &application{
		models: data.Models{
			Balances: data.BalanceModel{DB: db},
		},
	}
}

func TestDepositWithoutLifetimeDays(t *testing.T) {
	app := newTestApplication(t)
	server := httptest.NewServer(app.routes())
	defer server.Close()

	user := uuid.New().String()

	body := []byte(`{"user_id":"` + user + `","amount":100,"type":"deposit"}`)
	resp, err := http.Post(server.URL+"/v1/transactions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("deposit request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["amount"].(float64) != 100 {
		t.Errorf("expected amount 100, got %v", result["amount"])
	}
}

func TestDepositWithLifetimeDays(t *testing.T) {
	app := newTestApplication(t)
	server := httptest.NewServer(app.routes())
	defer server.Close()

	user := uuid.New().String()

	body := []byte(`{"user_id":"` + user + `","amount":200,"type":"deposit","lifetime_days":10}`)
	resp, err := http.Post(server.URL+"/v1/transactions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("deposit request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["amount"].(float64) != 200 {
		t.Errorf("expected amount 200, got %v", result["amount"])
	}
}

func TestDepositAndBalanceFlow(t *testing.T) {
	app := newTestApplication(t)
	server := httptest.NewServer(app.routes())
	defer server.Close()

	user := uuid.New().String()

	// Deposit 1: default lifetime (365 days)
	body1 := []byte(`{"user_id":"` + user + `","amount":100,"type":"deposit"}`)
	resp1, err := http.Post(server.URL+"/v1/transactions", "application/json", bytes.NewReader(body1))
	if err != nil {
		t.Fatalf("deposit1 request: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for deposit1, got %d", resp1.StatusCode)
	}

	// Deposit 2: 10 day lifetime (within 30 day window)
	body2 := []byte(`{"user_id":"` + user + `","amount":200,"type":"deposit","lifetime_days":10}`)
	resp2, err := http.Post(server.URL+"/v1/transactions", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatalf("deposit2 request: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for deposit2, got %d", resp2.StatusCode)
	}

	// Get balance - should be 300
	resp3, err := http.Get(server.URL + "/v1/users/" + user + "/balance")
	if err != nil {
		t.Fatalf("get balance request: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for balance, got %d", resp3.StatusCode)
	}

	var balanceResp map[string]any
	if err := json.NewDecoder(resp3.Body).Decode(&balanceResp); err != nil {
		t.Fatalf("failed to decode balance response: %v", err)
	}

	// Verify structure
	if _, ok := balanceResp["user_id"]; !ok {
		t.Error("expected user_id in response")
	}
	if _, ok := balanceResp["balance"]; !ok {
		t.Error("expected balance in response")
	}
	if _, ok := balanceResp["expirations"]; !ok {
		t.Error("expected expirations in response")
	}

	if balanceResp["balance"].(float64) != 300 {
		t.Errorf("expected balance 300, got %v", balanceResp["balance"])
	}

	// Expirations should only include 10-day deposit (within 30-day window)
	expirations := balanceResp["expirations"].(map[string]any)
	if len(expirations) == 0 {
		t.Error("expected at least one expiration entry")
	}

	// Withdraw 150
	body4 := []byte(`{"user_id":"` + user + `","amount":150,"type":"withdrawal"}`)
	resp4, err := http.Post(server.URL+"/v1/transactions", "application/json", bytes.NewReader(body4))
	if err != nil {
		t.Fatalf("withdraw request: %v", err)
	}
	defer resp4.Body.Close()

	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for withdrawal, got %d", resp4.StatusCode)
	}

	// Get balance after withdrawal
	resp5, err := http.Get(server.URL + "/v1/users/" + user + "/balance")
	if err != nil {
		t.Fatalf("get balance after withdrawal: %v", err)
	}
	defer resp5.Body.Close()

	if resp5.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp5.StatusCode)
	}

	var balanceResp2 map[string]any
	if err := json.NewDecoder(resp5.Body).Decode(&balanceResp2); err != nil {
		t.Fatalf("failed to decode balance response: %v", err)
	}

	// 300 - 150 = 150
	if balanceResp2["balance"].(float64) != 150 {
		t.Errorf("expected balance 150 after withdrawal, got %v", balanceResp2["balance"])
	}
}

func TestWithdrawalInsufficientFunds(t *testing.T) {
	app := newTestApplication(t)
	server := httptest.NewServer(app.routes())
	defer server.Close()

	user := uuid.New().String()

	// Deposit 50
	body1 := []byte(`{"user_id":"` + user + `","amount":50,"type":"deposit"}`)
	resp1, err := http.Post(server.URL+"/v1/transactions", "application/json", bytes.NewReader(body1))
	if err != nil {
		t.Fatalf("deposit request: %v", err)
	}
	resp1.Body.Close()

	// Try to withdraw 100
	body2 := []byte(`{"user_id":"` + user + `","amount":100,"type":"withdrawal"}`)
	resp2, err := http.Post(server.URL+"/v1/transactions", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatalf("withdraw request: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for insufficient funds, got %d", resp2.StatusCode)
	}
}

func TestValidationErrors(t *testing.T) {
	app := newTestApplication(t)
	server := httptest.NewServer(app.routes())
	defer server.Close()

	tests := []struct {
		name           string
		body           string
		expectedStatus int
	}{
		{
			name:           "invalid user_id",
			body:           `{"user_id":"not-a-uuid","amount":100,"type":"deposit"}`,
			expectedStatus: http.StatusUnprocessableEntity,
		},
		{
			name:           "zero amount",
			body:           `{"user_id":"` + uuid.New().String() + `","amount":0,"type":"deposit"}`,
			expectedStatus: http.StatusUnprocessableEntity,
		},
		{
			name:           "negative amount",
			body:           `{"user_id":"` + uuid.New().String() + `","amount":-10,"type":"deposit"}`,
			expectedStatus: http.StatusUnprocessableEntity,
		},
		{
			name:           "invalid type",
			body:           `{"user_id":"` + uuid.New().String() + `","amount":100,"type":"invalid"}`,
			expectedStatus: http.StatusUnprocessableEntity,
		},
		{
			name:           "invalid lifetime_days",
			body:           `{"user_id":"` + uuid.New().String() + `","amount":100,"type":"deposit","lifetime_days":0}`,
			expectedStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Post(server.URL+"/v1/transactions", "application/json", bytes.NewReader([]byte(tt.body)))
			if err != nil {
				t.Fatalf("request error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestBalanceForNonExistentUser(t *testing.T) {
	app := newTestApplication(t)
	server := httptest.NewServer(app.routes())
	defer server.Close()

	user := uuid.New().String()

	// Get balance for user with no transactions
	resp, err := http.Get(server.URL + "/v1/users/" + user + "/balance")
	if err != nil {
		t.Fatalf("get balance request: %v", err)
	}
	defer resp.Body.Close()

	// Should return 200 with balance 0
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var balanceResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&balanceResp); err != nil {
		t.Fatalf("failed to decode balance response: %v", err)
	}

	if balanceResp["balance"].(float64) != 0 {
		t.Errorf("expected balance 0 for new user, got %v", balanceResp["balance"])
	}
}

func TestBalanceInvalidUserID(t *testing.T) {
	app := newTestApplication(t)
	server := httptest.NewServer(app.routes())
	defer server.Close()

	// Get balance with invalid UUID
	resp, err := http.Get(server.URL + "/v1/users/invalid-uuid/balance")
	if err != nil {
		t.Fatalf("get balance request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for invalid UUID, got %d", resp.StatusCode)
	}
}
