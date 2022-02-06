package schemas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/fiatjaf/go-nostr/nip06"
	"github.com/microcosm-cc/bluemonday"
)

var appConfig *AppConfig

// InitConfig inits the config variable
func InitConfig(a *AppConfig) {
	appConfig = a
}

// Post defines a post structure
type Post struct {
	ID        string `json:"id,omitempty" form:"id"`                 // nostr event's ID
	Score     int32  `json:"score,omitempty" form:"id"`              // post's score
	Children  int32  `json:"children,omitempty" form:"children"`     // number of children
	PubKey    string `json:"pubkey,omitempty" form:"pubkey"`         // poster's public key
	CreatedAt uint32 `json:"created_at,omitempty" form:"created_at"` // creation timestamp
	Title     string `json:"title,omitempty" form:"title"`           // post's title
	Body      string `json:"body,omitempty" form:"body"`             // post's body
	Channel   string `json:"channel,omitempty" form:"channel"`       // post's channel
	Parent    string `json:"parent,omitempty" form:"parent"`         // parent post's nostr event ID
}

// IsValidPost ensures that a post looks valid for submission
func (post *Post) IsValidPost() bool {
	// Replies should have title/body
	if post.Title == "" || post.Body == "" {
		return false
	}

	return true
}

// IsValidComment ensures that a reply looks valid for submission
func (post *Post) IsValidComment() bool {
	// Replies should have body/parent
	if post.Body == "" {
		return false
	}
	if post.Parent == "" {
		return false
	}

	return true
}

// PrepareForPublish strips superflous parameters to prepare for publishing (omitempty)
// this is mainly to reduce nostr event content size
// clients shouldn't assume all post events received from relays have superflous parameters stripped
func (post *Post) PrepareForPublish() {
	post.ID = ""
	post.Score = 0
	post.Children = 0
	post.PubKey = ""
	post.CreatedAt = 0

	// Format replies
	if post.IsValidComment() {
		post.Title = ""
		post.Channel = ""
	}

	// Format top-level posts
	if post.IsValidPost() {
		post.Parent = ""
	}

	// Sanitize post data
	post.Sanitize()

	return
}

// Sanitize sanitizes the posts fields to prepare for publishing and DB insertion
func (post *Post) Sanitize() {
	// "all" is a special catch-all channel. Don't need to include the param
	if post.Channel == "all" {
		post.Channel = ""
	}

	// Only allow alphanumeric in channel
	// TODO: consider allowing underscores/dashes?
	reg, err := regexp.Compile("[^a-zA-Z0-9]+")
	if err != nil {
		post.Channel = ""
	}
	post.Channel = strings.ToLower(reg.ReplaceAllString(post.Channel, ""))

	// Strip HTML from title and body
	post.Title = bluemonday.StrictPolicy().Sanitize(post.Title)
	post.Body = bluemonday.StrictPolicy().Sanitize(post.Body)

	// Enforce title limit -- add x1.2 buffer for HTML escape characters
	// client side form should not include buffer
	if float64(len(post.Title)) > float64(appConfig.TitleMaxCharacters)*1.2 {
		post.Title = (post.Title[0:appConfig.TitleMaxCharacters])
	}

	// Enforce body limit -- add x1.2 buffer for HTML escape characters
	// client side form should not include buffer
	if float64(len(post.Body)) > float64(appConfig.BodyMaxCharacters)*1.2 {
		post.Title = (post.Title[0:appConfig.BodyMaxCharacters])
	}

	// Enforce channel limit
	if len(post.Channel) > appConfig.ChannelMaxCharacters {
		post.Title = (post.Title[0:appConfig.ChannelMaxCharacters])
	}
	return
}

const (
	// PostTypeAll specifies a request for all post types in a PostFilterset
	PostTypeAll = iota
	// PostTypePosts specifies a request for only top-level posts in a PostFilterset
	PostTypePosts
	// PostTypeComments specifies a request for only comments in a PostFilterset
	PostTypeComments
)

// PostFilterset defines a set of filters for querying posts
type PostFilterset struct {
	Channel       string // filter by channel
	PubKey        string // filter by submitter's pubkey
	PostType      int    // filter by post/comment/all (see iota above)
	HideBadUsers  bool   // hide users with low up/down ratios
	OrderByColumn string // which column to use for sorting
	Limit         int    // limit # of rows returned
	// TODO: sort direction?
}

// Vote defines an upvote/downvote
type Vote struct {
	PubKey    string `json:"pubkey,omitempty" form:"pubkey"`        // vote owner's public key
	Target    string `json:"target,omitempty" form:"target"`        // the post being voted on
	CreatedAt uint32 `json:"create_at,omitempty" form:"created_at"` // vote timestamp
	Direction bool   `json:"direction,omitempty" form:"direction"`  // false=down, true=up
}

