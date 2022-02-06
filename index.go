package main

import (
	"net/http"

	"github.com/rdbell/nvote/schemas"

	"github.com/labstack/echo/v4"
)

// indexRoutes sets up misc top-level routes
func indexRoutes(e *echo.Echo) {
	e.GET("/", indexHandler)
	e.GET("/about", aboutHandler)
	e.GET("/recent", activityHandler)
}

// indexHandler serves the index page
func indexHandler(c echo.Context) error {
	return viewPostsHandler(c)
}

// activityHandler serves a list of recent activity
func activityHandler(c echo.Context) error {
	// TODO: activity page should show userScore if requesting activity for a user?
	var page struct {
		PubKey   string
		Posts    []*schemas.Post
		Comments []*schemas.Post
		Votes    []*schemas.Vote
		Channel  string
	}

	page.PubKey = c.Param("pubkey")
	page.Channel = c.Param("channel")

	// Channel filter
	// "all" is a special catch-all channel. no need to filter by "all"

	// Recent posts
	var err error
	page.Posts, err = fetchPosts(&schemas.PostFilterset{
		Channel:       page.Channel,
		PubKey:        page.PubKey,
		PostType:      schemas.PostTypePosts,
		OrderByColumn: "created_at",
	})
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Recent comments
	page.Comments, err = fetchPosts(&schemas.PostFilterset{
		Channel:       page.Channel,
		PubKey:        page.PubKey,
		PostType:      schemas.PostTypeComments,
		OrderByColumn: "created_at",
	})
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Recent votes
	// TODO: this always returns all recent votes. should be able to return votes for a specified channel
	page.Votes, err = fetchVotes(&schemas.VoteFilterset{
		PubKey:        page.PubKey,
		OrderByColumn: "created_at",
	})
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	pd := new(pageData).Init(c)
	pd.Title = "Recent Activity"
	pd.Page = page
	return c.Render(http.StatusOK, "base:recent", pd)
}

// aboutHandler serves the about page
func aboutHandler(c echo.Context) error {
	pd := new(pageData).Init(c)
	pd.Title = "About"
	return c.Render(http.StatusOK, "base:about", pd)
}

// serveError serves an error
func serveError(c echo.Context, code int, err error) error {
	var page struct {
		Code    int
		Message string
	}
	pd := new(pageData).Init(c)
	page.Code = code
	page.Message = err.Error()
	pd.Title = "Error"
	pd.Page = page
	return c.Render(page.Code, "base:error", pd)
}
