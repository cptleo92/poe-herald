package ggg

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimit(t *testing.T) {
	// 1. Clear tracker before tests
	trackerMutex.Lock()
	rateLimitTracker = make(map[string]time.Time)
	trackerMutex.Unlock()

	accessToken := "test-token"
	userAgent := "test-agent"
	
	// 2. Setup mock server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"characters": []}`))
	}))
	defer server.Close()

	// Override baseURL
	originalBaseURL := baseURL
	baseURL = server.URL
	defer func() { baseURL = originalBaseURL }()

	client := NewClient(accessToken, userAgent)

	// 3. First call should hit mock server and return 429
	_, err := client.FetchCharacters()
	if err == nil {
		t.Fatal("Expected error on first call, got nil")
	}

	var rateErr *RateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("Expected RateLimitError, got %T: %v", err, err)
	}

	if rateErr.RetryAfter != 2*time.Second {
		t.Fatalf("Expected RetryAfter 2s, got %v", rateErr.RetryAfter)
	}

	if callCount != 1 {
		t.Fatalf("Expected 1 call to mock server, got %d", callCount)
	}

	// 4. Second call should be BLOCKED by client immediately without hitting server
	_, err = client.FetchCharacters()
	if err == nil {
		t.Fatal("Expected error on second call, got nil")
	}

	if !errors.As(err, &rateErr) {
		t.Fatalf("Expected RateLimitError on second call, got %T: %v", err, err)
	}

	if callCount != 1 {
		t.Fatalf("Expected mock server call count to stay at 1, but got %d (client didn't block!)", callCount)
	}

	t.Log("Rate limiting successfully blocked redundant request.")
}
