package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/rdbell/nvote/schemas"

	"github.com/labstack/echo/v4"
	"github.com/rdbell/go-nostr"
	"github.com/rdbell/go-nostr/nip06"
)

// userRoutes sets up auth/account-related routes
func userRoutes(e *echo.Echo) {
	e.POST("/logout", isLoggedIn(logoutHandler))
	e.GET("/login", isLoggedOut(loginHandler))
	e.GET("/alt_login", isLoggedOut(altLoginHandler))
	e.POST("/login", isLoggedOut(loginSubmitHandler))
	e.GET("/settings", isLoggedIn(settingsHandler))
	e.POST("/settings", isLoggedIn(settingsSubmitHandler))
	e.GET("/verify", isLoggedIn(isNotVerified(verifyHandler)))
	e.GET("/u/:pubkey", activityHandler)
}

// logoutHandler logs a user out and redirects to the home page
func logoutHandler(c echo.Context) error {
	clearCookie(c, "user")
	return c.Redirect(http.StatusFound, "/")
}

// verifyHandler serves the page for registering a user's pubkey with the nostr relay
func verifyHandler(c echo.Context) error {
	pd := new(pageData).Init(c)
	pd.Title = "Veriy Account"
	return c.Render(http.StatusOK, "base:verify", pd)
}

// loginHandler serves the login page
func loginHandler(c echo.Context) error {
	var page struct {
		SuggestedSeed string
	}

	// Generate suggested seed
	var err error
	page.SuggestedSeed, err = nip06.GenerateSeedWords()
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	pd := new(pageData).Init(c)
	pd.Page = page
	pd.Title = "Login"
	return c.Render(http.StatusOK, "base:login", pd)
}

// altLoginHandler serves the alternative login page
func altLoginHandler(c echo.Context) error {
	pd := new(pageData).Init(c)
	pd.Title = "Login"
	return c.Render(http.StatusOK, "base:alt_login", pd)
}

// settingsHandler serves the settings page
func settingsHandler(c echo.Context) error {
	pd := new(pageData).Init(c)
	pd.Title = "Settings"
	return c.Render(http.StatusOK, "base:settings", pd)
}

// loginSubmitHandler sets a user cookie in the browser
func loginSubmitHandler(c echo.Context) error {
	// Read form data
	login := &schemas.Login{}
	if err := c.Bind(login); err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	privkey, err := login.GeneratePrivateKey()
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Derive private key and set auth cookie
	pubkey, err := nostr.GetPublicKey(privkey)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	user := schemas.LoggedOutUser()
	user.PubKey = pubkey
	user.PrivKey = privkey

	userJSON, err := json.Marshal(user)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Save cookie
	setCookie(c, "user", string(userJSON), time.Time{})
	return c.Redirect(http.StatusFound, "/settings")
}

// settingsSubmitHandler sets a user cookie in the browser
func settingsSubmitHandler(c echo.Context) error {
	// Read form data
	user := &schemas.User{}
	if err := c.Bind(user); err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Ensure private/public keys are still valid
	pubkey, err := nostr.GetPublicKey(user.PrivKey)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}
	if user.PubKey != pubkey {
		return serveError(c, http.StatusInternalServerError, errors.New("invalid user object - try logging out"))
	}

	// Query for existing alias
	a, _ := aliasForPubKey(user.PubKey)

	// Upsert alias if changed
	if (a != nil && user.Alias != a.Name) || (a == nil && user.Alias != "") {
		alias := &schemas.Alias{
			PubKey: user.PubKey,
			Name:   user.Alias,
		}

		// Validate new alias
		alias.PrepareForPublish()
		if !alias.IsValid() {
			return serveError(c, http.StatusInternalServerError, errors.New("invalid alias"))
		}

		// Serialize data
		content, err := json.Marshal(alias)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}

		// Publish alias update event
		_, err = publishEvent(c, content, nostr.KindSetMetadata)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}
	}

	// Save cookie
	user.Alias = ""
	userJSON, err := json.Marshal(user)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}
	setCookie(c, "user", string(userJSON), time.Time{})
	return c.Redirect(http.StatusFound, "/")
}

// aliasForPubkey queries the DB and returns a *schemas.Alias for a given pubkey
func aliasForPubKey(pubkey string) (*schemas.Alias, error) {
	alias := &schemas.Alias{}

	var name string
	err := db.QueryRow(`SELECT name FROM aliases WHERE pubkey = ?`, pubkey).Scan(&name)
	if err != nil {
		return nil, err
	}

	alias.PubKey = pubkey
	alias.Name = name

	return alias, nil

}

// upsertAlias upserts an alias into the DB
func upsertAlias(alias *schemas.Alias) error {
	// Sanitize before upsert
	alias.Sanitize()

	// Validate
	if !alias.IsValid() {
		return errors.New("invalid alias")
	}

	// Add to DB
	_, err := db.Exec(`INSERT INTO aliases(pubkey, name) VALUES(?,?) ON CONFLICT (pubkey) DO UPDATE SET name=?`, alias.PubKey, alias.Name, alias.Name)
	if err != nil {
		return err
	}
	return nil
}
