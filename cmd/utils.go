package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
)

func generateOAuthLink() (string, error) {
	codeBuf := make([]byte, 32)
	_, err := rand.Read(codeBuf)
	if err != nil {
		return "", err
	}

	codeVerifier := hex.EncodeToString(codeBuf)

	codeChallengeBytes := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.StdEncoding.EncodeToString(codeChallengeBytes[:])

	stateBuf := make([]byte, 24)
	_, err = rand.Read(stateBuf)
	if err != nil {
		return "", err
	}

	state := base64.RawURLEncoding.EncodeToString(stateBuf)

	scopes := []string{
		"account:characters",
	}

	base := "https://www.pathofexile.com/oauth/authorize"
	params := url.Values{}
	params.Set("client_id", "poe-herald")
	params.Set("redirect_uri", "https://poe-herald.com/oauth/callback")
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(scopes, " "))
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)

	u, _ := url.Parse(base)
	u.RawQuery = params.Encode()
	authURL := u.String()

	return authURL, nil
}
