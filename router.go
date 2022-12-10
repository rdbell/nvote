package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func setRoutes(e *echo.Echo) {
	// Routes
	indexRoutes(e) // TODO: sitemap, favicon, opengraph, etc.?
	userRoutes(e)
	postRoutes(e)
	voteRoutes(e)
	channelRoutes(e)

	// Static assets
	fs := http.FileServer(http.Dir("./assets"))
	e.GET("/assets/*", echo.WrapHandler(http.StripPrefix("/assets/", fs)), addCacheHeaders)
}
