package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"simple-ledger.itmo.ru/internal/data"
	"simple-ledger.itmo.ru/internal/test"
)

func testApp(t *testing.T) *application {
	t.Helper()
	db := test.SetupTestDB(t)
	test.ResetTransactions(t, db)
	return &application{
		models: data.Models{Balances: data.BalanceModel{DB: db}},
	}
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request build: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func parseBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return m
}

func TestValidationErrors(t *testing.T) {
	app := testApp(t)
	srv := httptest.NewServer(app.routes())
	defer srv.Close()
	user := uuid.New().String()

	cases := []struct {
		name         string
		payload      string
		expectStatus int
		expectField  string
	}{
		{"invalid_type", `{"user_id":"` + user + `","amount":10,"type":"transfer"}`, 422, "type"},
		{"zero_amount", `{"user_id":"` + user + `","amount":0,"type":"deposit"}`, 422, "amount"},
		{"negative_amount", `{"user_id":"` + user + `","amount":-5,"type":"deposit"}`, 422, "amount"},
		{"empty_user", `{"user_id":"","amount":10,"type":"deposit"}`, 422, "user_id"},
		{"lifetime_zero", `{"user_id":"` + user + `","amount":10,"type":"deposit","lifetime_days":-1}`, 422, "lifetime_days"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := doJSON(t, srv, http.MethodPost, "/v1/transactions", []byte(c.payload))
			if resp.StatusCode != c.expectStatus {
				t.Fatalf("expected status %d got %d", c.expectStatus, resp.StatusCode)
			}
			body := parseBody(t, resp)
			// assume validation errors are in body["error"] which is a map
			found := false
			if errData, ok := body["error"].(map[string]any); ok {
				if _, exists := errData[c.expectField]; exists {
					found = true
				}
			}
			if !found {
				t.Errorf("expected field %s in error body: %+v", c.expectField, body)
			}
		})
	}
}

func TestWithdrawalInsufficientViaHTTP(t *testing.T) {
	app := testApp(t)
	srv := httptest.NewServer(app.routes())
	defer srv.Close()
	user := uuid.New().String()

	// deposit 50
	resp := doJSON(t, srv, http.MethodPost, "/v1/transactions", []byte(`{"user_id":"`+user+`","amount":50,"type":"deposit"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// withdraw 70 (more than balance)
	resp = doJSON(t, srv, http.MethodPost, "/v1/transactions", []byte(`{"user_id":"`+user+`","amount":70,"type":"withdrawal"}`))
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)

	// look for error substring "insufficient funds"
	found := false
	if errMsg, ok := body["error"].(string); ok {
		if strings.Contains(strings.ToLower(errMsg), "insufficient") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected insufficient funds error, body=%+v", body)
	}
}
