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
	e.GET("/explore", exploreHandler)
}

// indexHandler serves the index page
func indexHandler(c echo.Context) error {
	return viewPostsHandler(c)
}

// exploreHandler serves a list of top channels
func exploreHandler(c echo.Context) error {
	type channel struct {
		Name  string
		Count int
	}

	var page struct {
		Channels []*channel
	}

	// Query channel top-level post counts
	// TODO: consider keeping a separate 'channels' table with columns like 'channel', 'post_count', 'comment_count' and query against that instead
	rows, err := db.Query(`SELECT DISTINCT(channel), COUNT(channel) AS cnt FROM posts WHERE parent = '' GROUP BY channel ORDER BY cnt DESC`)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	for rows.Next() {
		ch := &channel{}
		err = rows.Scan(&ch.Name, &ch.Count)
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}
		page.Channels = append(page.Channels, ch)
	}

	pd := new(pageData).Init(c)
	pd.Title = "Explore"
	pd.Page = page
	return c.Render(http.StatusOK, "base:explore", pd)
}

// activityHandler serves a list of recent activity
func activityHandler(c echo.Context) error {
	var page struct {
		PubKey    string
		Posts     []*schemas.Post
		Comments  []*schemas.Post
		Votes     []*schemas.Vote
		Channel   string
		UserVotes []*schemas.Vote
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
		Limit:         20,
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
		Limit:         20,
	})
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Recent votes
	page.Votes, err = fetchVotes(&schemas.VoteFilterset{
		PubKey:        page.PubKey,
		Channel:       page.Channel,
		OrderByColumn: "created_at",
		Limit:         100,
	})
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Fetch all votes for this user, to disable votes for posts that have already been voted on
	if c.Get("user").(*schemas.User).PubKey != "" {
		var err error
		page.UserVotes, err = fetchVotes(&schemas.VoteFilterset{
			PubKey: c.Get("user").(*schemas.User).PubKey,
			// TODO: add limit?
		})
		if err != nil {
			return serveError(c, http.StatusInternalServerError, err)
		}
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
