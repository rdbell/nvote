package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
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

	// Query for existing metadata
	metadata, _ := metadataForPubkey(user.PubKey)

	// Upsert metadata if changed
	if (metadata != nil && user.Name != metadata.Name) || (metadata == nil && user.Name != "") || (metadata != nil && user.About != metadata.About) || (metadata == nil && user.About != "") {
		metadata := &schemas.Metadata{
			PubKey: user.PubKey,
			Name:   user.Name,
			About:  user.About,
		}

		// Validate new metadata
		metadata.PrepareForPublish()
		if !metadata.IsValid() {
			return serveError(c, http.StatusInternalServerError, errors.New("invalid metadata"))
		}

		// Serialize data
		content, err := json.Marshal(metadata)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}

		// Publish metadata update event
		_, err = publishEvent(c, content, nostr.KindSetMetadata, nil)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}
	}

	// Save cookie
	user.Name = ""  // don't need to save user.name client-side
	user.About = "" // don't need to save user.about client-side
	userJSON, err := json.Marshal(user)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}
	setCookie(c, "user", string(userJSON), time.Time{})
	return c.Redirect(http.StatusFound, "/")
}

// generatedUsername generates a random username based on a provided pubkey
func generatedUsername(pubkey string) string {
	// Ensure length
	if len(pubkey) < 15 {
		return "user"
	}

	// Use random name if no name provided
	// Convert pubkey string to integer from base16 hex
	// use [0:15] to prevent value out of range
	i, err := strconv.ParseUint(pubkey[0:15], 16, 64)
	if err != nil {
		return pubkey[0:8]
	}

	// New random source
	random := rand.New(rand.NewSource(int64(i)))

	w1 := bip39WordList[random.Intn(len(bip39WordList))]
	w2 := bip39WordList[random.Intn(len(bip39WordList))]
	randomNumber := random.Intn(9999)

	return fmt.Sprintf("%v%v%d", strings.Title(w1), strings.Title(w2), randomNumber)
}

// metadataForPubkey queries the DB and returns a *schemas.Metadata for a given pubkey
func metadataForPubkey(pubkey string) (*schemas.Metadata, error) {
	metadata := &schemas.Metadata{}
	metadata.PubKey = pubkey

	db.QueryRow(`SELECT SUM(score) FROM posts WHERE pubkey = ?`, pubkey).Scan(&metadata.UserScore)
	err := db.QueryRow(`SELECT name, about, created_at FROM metadata WHERE pubkey = ?`, pubkey).Scan(&metadata.Name, &metadata.About, &metadata.CreatedAt)
	if err != nil || metadata.Name == "" {
		metadata.Name = generatedUsername(pubkey)
	}

	return metadata, nil
}

// upsertMetadata upserts a user's metadata into the DB
func upsertMetadata(metadata *schemas.Metadata) error {
	// Sanitize before upsert
	metadata.Sanitize()

	// Validate
	if !metadata.IsValid() {
		return errors.New("invalid metadata")
	}

	// Add to DB
	_, err := db.Exec(`INSERT INTO metadata(pubkey, name, about, created_at) VALUES(?,?,?,?) ON CONFLICT (pubkey) DO UPDATE SET name=?, about=?, created_at=?`,
		metadata.PubKey, metadata.Name, metadata.About, metadata.CreatedAt, metadata.Name, metadata.About, metadata.CreatedAt)
	if err != nil {
		return err
	}
	return nil
}
