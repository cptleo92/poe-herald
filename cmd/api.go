package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cptleo92/poe-herald/database"
	"github.com/julienschmidt/httprouter"
)

const (
	authorizeLink = "https://www.pathofexile.com/oauth/authorize"
	tokenLink     = "https://www.pathofexile.com/oauth/token"
	redirectURI   = "https://bot.poe-herald.com/oauth/callback"
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

	OauthMutex.Lock()
	oauthCredentials, ok := OauthMap[state]
	if ok {
		delete(OauthMap, state)
	}
	OauthMutex.Unlock()

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

	var tokenRequestBody struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		RedirectURI  string `json:"redirect_uri"`
		Scope        string `json:"scope"`
		CodeVerifier string `json:"code_verifier"`
	}

	tokenRequestBody.ClientID = os.Getenv("CLIENT_ID")
	tokenRequestBody.ClientSecret = os.Getenv("CLIENT_SECRET")
	tokenRequestBody.GrantType = "authorization_code"
	tokenRequestBody.Code = code
	tokenRequestBody.RedirectURI = redirectURI
	tokenRequestBody.Scope = "account:characters"
	tokenRequestBody.CodeVerifier = oauthCredentials.codeVerifier

	_, err := json.Marshal(tokenRequestBody)
	if err != nil {
		http.Error(w, "Error marshalling token request body", http.StatusInternalServerError)
		return
	}

	// Make token request
	// resp, err := http.Post(tokenLink, "application/x-www-form-urlencoded", bytes.NewBuffer(bodyJson))
	// if err != nil {
	// 	http.Error(w, err.Error(), http.StatusInternalServerError)
	// 	return
	// }

	// defer resp.Body.Close()

	// if resp.StatusCode != http.StatusOK {
	// 	http.Error(w, "Error getting OAuth token", http.StatusInternalServerError)
	// 	return
	// }

	// body, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	http.Error(w, err.Error(), http.StatusInternalServerError)
	// 	return
	// }

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

	dummyResponse := []byte(`
				{
		    "access_token": "486132c90fedb152360bc0e1aa54eea155768eb9",
		    "expires_in": 2592000,
		    "token_type": "bearer",
		    "scope": "account:profile",
		    "username": "Novynn",
		    "sub": "c5b9c286-8d05-47af-be41-67ab10a8c53e",
		    "refresh_token": "17abaa74e599192f7650a4b89b6e9dfef2ff68cd"
		}`)

	var tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Username     string `json:"username"`
	}

	// TODO: un-dummy
	err = json.Unmarshal(dummyResponse, &tokenResponse)
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

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("User linked successfully! Please go back to the Discord bot for further instructions."))
}
