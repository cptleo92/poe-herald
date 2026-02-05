package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cptleo92/poe-herald/database"
)

func testNewApplication() *application {
	return &application{
		config: config{env: "development"},
		models: database.Models{},
	}
}

func TestHealthcheck(t *testing.T) {
	app := testNewApplication()

	req := httptest.NewRequest(http.MethodGet, "/healthcheck", nil)
	rr := httptest.NewRecorder()

	app.healthcheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK; got %d", rr.Code)
	}

	body := rr.Body.String()
	expected := fmt.Sprintf(`{"status": "available", "environment": "development", "version": %q}`, version)
	if body != expected {
		t.Errorf("expected %q; got %q", expected, body)
	}
}

func TestOauthCallback_InvalidParams(t *testing.T) {
	app := testNewApplication()

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback", nil)
	rr := httptest.NewRecorder()

	app.oauthCallback(rr, req)

	test := []struct {
		name           string
		query          string
		expectedStatus int
		expectedBody   string
	}{
		{"missing params", "", http.StatusBadRequest, "Invalid request"},
		{"missing code", "?state=123", http.StatusBadRequest, "Invalid request"},
		{"missing state", "?code=123", http.StatusBadRequest, "Invalid request"},
	}

	for _, tt := range test {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/oauth/callback?"+tt.query, nil)
			rr := httptest.NewRecorder()

			app.oauthCallback(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d; got %d", tt.expectedStatus, rr.Code)
			}

			body := rr.Body.String()
			if !strings.Contains(body, tt.expectedBody) {
				t.Errorf("expected body %q; got %q", tt.expectedBody, body)
			}
		})
	}
}

// func TestOauthCallback_ValidParams(t *testing.T) {
// 	app := testNewApplication(t)

// 	state := "fake-state-123"
// 	OauthMutex.Lock()
// 	OauthMap[state] = oauthCredentials{
// 		discordID:    "fake-discord-id-123",
// 		codeVerifier: "fake-code-verifier-123",
// 	}
// 	OauthMutex.Unlock()

// 	defer func() {
// 		OauthMutex.Lock()
// 		delete(OauthMap, state)
// 		OauthMutex.Unlock()
// 	}()

// 	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=123&state="+state, nil)
// 	rr := httptest.NewRecorder()

// 	app.oauthCallback(rr, req)
// }
