package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/rdbell/nvote/schemas"

	"github.com/fiatjaf/go-nostr"
	"github.com/labstack/echo/v4"
)

// voteRoutes sets up vote-related routes
func voteRoutes(e *echo.Echo) {
	e.POST("/vote/:id", isLoggedIn(isVerified(voteSubmitHandler)))
}

// voteSubmitHandler handles an upvote/downvote
func voteSubmitHandler(c echo.Context) error {
	// Read form data
	vote := &schemas.Vote{}
	if c.Bind(vote) != nil || !vote.IsValid() {
		return serveError(c, http.StatusInternalServerError, errors.New("invalid vote data"))
	}

	if alreadyVoted(vote.Target, c.Get("user").(*schemas.User).PubKey) {
		return serveError(c, http.StatusUnauthorized, errors.New("you have already voted on this post"))
	}

	// Serialize content
	vote.StripForPublish()
	content, err := json.Marshal(vote)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	// Publish
	_, err = publishEvent(c, content)
	if err != nil {
		return serveError(c, http.StatusInternalServerError, err)
	}

	return c.Redirect(http.StatusFound, fmt.Sprintf("/p/%s", vote.Target))
}

// alreadyVoted checks to see if a pubkey has already voted on a post
func alreadyVoted(target string, pubkey string) bool {
	var result string
	err := db.QueryRow(`SELECT target FROM votes WHERE pubkey = ? AND target = ?`, pubkey, target).Scan(&result)
	if err == nil && result != "" {
		return true
	}
	return false
}

// insertVote inserts a vote event into the DB
func insertVote(event *nostr.Event) error {
	// Parse vote
	vote := &schemas.Vote{}
	err := json.Unmarshal([]byte(event.Content), vote)
	if err != nil {
		return err
	}

	// Ensure valid vote (no target, already voted)
	if !vote.IsValid() || alreadyVoted(vote.Target, event.PubKey) {
		return errors.New("invalid vote")
	}

	// Add to DB
	_, err = db.Exec(`INSERT INTO votes(pubkey, target, direction, created_at) VALUES(?,?,?,?)`, event.PubKey, vote.Target, vote.Direction, event.CreatedAt)
	if err != nil {
		return err
	}

	// Add to post score
	direction := -1
	if vote.Direction == true {
		direction = 1
	}

	var createdAt uint32
	var score int32
	var postPubkey string
	err = db.QueryRow(`UPDATE posts SET score = score + ? WHERE id = ? RETURNING created_at, score, pubkey`, direction, vote.Target).Scan(&createdAt, &score, &postPubkey)
	if err != nil {
		return err
	}

	// Update post ranking
	// Would like to add this to the previous statement but can't calculate post ranking in a SQLite Query because sqlite3 driver isn't compiled with math functions enabled
	ranking := reddit(score, createdAt)
	_, err = db.Exec(`UPDATE posts SET ranking = ? WHERE id = ?`, ranking, vote.Target)
	if err != nil {
		return err
	}

	// Update post owner's user_score
	// TODO: user_score should be more generous to new users, to prevent a user from being marked as "bad" if they make their first post and get downvoted immediately?
	_, err = db.Exec(`UPDATE users SET user_score = user_score + ? WHERE pubkey = ?`, direction, postPubkey)
	if err != nil {
		return err
	}

	return nil
}

// fetchVotes fetches votes for a given set of filters
func fetchVotes(filters *schemas.VoteFilterset) ([]*schemas.Vote, error) {
	pubkeyStmt := ""
	orderByStmt := ""
	limitStmt := ""
	if filters.PubKey != "" {
		pubkeyStmt = " AND pubkey = $1"
	}
	if filters.Limit > 0 {
		limitStmt = fmt.Sprintf("LIMIT %d", filters.Limit)
	}
	if filters.OrderByColumn != "" {
		// OrderByColumn should never be set by a user's input, in order to prevent sql injection
		// but use a whitelist just in case this rule is every violated somewhere
		if filters.OrderByColumn != "created_at" {
			return nil, errors.New("invalid value for OrderedByColumn")
		}
		orderByStmt = fmt.Sprintf(" ORDER BY %s DESC", filters.OrderByColumn)
	}

	rows, err := db.Query(fmt.Sprintf(`
		SELECT pubkey, target, direction, created_at
		FROM votes
		WHERE TRUE
		%s%s%s
	`, pubkeyStmt, orderByStmt, limitStmt), filters.PubKey)
	if err != nil {
		return nil, err
	}

	var votes []*schemas.Vote
	for rows.Next() {
		vote := &schemas.Vote{}
		err = rows.Scan(&vote.PubKey, &vote.Target, &vote.Direction, &vote.CreatedAt)
		votes = append(votes, vote)
	}

	return votes, err
}

// reddit style ranking
// https://github.com/anhle128/go-ranking-algorithms
func reddit(score int32, createdAt uint32) float64 {
	var sign float64
	order := math.Log10(math.Max(math.Abs(float64(score)), 1))
	if score > 0 {
		sign = 1
	} else if score < 0 {
		sign = -1
	} else {
		sign = 0
	}
	seconds := float64(createdAt) - 1134028003
	return round(sign*order+seconds/45000, 7)
}

func round(val float64, prec int) float64 {
	var rounder float64
	intermed := val * math.Pow(10, float64(prec))

	if val >= 0.5 {
		rounder = math.Ceil(intermed)
	} else {
		rounder = math.Floor(intermed)
	}
	return rounder / math.Pow(10, float64(prec))
}
