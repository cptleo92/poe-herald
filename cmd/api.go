package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cptleo92/poe-herald/database"
	"github.com/julienschmidt/httprouter"
)

const (
	authorizeLink = "https://www.pathofexile.com/oauth/authorize"
	tokenLink     = "https://www.pathofexile.com/oauth/token"
	redirectURI   = "https://bot.poe-herald.com/oauth/callback"
	scope         = "account:characters"
)

type oauthCredentials struct {
	discordID      string
	codeVerifier   string
	successChannel chan bool
}

var (
	OauthMutex sync.Mutex
	OauthMap   = make(map[string]oauthCredentials)
)

func (app *application) routes() http.Handler {
	router := httprouter.New()
	router.HandlerFunc(http.MethodGet, "/healthcheck", app.healthcheck)
	router.HandlerFunc(http.MethodGet, "/oauth/callback", app.oauthCallback)
	return router
}

func (app *application) healthcheck(w http.ResponseWriter, r *http.Request) {
	js := `{"status": "available", "environment": %q, "version": %q}`
	js = fmt.Sprintf(js, app.config.env, version)

	w.Header().Set("Content-Type", "application/json")

	w.Write([]byte(js))
}

func (app *application) oauthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	oauthCredentials, ok := OauthMap[state]

	if !ok || code == "" || state == "" {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if oauthCredentials.discordID == "" {
		http.Error(w, "Unable to find account", http.StatusBadRequest)
		return
	}

	// Any upcoming erorrs will send false to the success channel
	success := false
	defer func() {
		if !success {
			oauthCredentials.successChannel <- false
		}
	}()

	values := url.Values{}
	values.Set("client_id", os.Getenv("CLIENT_ID"))
	values.Set("client_secret", os.Getenv("CLIENT_SECRET"))
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", scope)
	values.Set("code_verifier", oauthCredentials.codeVerifier)
	formBody := values.Encode()

	// Make token request
	req, err := http.NewRequest(http.MethodPost, tokenLink, strings.NewReader(formBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "OAuth "+os.Getenv("CLIENT_ID")+"/"+version+" (contact: leo.cheng92@gmail.com)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("OAuth token error: status=%d body=%s", resp.StatusCode, string(body))
		http.Error(w, "Error getting OAuth token", http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	/*
		Example response:
		{
		    "access_token": "486132c90fedb152360bc0e1aa54eea155768eb9",
		    "expires_in": 2592000,
		    "token_type": "bearer",
		    "scope": "account:profile",
		    "username": "Novynn",
		    "sub": "c5b9c286-8d05-47af-be41-67ab10a8c53e",
		    "refresh_token": "17abaa74e599192f7650a4b89b6e9dfef2ff68cd"
		}
	*/
	var tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Username     string `json:"username"`
	}

	err = json.Unmarshal(body, &tokenResponse)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Save to DB, etc...
	err = app.models.Users.InsertUser(database.User{
		ID:                oauthCredentials.discordID,
		GGGAccountName:    tokenResponse.Username,
		OauthAccessToken:  tokenResponse.AccessToken,
		OauthRefreshToken: tokenResponse.RefreshToken,
		OauthExpiresAt:    time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second),
	})
	if err != nil {
		if isPGDuplicateError(err) {
			oauthCredentials.successChannel <- false
			http.Error(w, "User already linked", http.StatusBadRequest)
			return
		}
		oauthCredentials.successChannel <- false
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	success = true // Prevent defer from running
	oauthCredentials.successChannel <- true

	// Clean state up
	OauthMutex.Lock()
	delete(OauthMap, state)
	OauthMutex.Unlock()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("User linked successfully! Please go back to the Discord bot for further instructions."))
}
