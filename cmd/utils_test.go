package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"testing"
)

func TestCodeChallengeFromVerifier(t *testing.T) {
	verifier := "test-verifier-123"
	expectedSum := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.StdEncoding.EncodeToString(expectedSum[:])

	got := codeChallengeFromVerifier(verifier)
	if got != expectedChallenge {
		t.Errorf("codeChallengeFromVerifier(%q) = %q; want %q", verifier, got, expectedChallenge)
	}
}

func TestCodeChallengeFromVerifier_Deterministic(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	a := codeChallengeFromVerifier(verifier)
	b := codeChallengeFromVerifier(verifier)
	if a != b {
		t.Errorf("code challenge should be deterministic; got %q and %q", a, b)
	}
	if a == "" {
		t.Error("code challenge should not be empty")
	}
}

func TestGenerateOAuthAuthorizationLink_URLShape(t *testing.T) {
	linked := make(chan bool)
	state, link, err := generateOAuthAuthorizationLink("test-discord-id", linked)
	if err != nil {
		t.Fatalf("generateOAuthAuthorizationLink: %v", err)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	if u.Scheme != "https" {
		t.Errorf("scheme = %q; want https", u.Scheme)
	}
	if u.Host != "www.pathofexile.com" {
		t.Errorf("host = %q; want www.pathofexile.com", u.Host)
	}
	if u.Path != "/oauth/authorize" {
		t.Errorf("path = %q; want /oauth/authorize", u.Path)
	}

	q := u.Query()
	required := map[string]string{
		"client_id":             "poe-herald",
		"response_type":         "code",
		"scope":                 "account:characters",
		"code_challenge_method": "S256",
	}
	for key, want := range required {
		got := q.Get(key)
		if got != want {
			t.Errorf("query %q = %q; want %q", key, got, want)
		}
	}

	if q.Get("redirect_uri") == "" {
		t.Error("redirect_uri must be set")
	}
	if q.Get("state") == "" {
		t.Error("state must be non-empty")
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge must be non-empty")
	}

	state = q.Get("state")
	OauthMutex.Lock()
	delete(OauthMap, state)
	OauthMutex.Unlock()
}