// StripForPublish strips superflous parameters to prepare for publishing (omitempty)
// this is mainly to reduce nostr event content size
// clients shouldn't assume all post events received from relays have superflous parameters stripped
func (vote *Vote) StripForPublish() {
	vote.PubKey = ""
	vote.CreatedAt = 0
	return
}

// IsValid ensures that a vote looks valid for submission
func (vote *Vote) IsValid() bool {
	if vote.Target == "" {
		return false
	}
	return true
}

// VoteFilterset defines a set of filters for querying votes
type VoteFilterset struct {
	PubKey        string // filter by submitter's pubkey
	OrderByColumn string // which column to use for sorting
	Limit         int    // limit # of rows returnd
	// TODO: sort direction?
}

// User defines a user
type User struct {
	PrivKey       string `json:"privkey,omitempty" form:"privkey"`               // user private key
	PubKey        string `json:"pubkey,omitempty" form:"pubkey"`                 // user public key
	HideDownvoted bool   `json:"hide_downvoted,omitempty" form:"hide_downvoted"` // hide downvoted comments
	HideBadUsers  bool   `json:"hide_bad_users,omitempty" form:"hide_bad_users"` // hide users with low up/down ratios
	HideImages    bool   `json:"hide_images,omitempty" form:"hide_images"`       // don't auto-load images in posts
	DarkMode      bool   `json:"dark_mode,omitempty" form:"dark_mode"`           // enable dark mode styling
}

// LoggedOutUser creates a new user object with default values
func LoggedOutUser() *User {
	user := &User{
		HideDownvoted: true, // hide downvoted posts by default
		HideBadUsers:  true, // hide low ratio users by default
		DarkMode:      true, // use dark mode by default
	}
	return user
}

// Login defines a login
type Login struct {
	Password string `json:"password" form:"password"` // allows user to login with a password
	PrivKey  string `json:"privkey" form:"privkey"`   // allows user to login with a private key
	Seed     string `json:"seed" form:"seed"`         // allows user to login with a bip39 mnemonic
}

// GeneratePrivateKey generates a private key for a given login
func (login Login) GeneratePrivateKey() (string, error) {
	if login.Password != "" {
		// Make sure the user isn't attempting to provide bip39 mnemonic as password
		if nip06.ValidateWords(login.Password) {
			return "", errors.New("seed phrase provided in password field")
		}

		// Derive private key
		sum := sha256.Sum256([]byte(login.Password))
		return hex.EncodeToString(sum[:]), nil
	}

	if login.PrivKey != "" {
		// Check for valid hex
		if _, err := hex.DecodeString(login.PrivKey); err != nil {
			return "", err
		}

		// Validate length
		if len(login.PrivKey) != 64 {
			return "", errors.New("invalid privkey length")
		}

		return login.PrivKey, nil
	}

	if login.Seed != "" {
		if !nip06.ValidateWords(login.Seed) {
			return "", errors.New("invalid seed")
		}
		return nip06.PrivateKeyFromSeed([]byte(login.Seed))
	}

	return "", errors.New("invalid auth")
}

// AppConfig defines the schema for global app config
type AppConfig struct {
	SiteName             string `json:"site_name"`               // website's name
	SiteIcon             string `json:"site_icon"`               // website's icon displayed in the header
	Tagline              string `json:"tagline"`                 // website's tagline
	SiteURL              string `json:"site_url"`                // webiste's base URL including protocol. no trailing slash
	Relay                string `json:"relay"`                   // primary nostr relay endpoint - TODO: allow multiple relays
	RelayPublic          string `json:"relay_public"`            // publicly accessable relay endpoint
	RepoLink             string `json:"app_repo"`                // public repo for the project
	TelegramLink         string `json:"telegram_link"`           // public telegram group link
	PubkeyVerifyURL      string `json:"pubkey_verify_url"`       // URL for verifying a user's pubkey with the nostr relay
	VerifyBaseURL        string `json:"verify_base_url"`         // base URL for a user to submit verification for account
	CheckVerifiedBaseURL string `json:"check_verified_base_url"` // base URL for checking if a user is registered with the nostr relay
	TitleMaxCharacters   int    `json:"title_max_characters"`    // maximum allowed characters in a post title
	BodyMaxCharacters    int    `json:"body_max_characters"`     // maximum allowed characters in a post/comment body
	ChannelMaxCharacters int    `json:"channel_max_characters"`  // maximum allowed characters in a channel name
}
