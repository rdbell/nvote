package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rdbell/nvote/schemas"

	"github.com/labstack/echo/v4"
)

// addCacheHeaders is a HandlerFunc middleware that adds cache headers to static asset GET requests
func addCacheHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set("cache-control", "public, max-age=2592000")
		c.Response().Header().Set("etag", `"`+cacheBuster+`"`)
		return next(c)
	}
}

// httpsRedir pre-middleware ensures the user is accessing the server via HTTPS and non-www hostname
func httpsRedir(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		/*
		   // Debug Headers
		   for name, headers := range c.Request().Header {
		       name = strings.ToLower(name)
		       for _, h := range headers {
		           log.Info(fmt.Sprintf("%v: %v", name, h))
		       }
		   }
		*/

		// Catch and redirect www
		host := c.Request().Host[:4]
		if host == "www." {
			target := "https://" + c.Request().Host[4:] + c.Request().URL.String()
			c.Redirect(http.StatusFound, target)
			return nil
		}

		// Catch and redirect non-HTTPS
		port := c.Request().Header.Get("x-forwarded-port")
		if port == "80" {
			target := "https://" + c.Request().Host + c.Request().URL.String()

			c.Redirect(http.StatusFound, target)
			return nil
		}

		return next(c)
	}
}

// setupContext middleware adds user-related vars and response headers to the context
func setupContext(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Send Server header value
		c.Response().Header().Set(echo.HeaderServer, "Nvote Server/0.1")

		cookie, err := c.Cookie("user")
		if err != nil || cookie.Value == "" {
			// Bad cookie data. Clear cookie
			if cookie != nil {
				clearCookie(c, "user")
			}

			// User cookie doesn't exist. Set empty user
			c.Set("user", schemas.LoggedOutUser())
			return next(c)
		}

		// Replace single quotes https://github.com/golang/go/issues/18627
		cookie.Value = strings.Replace(cookie.Value, "'", "\"", -1)

		// Decode user
		user := &schemas.User{}
		err = json.Unmarshal([]byte(cookie.Value), &user)

		// Clear user cookie if unmarshal fails
		if err != nil || user.PubKey == "" || user.PrivKey == "" {
			clearCookie(c, "user")

			// Set empty user
			c.Set("user", schemas.LoggedOutUser())
			return next(c)
		}

		// Add to context
		c.Set("user", user)

		return next(c)
	}
}

// isLoggedOut middleware ensures a user is logged out and redirects to index page if logged in
func isLoggedOut(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		user := c.Get("user").(*schemas.User)

		if user.PubKey == "" || user.PrivKey == "" {
			return next(c)
		}

		return c.Redirect(http.StatusFound, "/")
	}
}

// isLoggedIn middleware ensures a user is logged in and redirects to login page if not logged in
func isLoggedIn(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		user := c.Get("user").(*schemas.User)

		if user.PrivKey == "" || user.PubKey == "" {
			return c.Redirect(http.StatusFound, "/login")
		}

		return next(c)
	}
}

// isVerified middleware ensures a user is registered with the relay
func isVerified(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Skip this middleware if no registration-check URL is defined
		if appConfig.CheckVerifiedBaseURL == "" {
			return next(c)
		}

		user := c.Get("user").(*schemas.User)
		response, err := http.Get(appConfig.CheckVerifiedBaseURL + "/" + user.PubKey)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}

		defer response.Body.Close()
		contents, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}

		if strings.Contains(string(contents), "pubkey registered at timestamp") {
			// User is registered, continue
			return next(c)
		}

		// User is not registered, go to verify page
		return c.Redirect(http.StatusFound, "/verify")
	}
}

// isNotVerified middleware ensures a user is registered with the relay
func isNotVerified(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Skip this middleware if no registration-check URL is defined
		if appConfig.CheckVerifiedBaseURL == "" {
			return c.Redirect(http.StatusFound, "/")
		}

		user := c.Get("user").(*schemas.User)
		response, err := http.Get(appConfig.CheckVerifiedBaseURL + "/" + user.PubKey)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}

		defer response.Body.Close()
		contents, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}

		if strings.Contains(string(contents), "pubkey registered at timestamp") {
			// User is already registered, go back to index
			return c.Redirect(http.StatusFound, "/")
		}

		// User not registered, continue
		return next(c)
	}
}

// addXFrameOptionsHeader adds X-Frame-Options: DENY to prevent iframe clickjacking
func addXFrameOptionsHeader(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderXFrameOptions, "DENY")
		return next(c)
	}
}

// httpErrorHandler is a custom HTTP error responder
func httpErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	if he, ok := err.(*echo.HTTPError); ok {
		type errorSchema struct {
			Code    int
			Message string
		}

		code = he.Code
		pd := new(pageData).Init(c)
		pd.Title = fmt.Sprintf("Error - %d", code)
		re := regexp.MustCompile(`code=.*, message=`)
		readableError := re.ReplaceAllString(err.Error(), "")
		pd.Page = &errorSchema{code, readableError}
		c.Render(code, "base:error", pd)
	}

}

// setCookie sets a browser cookie
func setCookie(c echo.Context, name string, value string, expiration time.Time) {
	// Build cookie
	cookie := new(http.Cookie)
	cookie.Name = name
	cookie.Value = value
	cookie.Path = "/"
	cookie.Expires = expiration

	// Default cookie expiration 10 years
	if expiration.IsZero() {
		cookie.Expires = time.Now().Add(24 * time.Hour * 365 * 10)
	}

	// Replace double quotes https://github.com/golang/go/issues/18627
	cookie.Value = strings.Replace(cookie.Value, "\"", "'", -1)

	// Set the cookie in the provided context
	c.SetCookie(cookie)
}

// clearCookie unsets a browser cookie
func clearCookie(c echo.Context, name string) {
	cookie := new(http.Cookie)
	cookie.Name = name
	cookie.Value = ""
	cookie.Path = "/"
	cookie.Expires = time.Unix(0, 0)
	c.SetCookie(cookie)
}
