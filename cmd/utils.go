package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
)

func codeChallengeFromVerifier(codeVerifier string) string {
	sum := sha256.Sum256([]byte(codeVerifier))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (app *application) generateOAuthAuthorizationLink(discordID string, successChannel chan bool) (string, string, error) {
	codeBuf := make([]byte, 32)
	_, err := rand.Read(codeBuf)
	if err != nil {
		return "", "", err
	}

	codeVerifier := hex.EncodeToString(codeBuf)
	codeChallenge := codeChallengeFromVerifier(codeVerifier)

	stateBuf := make([]byte, 24)
	_, err = rand.Read(stateBuf)
	if err != nil {
		return "", "", err
	}

	state := base64.RawURLEncoding.EncodeToString(stateBuf)

	OauthMutex.Lock()
	OauthMap[state] = oauthCredentials{
		discordID:      discordID,
		codeVerifier:   codeVerifier,
		successChannel: successChannel,
	}
	OauthMutex.Unlock()

	base := authorizeLink
	params := url.Values{}
	params.Set("client_id", app.config.clientID)
	params.Set("response_type", "code")
	params.Set("scope", scope)
	params.Set("state", state)
	params.Set("redirect_uri", app.config.redirectURI)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")

	u, _ := url.Parse(base)
	u.RawQuery = params.Encode()
	authURL := u.String()

	return state, authURL, nil
}
