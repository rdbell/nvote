package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/rdbell/nvote/schemas"

	"github.com/fiatjaf/go-nostr"
	"github.com/labstack/echo/v4"
)

// postRoutes sets up post-related routes
func postRoutes(e *echo.Echo) {
	e.GET("/new", isLoggedIn(isVerified(newPostHandler)))
	e.POST("/new", isLoggedIn(isVerified(newPostSubmitHandler)))
	e.GET("/p/:id", viewSinglePostHandler)
	e.GET("/p/:parent/reply", isLoggedIn(isVerified(newPostHandler)))
}

// viewPostsHandler serves all posts, or the posts for a channel
func viewPostsHandler(c echo.Context) error {
	var page struct {
		Posts   []*schemas.Post
		Channel string
	}
	page.Channel = c.Param("channel")

	// Fetch posts ordered by ranking
	// TODO: include Limit param here after implementing pagination
	var err error
	page.Posts, err = fetchPosts(&schemas.PostFilterset{
		Channel:       page.Channel,
		PostType:      schemas.PostTypePosts,
		HideBadUsers:  c.Get("user").(*schemas.User).HideBadUsers,
		OrderByColumn: "ranking",
	})

	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	pd := new(pageData).Init(c)
	pd.Title = pd.Config.Tagline
	pd.Page = page
	return c.Render(http.StatusOK, "base:index", pd)
}

