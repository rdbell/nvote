package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rdbell/go-nostr"
	"github.com/rdbell/nvote/schemas"

	"github.com/labstack/echo/v4"
)

// postRoutes sets up post-related routes
func postRoutes(e *echo.Echo) {
	e.GET("/new", isLoggedIn(isVerified(newPostHandler)))
	e.POST("/new", isLoggedIn(isVerified(newPostSubmitHandler)))
	e.POST("/new/preview", isLoggedIn(isVerified(newPostHandler)))
	e.GET("/p/:id", viewPostHandler)
	e.GET("/p/:parent/reply", isLoggedIn(isVerified(newPostHandler)))
	e.POST("/p/:id/delete", isLoggedIn(isVerified(deletePostHandler)))
	e.GET("/search", searchHandler)
}

// deletePostHandler deletes a post
func deletePostHandler(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return serveError(c, http.StatusInternalServerError, errors.New("invalid post"))
	}

	post, err := getPost(id)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}
	if post.PubKey != c.Get("user").(*schemas.User).PubKey {
		return serveError(c, http.StatusInternalServerError, errors.New("cannot delete another user's post"))
	}

	// Publish deletion event
	tags := nostr.Tags{nostr.Tag{"e", post.ID}}
	publishEvent(c, []byte{}, nostr.KindDeletion, tags)

	// Attempt to redirect the user back to where they came from, but only if that content still exists
	referer := c.Request().Header["Referer"]
	if len(referer) != 0 && strings.Contains(referer[0], appConfig.SiteURL) {
		u, err := stringToURL(referer[0])
		if err != nil || u.Path == "/p/"+id {
			target, err := getPost(id)
			if err != nil {
				return c.Redirect(http.StatusFound, "/")
			}
			return c.Redirect(http.StatusFound, "/p/"+target.ID)
		}
		return c.Redirect(http.StatusFound, u.Path)
	}

	// If all else fails, redirect to index
	return c.Redirect(http.StatusFound, "/")
}

// searchHandler serves a page of search results
func searchHandler(c echo.Context) error {
	// TODO: Offload search to a service like algolia later
	var page struct {
		Posts     []*schemas.Post
		Comments  []*schemas.Post
		UserVotes []*schemas.Vote
		Query     string
	}

	page.Query = c.FormValue("q")

	// Fetch posts ordered by recent
	// TODO: search within channel
	// TODO: paginate search results
	var err error
	page.Posts, err = fetchPosts(&schemas.PostFilterset{
		PostContains:  page.Query,
		PostType:      schemas.PostTypePosts,
		HideBadUsers:  c.Get("user").(*schemas.User).HideBadUsers,
		OrderByColumn: "created_at",
		Limit:         appConfig.PostsPerPage * 2,
	})

	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Fetch comments ordered by recent
	// TODO: search within channel
	// TODO: paginate search results
	page.Comments, err = fetchPosts(&schemas.PostFilterset{
		PostContains:  page.Query,
		PostType:      schemas.PostTypeComments,
		HideBadUsers:  c.Get("user").(*schemas.User).HideBadUsers,
		OrderByColumn: "created_at",
		Limit:         appConfig.PostsPerPage * 2,
	})

	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Fetch all votes for this user, to disable votes for posts that have already been voted on
	// TODO: DRY this into a single function
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
	pd.Title = pd.Config.Tagline
	pd.Page = page
	return c.Render(http.StatusOK, "base:search", pd)
}

