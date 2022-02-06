package main

import "github.com/labstack/echo/v4"

// channelRoutes sets up channel-related routes
func channelRoutes(e *echo.Echo) {
	e.GET("/c/:channel", viewPostsHandler)
	e.GET("/c/:channel/new", newPostHandler)
	e.GET("/c/:channel/recent", activityHandler)
}
