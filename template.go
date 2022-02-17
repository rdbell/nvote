package main

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	checkErr "github.com/rdbell/nvote/check"
	"github.com/rdbell/nvote/schemas"

	timeago "github.com/ararog/timeago"
	"github.com/gobuffalo/packr/v2"
	"github.com/gobuffalo/packr/v2/file"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/parser"
	"github.com/labstack/echo/v4"
	"github.com/microcosm-cc/bluemonday"
)

var appConfig *schemas.AppConfig

// pageData defines common data rendered by page templates
type pageData struct {
	Config    *schemas.AppConfig
	User      *schemas.User
	Title     string
	Page      interface{}
	CsrfToken string
}

// Init initializes a PageData instance with request info
func (p *pageData) Init(c echo.Context) *pageData {
	user := schemas.LoggedOutUser()
	if u, ok := c.Get("user").(*schemas.User); ok {
		user = u
	}
	p.Config = appConfig
	p.User = user
	p.CsrfToken, _ = c.Get("csrf").(string)
	return p
}

// Template struct for template rendering
type Template struct {
	templates *template.Template
}

// t is a Template instance that gets used by Render()
var t *Template

var templates map[string]*template.Template

// Render renders templates for http responses
func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	keys := make([]string, 0, len(templates))
	for k := range templates {
		keys = append(keys, k)
	}

	tmp, ok := templates[name]
	if !ok {
		panic(errors.New("invalid template: " + name))
	}

	// layout -> defined in each layout template
	return tmp.ExecuteTemplate(w, "layout", data)
}

// cacheBuster is a build-time variable that gets set to the current unix timestamp at time of build
var cacheBuster = "0"