// viewPostsHandler serves all posts, or the posts for a channel
func viewPostsHandler(c echo.Context) error {
	var page struct {
		Posts     []*schemas.Post
		Channel   string
		Page      int
		UserVotes []*schemas.Vote
	}

	page.Channel = c.Param("channel")
	page.Page, _ = strconv.Atoi(c.FormValue("page"))

	// Sanitize page number
	if page.Page < 0 {
		page.Page = 0
	}

	// Fetch posts ordered by ranking
	var err error
	page.Posts, err = fetchPosts(&schemas.PostFilterset{
		Channel:       page.Channel,
		PostType:      schemas.PostTypePosts,
		HideBadUsers:  c.Get("user").(*schemas.User).HideBadUsers,
		Page:          page.Page,
		OrderByColumn: "ranking",
		Limit:         appConfig.PostsPerPage,
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
	pd.Title = pd.Config.Tagline
	pd.Page = page
	return c.Render(http.StatusOK, "base:index", pd)
}

// fetchPosts fetches posts for a given set of filters
func fetchPosts(filters *schemas.PostFilterset) ([]*schemas.Post, error) {
	// Post filters
	// "all" is a special catch-all channel. no need to filter by "all"
	channelStmt := " AND $1 = $1"
	pubkeyStmt := " AND $2 = $2"
	postContainsStmt := " AND $3 = $3"
	postTypeStmt := ""
	badUsersStmt := ""
	pageStmt := ""
	orderByStmt := ""
	limitStmt := ""
	if filters.Channel != "" && filters.Channel != "all" {
		channelStmt = " AND channel = $1"
	}
	if filters.PubKey != "" {
		pubkeyStmt = " AND pubkey = $2"
	}
	var pc1, pc2, pc3, pc4 string
	if filters.PostContains != "" {
		// TODO: look for a better approach to content search
		// maybe something like `" " + body + " " LIKE $3`
		pc1 = filters.PostContains + " %"         // whole words at the beginning
		pc2 = "% " + filters.PostContains         // whole words at the end
		pc3 = "% " + filters.PostContains + " %"  // whole words in the middle
		pc4 = "%\n" + filters.PostContains + " %" // whole words after a line break
		postContainsStmt = " AND ((title != '' AND (title = $3 OR title LIKE $4 OR title LIKE $5 OR title LIKE $6)) OR (body = $3 OR body LIKE $4 OR body LIKE $5 OR body LIKE $6 OR body LIKE $7))"
	}
	if filters.PostType == schemas.PostTypePosts {
		postTypeStmt = " AND parent = ''"
	} else if filters.PostType == schemas.PostTypeComments {
		postTypeStmt = " AND parent != ''"
	}
	if filters.OrderByColumn != "" {
		// OrderByColumn should never be set by a user's input, in order to prevent sql injection
		// but use a whitelist just in case this rule is ever violated somewhere
		if filters.OrderByColumn != "created_at" &&
			filters.OrderByColumn != "score" &&
			filters.OrderByColumn != "ranking" {
			return nil, errors.New("invalid value for OrderedByColumn")
		}
		orderByStmt = fmt.Sprintf(" ORDER BY %s DESC", filters.OrderByColumn)
	}
	if filters.HideBadUsers {
		badUsersStmt = " AND user_score > -20 "
	}
	if filters.Limit > 0 {
		limitStmt = fmt.Sprintf(" LIMIT %d", filters.Limit)
	}
	if filters.Page > 0 {
		pageStmt = fmt.Sprintf(" OFFSET %d", filters.Page*appConfig.PostsPerPage)
	}

	rows, err := db.Query(fmt.Sprintf(`
		SELECT id, score, children, pubkey, created_at, title, body, channel, parent
		FROM posts WHERE TRUE
		%s%s%s%s%s%s%s%s
	`, channelStmt, pubkeyStmt, postContainsStmt, postTypeStmt, badUsersStmt, orderByStmt, limitStmt, pageStmt), filters.Channel, filters.PubKey, filters.PostContains, pc1, pc2, pc3, pc4)
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
		Post      *schemas.Post
		Parent    *schemas.Post
		UserVotes []*schemas.Vote
	}
	page.Post = &schemas.Post{}
	page.Post.Channel = c.Param("channel")
	page.Parent = &schemas.Post{}

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

	// Handle POST request for post preview
	if c.Bind(page.Post) != nil || (!page.Post.IsValidPost() && !page.Post.IsValidComment()) {
		page.Post = &schemas.Post{}
	}

	parentID := c.Param("parent")
	if page.Post.Parent != "" {
		parentID = page.Post.Parent
	}

	pd := new(pageData).Init(c)
	pd.Title = "New Post"

	// Fill parent info
	if parentID != "" {
		pd.Title = "Reply to Post"
		var err error
		page.Parent, err = getPost(parentID)
		if err != nil || page.Parent == nil {
			return serveError(c, http.StatusNotFound, errors.New("not found"))
		}
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
	event, err := publishEvent(c, content, nostr.KindTextNote, nil)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Also submit vote for own post
	vote := &schemas.Vote{
		Target:    event.ID,
		Direction: true,
	}
	content, err = json.Marshal(vote)
	if err != nil {
		return c.Redirect(http.StatusFound, fmt.Sprintf("/p/%s", event.ID))
	}

	// Publish
	_, err = publishEvent(c, content, nostr.KindTextNote, nil)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Get top-level parent
	op, err := getOP(post.Parent)
	if err != nil || op == nil {
		// Redirect to the new post
		return c.Redirect(http.StatusFound, fmt.Sprintf("/p/%s", event.ID))
	}

	// Redirect to top-level parent
	return c.Redirect(http.StatusFound, fmt.Sprintf("/p/%s", op.ID))
}

// viewPostHandler serves a single post and its children
func viewPostHandler(c echo.Context) error {
	// Ensure ID provided
	id := c.Param("id")
	if id == "" {
		return serveError(c, http.StatusInternalServerError, errors.New("invalid ID"))
	}

	var page struct {
		ID        string
		Posts     []*schemas.Post
		UserVotes []*schemas.Vote
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
	pd.Title = page.Posts[0].Title
	pd.Page = page
	return c.Render(http.StatusOK, "base:view_post", pd)
}

// getPost queries the DB to return a single post with a specified ID
func getPost(id string) (*schemas.Post, error) {
	// Get post
	post := &schemas.Post{}
	err := db.QueryRow(`SELECT id, score, children, pubkey, created_at, title, body, channel, parent FROM posts WHERE id = ?`, id).Scan(
		&post.ID, &post.Score, &post.Children, &post.PubKey, &post.CreatedAt, &post.Title, &post.Body, &post.Channel, &post.Parent,
	)

	if err != nil {
		return nil, err
	}

	return post, nil
}

// getPostTree recursively queries the DB to return a post and all of its children
// TODO: switch to WITH RECURSIVE ... SELECT?
func getPostTree(id string, depth int) []*schemas.Post {
	var posts []*schemas.Post

	// Get parent post
	if depth == 0 {
		post, err := getPost(id)
		if err != nil || post == nil {
			return nil
		}
		posts = append(posts, post)
	}

	// TODO: change this to 'ORDER BY ranking' later when there's more activity
	rows, err := db.Query(fmt.Sprintf(`SELECT id, score, children, pubkey, created_at, title, body, parent FROM posts WHERE parent = ? ORDER BY score DESC`), id)
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

// insertPost inserts a post into the DB
func insertPost(post *schemas.Post) error {
	// Fill channel field for replies
	if post.IsValidComment() {
		post.Channel = ""
		parent, err := getOP(post.Parent)
		if err == nil {
			post.Channel = parent.Channel
		}
	}

	// Sanitize before insert
	post.Sanitize()

	// Query poster's information
	userScore := 0
	err := db.QueryRow(`SELECT user_score FROM users WHERE pubkey = ?`, post.PubKey).Scan(&userScore)
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

// deletePost delets a post from the local cache
func deletePost(event *nostr.Event) error {
	if len(event.Tags) < 1 || len(event.Tags[0]) < 1 {
		return errors.New("invalild request")
	}

	for _, target := range event.Tags {
		// Validate target
		if len(target) < 2 {
			break
		}
		if target[0] != "e" {
			break
		}
		if target[1] == "" {
			break
		}

		// delete targets
		db.Exec(`DELETE FROM posts WHERE pubkey = ? AND id = ?`, event.PubKey, event.Tags[0][1].(string))

		// TODO: update parent reply count recursively?
	}

	return nil
}

// getOP recursively queries the DB to find the top-level post
// TODO: switch to WITH RECURSIVE ... SELECT?
func getOP(id string) (*schemas.Post, error) {
	grandparent := ""
	err := db.QueryRow(`SELECT parent FROM posts WHERE id = ?`, id).Scan(&grandparent)
	if err != nil {
		return nil, err
	}

	if grandparent != "" {
		return getOP(grandparent)
	}

	post, err := getPost(id)

	return post, nil
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
