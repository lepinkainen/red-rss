package main

import (
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// Config struct to hold application settings and tokens
type Config struct {
	ClientID      string    `json:"client_id"`
	ClientSecret  string    `json:"client_secret"` // This will be empty for "installed app" type
	RedirectURI   string    `json:"redirect_uri"`
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token"`
	ExpiresAt     time.Time `json:"expires_at"`
	ScoreFilter   int       `json:"score_filter"`
	CommentFilter int       `json:"comment_filter"`
	FeedType      string    `json:"feed_type"` // "rss" or "atom"
	OutputPath    string    `json:"output_path"`
}

// RedditPost represents a simplified Reddit post structure for our needs
type RedditPost struct {
	Data struct {
		Title       string  `json:"title"`
		URL         string  `json:"url"`
		Permalink   string  `json:"permalink"`
		CreatedUTC  float64 `json:"created_utc"`
		Score       int     `json:"score"`
		NumComments int     `json:"num_comments"`
		Author      string  `json:"author"`
		Subreddit   string  `json:"subreddit"`
	} `json:"data"`
}

// RedditListing represents the structure of the Reddit API response for listings
type RedditListing struct {
	Data struct {
		Children []RedditPost `json:"children"`
		After    string       `json:"after"`
	} `json:"data"`
}

// OpenGraphData represents OpenGraph metadata for external links
type OpenGraphData struct {
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Image       string    `json:"image"`
	SiteName    string    `json:"site_name"`
	FetchedAt   time.Time `json:"fetched_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Global constants
const (
	ConfigFileName      = "reddit_feed_config.json"
	AuthPort            = "8080"               // Port for the local authentication server
	OpenGraphDBFile     = "opengraph_cache.db" // SQLite database file for OpenGraph cache
	OpenGraphCacheHours = 24                   // Cache expiry in hours
)

// Global variables
var (
	OAuth2Config *oauth2.Config
	Token        *oauth2.Token
	GlobalConfig Config
	AuthCodeChan = make(chan string) // Channel to receive the authorization code
	ServerWg     sync.WaitGroup      // WaitGroup to manage the HTTP server lifecycle
)
