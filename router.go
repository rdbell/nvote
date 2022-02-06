package main

import (
	"net/http"

	"github.com/gobuffalo/packr/v2"
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
	box := packr.New("AssetsBox", "./assets")
	fs := http.FileServer(box)
	e.GET("/assets/*", echo.WrapHandler(http.StripPrefix("/assets/", fs)), addCacheHeaders)
}