// fetchPosts fetches posts for a given set of filters
func fetchPosts(filters *schemas.PostFilterset) ([]*schemas.Post, error) {
	// TODO: implement pagination
	// Post filters
	// "all" is a special catch-all channel. no need to filter by "all"
	channelStmt := ""
	pubkeyStmt := ""
	postTypeStmt := ""
	badUsersStmt := ""
	orderByStmt := ""
	limitStmt := ""
	if filters.Channel != "" && filters.Channel != "all" {
		channelStmt = " AND channel = $1"
	}
	if filters.PubKey != "" {
		pubkeyStmt = " AND pubkey = $2"
	}
	if filters.PostType == schemas.PostTypePosts {
		postTypeStmt = " AND parent = ''"
	} else if filters.PostType == schemas.PostTypeComments {
		postTypeStmt = " AND parent != ''"
	}
	if filters.OrderByColumn != "" {
		// OrderByColumn should never be set by a user's input, in order to prevent sql injection
		// but use a whitelist just in case this rule is every violated somewhere
		if filters.OrderByColumn != "created_at" &&
			filters.OrderByColumn != "score" &&
			filters.OrderByColumn != "ranking" {
			return nil, errors.New("invalid value for OrderedByColumn")
		}
		orderByStmt = fmt.Sprintf(" ORDER BY %s DESC", filters.OrderByColumn)
	}
	if filters.HideBadUsers {
		// TODO: is this value ok?
		badUsersStmt = " AND user_score > -20 "
	}
	if filters.Limit > 0 {
		limitStmt = fmt.Sprintf("LIMIT %d", filters.Limit)
	}

	// `LIKE "%"+pubKey` filter fetches all rows if pubkey is empty, but only rows related to the user if pubkey is populated
	// consider using something like CASE WHEN or COALESCE/NULLIF if it's more performant
	rows, err := db.Query(fmt.Sprintf(`
		SELECT id, score, children, pubkey, created_at, title, body, channel, parent
		FROM posts WHERE TRUE
		%s%s%s%s%s%s
	`, channelStmt, pubkeyStmt, postTypeStmt, badUsersStmt, orderByStmt, limitStmt), filters.Channel, filters.PubKey)
	if err != nil {
		return nil, err
	}

	var posts []*schemas.Post
	for rows.Next() {
		post := &schemas.Post{}
		err = rows.Scan(&post.ID, &post.Score, &post.Children, &post.PubKey, &post.CreatedAt, &post.Title, &post.Body, &post.Channel, &post.Parent)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	return posts, err
}

// newPostHandler serves the New Post page
func newPostHandler(c echo.Context) error {
	var page struct {
		Parent  string
		Channel string
	}
	page.Channel = c.Param("channel")

	pd := new(pageData).Init(c)
	pd.Title = "New Post"
	page.Parent = c.Param("parent")
	if page.Parent != "" {
		pd.Title = "Reply to Post"
	}
	pd.Page = page
	return c.Render(http.StatusOK, "base:new_post", pd)
}

// newPostSubmitHandler handles a new post submission
func newPostSubmitHandler(c echo.Context) error {
	// Read form data and validate post
	post := &schemas.Post{}
	if c.Bind(post) != nil || (!post.IsValidPost() && !post.IsValidComment()) {
		return serveError(c, http.StatusInternalServerError, errors.New("invalid post"))
	}

	// Format and serialize post
	post.PrepareForPublish()
	content, err := json.Marshal(post)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Publish
	event, err := publishEvent(c, content)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	return c.Redirect(http.StatusFound, fmt.Sprintf("/p/%s", event.ID))
}

// viewSinglePostHandler serves a single post
func viewSinglePostHandler(c echo.Context) error {
	// Ensure ID provided
	id := c.Param("id")
	if id == "" {
		return serveError(c, http.StatusInternalServerError, errors.New("invalid ID"))
	}

	var page struct {
		ID    string
		Posts []*schemas.Post
	}
	page.ID = id

	// Get post tree
	posts := getPostTree(page.ID, 0)
	for _, post := range posts {
		page.Posts = append(page.Posts, post)
	}

	if len(posts) == 0 {
		return serveError(c, http.StatusNotFound, errors.New("not found"))
	}

	pd := new(pageData).Init(c)
	pd.Title = page.Posts[0].Title
	pd.Page = page
	return c.Render(http.StatusOK, "base:view_post", pd)
}

// getPostTree recursively queries the DB to return a post and all of its children
// TODO: switch to WITH RECURSIVE ... SELECT?
func getPostTree(id string, depth int) []*schemas.Post {
	var posts []*schemas.Post

	// Get parent post
	// it shouldn't be hidden from them regardless of their settings flag
	post := &schemas.Post{}
	err := db.QueryRow(`SELECT id, score, children, pubkey, created_at, title, body, channel, parent FROM posts WHERE id = ?`, id).Scan(
		&post.ID, &post.Score, &post.Children, &post.PubKey, &post.CreatedAt, &post.Title, &post.Body, &post.Channel, &post.Parent,
	)

	if err != nil {
		return nil
	}
	if depth == 0 {
		posts = append(posts, post)
	}

	rows, err := db.Query(fmt.Sprintf(`SELECT id, score, children, pubkey, created_at, title, body, parent FROM posts WHERE parent = ? ORDER BY ranking DESC`), post.ID)
	if err != nil {
		return posts
	}

	for rows.Next() {
		post := &schemas.Post{}
		err = rows.Scan(&post.ID, &post.Score, &post.Children, &post.PubKey, &post.CreatedAt, &post.Title, &post.Body, &post.Parent)
		posts = append(posts, post)

		// Also get child's children
		posts = append(posts, getPostTree(post.ID, depth+1)...)
	}

	return posts
}

// insertPost inserts a post event into the DB
func insertPost(e *nostr.Event) error {
	// Parse post
	post := &schemas.Post{}
	err := json.Unmarshal([]byte(e.Content), post)
	if err != nil {
		return err
	}

	// Don't insert invalid posts
	if !post.IsValidPost() && !post.IsValidComment() {
		return errors.New("invalid post")
	}

	post.ID = e.ID
	post.CreatedAt = e.CreatedAt
	post.PubKey = e.PubKey

	// Fill channel field for replies
	if post.IsValidComment() {
		post.Channel = getTopParentChannel(post.Parent)
	}

	post.Sanitize()

	// Query poster's information
	userScore := 0
	err = db.QueryRow(`SELECT user_score FROM users WHERE pubkey = ?`, post.PubKey).Scan(&userScore)
	if err != nil {
		// User doesn't exist in users table yet. Insert
		_, err := db.Exec(`INSERT INTO users(pubkey, user_score) VALUES(?,?)`, post.PubKey, 0)
		if err != nil {
			return err
		}
	}

	// Add to DB
	_, err = db.Exec(`INSERT INTO posts(id, score, user_score, ranking, children, pubkey, created_at, title, body, channel, parent) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, post.ID, 0, userScore, reddit(post.Score, post.CreatedAt), 0, post.PubKey, post.CreatedAt, post.Title, post.Body, post.Channel, post.Parent)
	if err != nil {
		return err
	}

	// Update parent's children count
	if post.Parent != "" {
		updateChildrenCounts(post.Parent)
	}

	return nil
}

// getTopParentChannel recursively queries the DB to find the top-level post's channel
// TODO: switch to WITH RECURSIVE ... SELECT?
func getTopParentChannel(parent string) string {
	grandparent := ""
	channel := ""
	err := db.QueryRow(`SELECT parent, channel FROM posts WHERE id = ?`, parent).Scan(&grandparent, &channel)
	if err != nil {
		return ""
	}

	if grandparent != "" {
		getTopParentChannel(grandparent)
	}

	return channel
}

// updateChildrenCounts recursvely updates childen counts for parent posts
// TODO: switch to WITH RECURSIVE ... UPDATE?
func updateChildrenCounts(parent string) {
	grandparent := ""
	db.QueryRow(`UPDATE posts SET children = children + 1 WHERE id = ? RETURNING parent`, parent).Scan(&grandparent)
	if grandparent != "" {
		updateChildrenCounts(grandparent)
	}
}
