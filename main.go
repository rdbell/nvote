package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/rdbell/nvote/schemas"

	checkErr "github.com/rdbell/nvote/check"

	"github.com/gobuffalo/packr/v2"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rdbell/go-nostr"
)

var pool *nostr.RelayPool

func init() {
	rand.Seed(time.Now().UnixNano())

	// Configure app
	switch os.Getenv("NV_ENVIRONMENT") {
	case "prod":
		appConfig = readConfig("./config_prod.json")
		schemas.InitConfig(appConfig)
		break
	case "local":
		appConfig = readConfig("./config_local.json")
		schemas.InitConfig(appConfig)
		break
	default:
		appConfig = readConfig("./config_dev.json")
		schemas.InitConfig(appConfig)
		break
	}

	// Load templates
	box := packr.New("WebTemplatesBox", "./views")
	loadTemplates(box)

	// Init SQLite DB
	initSQLite()
	setupPostsTable()
	setupUsersTable()
	setupVotesTable()
	setupMetadataTable()

	go fetchEvents()
}

func main() {
	// Echo instance
	e := echo.New()

	// Middleware
	if appConfig.Environment == "dev" {
		e.Use(middleware.Logger())
	}
	e.Use(middleware.Recover())

	// Don't perform any re-routing in development environment
	if appConfig.Environment != "" && appConfig.Environment != "dev" {
		e.Pre(httpsRedir)
	}

	// Remove trailing slashes from URL paths
	e.Pre(middleware.RemoveTrailingSlash())

	// Enable CSRF protection
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "form:csrf",
		CookiePath:  "/",
	}))

	// Set up custom context with user-related vars and response headers
	e.Use(setupContext)

	// Add X-Frame-Options header
	e.Use(addXFrameOptionsHeader)

	// Enable GZIP compression
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{Level: 5}))

	// Set renderer
	e.Renderer = t

	// Error handler function
	e.HTTPErrorHandler = httpErrorHandler

	// Set routes
	setRoutes(e)

	// Start server
	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", appConfig.ListenPort)))
}

// readConfig reads a config file into an AppConfig struct
func readConfig(filePath string) *schemas.AppConfig {
	file, err := ioutil.ReadFile(filePath)
	checkErr.Panic(err)
	a := &schemas.AppConfig{}
	err = json.Unmarshal([]byte(file), &a)
	checkErr.Panic(err)
	return a
}