// loadTemplates pre-computes templates in the views folder
// box should be a pointer to a packr v2 file box containing the views
func loadTemplates(box *packr.Box) {
	// Setup custom template functions
	funcMap := template.FuncMap{
		// The name in quotes is what the function will be called in the template text.
		"add": func(val1 int, val2 int) int {
			return val1 + val2
		},
		"floatToString": func(val float32) string {
			return fmt.Sprintf("%.2f", val)
		},
		"timeAgo": func(ts uint32) string {
			t := time.Unix(int64(ts), 0)
			timeAgo, err := timeago.TimeAgoWithTime(time.Now(), t)
			if err != nil {
				return "some time ago"
			}

			return strings.ToLower(timeAgo)
		},
		"cacheBuster": func() string {
			return cacheBuster
		},
		"pointsGrammar": func(points int32) string {
			s := fmt.Sprintf("%d points", points)
			if points == -1 || points == 1 {
				s = s[:len(s)-1]
			}
			return s
		},
		"joinStrings": func(ss []string) string {
			return strings.Join(ss[:], ",")
		},
		"sanitize": func(s string) template.HTML {
			// Sanitize HTML
			sanitized := bluemonday.UGCPolicy().Sanitize(s)
			return template.HTML(sanitized)
		},
		"shortBody": func(s string) string {
			if len(s) > 64 {
				return s[0:64] + "..."
			}
			return s
		},
		"shortHash": func(s string) string {
			if len(s) > 8 {
				return (s[0:8]) + "…"
			}
			return s
		},
		"isVerified": func(pubkey string) bool {
			verified, _ := checkVerification(pubkey)
			return verified
		},
		"hasVoted": func(votes []*schemas.Vote, target string) string {
			for _, vote := range votes {
				if vote.Target != target {
					continue
				}
				if vote.Direction == true {
					return "up"
				}
				if vote.Direction == false {
					return "down"
				}
			}
			return ""
		},
		"linkDomain": func(s string) string {
			// Parse URL
			u, err := stringToURL(s)
			if err != nil {
				return ""
			}

			// Extract just domain
			parts := strings.Split(u.Hostname(), ".")
			domain := parts[len(parts)-2] + "." + parts[len(parts)-1]

			// Truncate long domains
			if len(domain) > 32 {
				return domain[0:32] + "..."
			}

			return domain
		},
		"pubkeyAlias": func(pubkey string) string {
			// Ensure length
			if len(pubkey) < 15 {
				return pubkey
			}

			// Query DB for alias first
			a, err := aliasForPubKey(pubkey)
			if err == nil && a.Name != "" {
				return a.Name
			}

			// Convert pubkey string to integer from base16 hex
			// use [0:15] to prevent value out of range
			i, err := strconv.ParseUint(pubkey[0:15], 16, 64)
			if err != nil {
				return (pubkey[0:8])
			}

			// New random source
			random := rand.New(rand.NewSource(int64(i)))

			w1 := bip39WordList[random.Intn(len(bip39WordList))]
			w2 := bip39WordList[random.Intn(len(bip39WordList))]
			randomNumber := random.Intn(9999)

			randomName := fmt.Sprintf("%v%v%d", strings.Title(w1), strings.Title(w2), randomNumber)

			return randomName
		},
		"whitespaceTrimmedURL": func(s string) string {
			// Don't return trimmed URL for images
			if _, err := stringToImageURL(s); err == nil {
				return ""
			}

			u, err := stringToURL(s)
			if err != nil {
				return ""
			}
			return u.String()
		},
		"renderMarkdown": func(s string) template.HTML {
			// If post is just an image link and nothing else, turn it into an inline image
			// TODO: do the same for all image links in body?
			u, err := stringToImageURL(s)
			if err == nil {
				s = `[![](` + u.String() + ` "")](` + u.String() + `)`
			}

			// Render markdown
			parser := parser.NewWithExtensions(parser.Autolink | parser.Strikethrough | parser.HardLineBreak | parser.NonBlockingSpace)
			html := string(markdown.ToHTML([]byte(s), parser, nil))

			// Sanitize HTML
			sanitized := bluemonday.UGCPolicy().Sanitize(html)

			// bluemonday seems to strip loading=lazy param. Manually add it.
			return template.HTML(strings.Replace(sanitized, "<img src", "<img loading=\"lazy\" src", -1))
		},
		"renderMarkdownNoImages": func(s string) template.HTML {
			// Render markdown
			parser := parser.NewWithExtensions(parser.Autolink | parser.Strikethrough | parser.HardLineBreak | parser.NonBlockingSpace)
			html := markdown.ToHTML([]byte(s), parser, nil)

			// Replace <img> tags with <a>
			var re = regexp.MustCompile(`<img .*src="([^"]+)".*>`)
			html = []byte(re.ReplaceAllString(string(html), `<a href="$1">$1</a>`))

			// Sanitize HTML
			sanitized := bluemonday.UGCPolicy().Sanitize(string(html))
			return template.HTML(sanitized)
		},
		"contentType": func(s string) string {
			// Detect image link
			if _, err := stringToImageURL(s); err == nil {
				return "image"
			}

			// Detect non-imgate link
			if _, err := stringToURL(s); err == nil {
				return "link"
			}

			// Text post
			return "text"
		},
		"score": func(score int32) int32 {
			if score < 0 {
				return 0
			}
			return score
		},
		"shouldDisplayBody": func(post *schemas.Post, hideImages bool) bool {
			// Always display body for replies
			if post.Parent != "" {
				return true
			}

			// Display body if the post is an image
			if _, err := stringToImageURL(post.Body); err == nil {
				return true
			}

			// Don't display body if the post is a link
			if _, err := stringToURL(post.Body); err == nil {
				return false
			}

			// Display body for text posts
			return true
		},
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, errors.New("invalid dict call")
			}
			dict := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, errors.New("dict keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
	}

	// Initialize templates
	if templates == nil {
		templates = make(map[string]*template.Template)
	}

	// Layouts/pages/shared
	var layouts []string
	var pages []string
	var shared []string
	var walkFolder *[]string

	walkFunc := func(path string, info file.File) error {
		finfo, err := info.FileInfo()
		checkErr.Panic(err)
		filename := finfo.Name()
		if filename[len(filename)-10:] == ".html.tmpl" {
			*walkFolder = append(*walkFolder, path)
		}
		return nil
	}

	walkFolder = &layouts
	box.WalkPrefix("layouts", walkFunc)
	walkFolder = &pages
	box.WalkPrefix("pages", walkFunc)
	walkFolder = &shared
	box.WalkPrefix("shared", walkFunc)

	// Generate our templates map from our layouts, pages, and shared boxed file lists
	for _, layout := range layouts {
		for _, page := range pages {
			layoutBase := filepath.Base(layout)
			layoutShort := layoutBase[0:strings.Index(layoutBase, ".")]
			pageBase := filepath.Base(page)
			pageShort := pageBase[0:strings.Index(pageBase, ".")]

			layoutString, err := box.FindString(layout)
			checkErr.Panic(err)
			pageString, err := box.FindString(page)
			checkErr.Panic(err)
			templatesString := layoutString + pageString
			for _, sharedName := range shared {
				sharedString, err := box.FindString(sharedName)
				checkErr.Panic(err)
				templatesString += sharedString
			}
			templates[layoutShort+":"+pageShort] = template.Must(template.New(pageShort).Delims("[[", "]]").Funcs(funcMap).Parse(templatesString))
		}
	}
}

// stringToImageURL returns a *url.URL if the provided string is a link to an image
func stringToImageURL(s string) (*url.URL, error) {
	if len(s) < 15 {
		return nil, errors.New("link is too short")
	}
	s = strings.TrimSpace(s)
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return nil, errors.New("unable to parse URL")
	}
	last4 := u.String()[len(s)-4:]
	last5 := u.String()[len(s)-5:]
	if last4 == ".gif" || last4 == ".png" || last4 == ".jpg" || last4 == ".svg" ||
		last5 == ".jpeg" || last5 == ".webp" || last5 == ".svgz" {
		return u, nil
	}
	return nil, errors.New("not an image link")
}

// stringToURL returns a *url.URL if the provided string is a link
func stringToURL(s string) (*url.URL, error) {
	if len(s) < 10 {
		return nil, errors.New("link is too short")
	}
	s = strings.TrimSpace(s)
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return nil, errors.New("unable to parse URL")
	}
	return u, nil
}
