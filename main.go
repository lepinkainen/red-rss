package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/feeds"
	"golang.org/x/oauth2"
)

const (
	Version = "1.0.0"
)

func main() {
	// Set up structured logging
	setupLogging()

	// Parse command-line flags
	var (
		configURL = flag.String("config", "", "URL to load remote configuration from")
		version   = flag.Bool("version", false, "Show version information")
		verbose   = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	if *version {
		fmt.Printf("GoRedditFeedGenerator version %s\n", Version)
		return
	}

	if *verbose {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	slog.Info("Starting GoRedditFeedGenerator", "version", Version)

	// Initialize default configuration
	InitializeDefaultConfig()

	// Load configuration
	err := LoadConfig(*configURL)
	if err != nil {
		slog.Warn("Could not load config, creating new one", "error", err)

		// Interactive configuration setup
		if err := setupInteractiveConfig(); err != nil {
			slog.Error("Failed to set up configuration", "error", err)
			os.Exit(1)
		}

		if err := SaveConfig(); err != nil {
			slog.Error("Failed to save configuration", "error", err)
			os.Exit(1)
		}
	}

	// Initialize OAuth2 configuration
	InitializeOAuth2Config()

	// Authenticate or refresh token
	if err := handleAuthentication(); err != nil {
		slog.Error("Authentication failed", "error", err)
		os.Exit(1)
	}

	// Initialize OpenGraph database
	slog.Info("Initializing OpenGraph cache database")
	db, err := InitOpenGraphDB()
	if err != nil {
		slog.Error("Failed to initialize OpenGraph database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Clean up expired entries
	if err := db.CleanupExpiredEntries(); err != nil {
		slog.Warn("Failed to cleanup expired entries", "error", err)
	}

	// Create authenticated HTTP client
	ctx := context.Background()
	client := CreateAuthenticatedClient(ctx, Token)

	// Create Reddit API client
	redditAPI := NewRedditAPI(client)

	// Fetch Reddit homepage posts
	slog.Info("Fetching Reddit homepage posts")
	posts, err := redditAPI.FetchRedditHomepage()
	if err != nil {
		slog.Error("Failed to fetch Reddit homepage", "error", err)
		os.Exit(1)
	}
	slog.Info("Fetched Reddit posts", "count", len(posts))

	// Filter posts
	filteredPosts := FilterPosts(posts, GlobalConfig.ScoreFilter, GlobalConfig.CommentFilter)
	slog.Info("Filtered posts", "count", len(filteredPosts), "minScore", GlobalConfig.ScoreFilter, "minComments", GlobalConfig.CommentFilter)

	// Create OpenGraph fetcher
	ogFetcher := NewOpenGraphFetcher(db)

	// Create feed generator
	feedGenerator := NewFeedGenerator(ogFetcher)

	// Generate feed
	slog.Info("Generating feed", "type", GlobalConfig.FeedType)
	feed, err := feedGenerator.GenerateFeed(filteredPosts, GlobalConfig.FeedType)
	if err != nil {
		slog.Error("Failed to generate feed", "error", err)
		os.Exit(1)
	}

	// Validate feed
	if err := feedGenerator.ValidateFeed(feed); err != nil {
		slog.Error("Feed validation failed", "error", err)
		os.Exit(1)
	}

	// Save feed to file
	if err := feedGenerator.SaveFeedToFile(feed, GlobalConfig.FeedType, GlobalConfig.OutputPath); err != nil {
		slog.Error("Failed to save feed to file", "error", err)
		os.Exit(1)
	}

	// Display success message
	slog.Info("Feed generation completed successfully",
		"type", GlobalConfig.FeedType,
		"path", GlobalConfig.OutputPath,
		"items", len(feed.Items))

	fmt.Printf("🎉 Successfully generated %s feed and saved to %s\n", GlobalConfig.FeedType, GlobalConfig.OutputPath)
}

// setupLogging configures structured logging
func setupLogging() {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{Key: "time", Value: slog.StringValue(a.Value.Time().Format("15:04:05"))}
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}

// setupInteractiveConfig prompts the user for configuration values
func setupInteractiveConfig() error {
	// Prompt user for client ID
	if GlobalConfig.ClientID == "" {
		fmt.Print("Enter Reddit Client ID (from your Reddit app settings): ")
		fmt.Scanln(&GlobalConfig.ClientID)
	}
	GlobalConfig.ClientSecret = "" // Ensure it's empty for installed apps

	// Prompt user for score filter
	var scoreInput string
	fmt.Print("Enter minimum post score (e.g., 50 for posts with 50+ score, 0 for no filter): ")
	fmt.Scanln(&scoreInput)
	score, err := strconv.Atoi(scoreInput)
	if err != nil {
		slog.Warn("Invalid score filter input, defaulting to 0", "error", err)
		GlobalConfig.ScoreFilter = 0
	} else {
		GlobalConfig.ScoreFilter = score
	}

	// Prompt user for comment filter
	var commentInput string
	fmt.Print("Enter minimum comment count (e.g., 10 for posts with 10+ comments, 0 for no filter): ")
	fmt.Scanln(&commentInput)
	comments, err := strconv.Atoi(commentInput)
	if err != nil {
		slog.Warn("Invalid comment filter input, defaulting to 0", "error", err)
		GlobalConfig.CommentFilter = 0
	} else {
		GlobalConfig.CommentFilter = comments
	}

	GlobalConfig.RedirectURI = fmt.Sprintf("http://localhost:%s/callback", AuthPort)
	GlobalConfig.FeedType = "rss"                        // Default feed type
	GlobalConfig.OutputPath = "reddit_homepage_feed.xml" // Default output path

	return nil
}

// handleAuthentication manages OAuth2 authentication flow
func handleAuthentication() error {
	if GlobalConfig.RefreshToken == "" {
		slog.Info("No refresh token found, starting browser authentication")
		return AuthenticateUser()
	}

	slog.Info("Refresh token found, attempting to refresh access token")
	Token = &oauth2.Token{
		RefreshToken: GlobalConfig.RefreshToken,
		AccessToken:  GlobalConfig.AccessToken,
		Expiry:       GlobalConfig.ExpiresAt,
	}

	if !Token.Valid() {
		slog.Info("Access token expired or invalid, refreshing")
		if err := RefreshAccessToken(); err != nil {
			slog.Error("Failed to refresh access token", "error", err)
			return err
		}
		slog.Info("Access token refreshed successfully")
	} else {
		slog.Info("Access token is still valid")
	}

	return nil
}

// filterPosts is a simple wrapper for the FilterPosts function for backward compatibility
func filterPosts(posts []RedditPost, minScore, minComments int) []RedditPost {
	return FilterPosts(posts, minScore, minComments)
}

// generateFeed is a simple wrapper for the feed generator for backward compatibility
func generateFeed(posts []RedditPost, feedType string, db *OpenGraphDB) (*feeds.Feed, error) {
	ogFetcher := NewOpenGraphFetcher(db)
	feedGenerator := NewFeedGenerator(ogFetcher)
	return feedGenerator.GenerateFeed(posts, feedType)
}

// saveFeedToFile is a simple wrapper for the feed generator for backward compatibility
func saveFeedToFile(feed *feeds.Feed, feedType, outputPath string) error {
	ogFetcher := NewOpenGraphFetcher(nil)
	feedGenerator := NewFeedGenerator(ogFetcher)
	return feedGenerator.SaveFeedToFile(feed, feedType, outputPath)
}

// getOpenGraphPreview is a simple wrapper for the OpenGraph fetcher for backward compatibility
func getOpenGraphPreview(db *OpenGraphDB, url string) *OpenGraphData {
	ogFetcher := NewOpenGraphFetcher(db)
	return ogFetcher.GetOpenGraphPreview(url)
}

// fetchRedditHomepage is a simple wrapper for the Reddit API for backward compatibility
func fetchRedditHomepage(client *http.Client) ([]RedditPost, error) {
	redditAPI := NewRedditAPI(client)
	return redditAPI.FetchRedditHomepage()
}

// Compatibility functions for legacy code that might still reference these
func loadConfig() error {
	return LoadConfig("")
}

func saveConfig() error {
	return SaveConfig()
}

func authenticateUser() error {
	return AuthenticateUser()
}

func refreshAccessToken() error {
	return RefreshAccessToken()
}

func initOpenGraphDB() (*sql.DB, error) {
	db, err := InitOpenGraphDB()
	if err != nil {
		return nil, err
	}
	return db.db, nil
}

func getCachedOpenGraph(db *sql.DB, url string) (*OpenGraphData, error) {
	ogDB := &OpenGraphDB{db: db}
	return ogDB.GetCachedOpenGraph(url)
}

func saveCachedOpenGraph(db *sql.DB, og *OpenGraphData) error {
	ogDB := &OpenGraphDB{db: db}
	return ogDB.SaveCachedOpenGraph(og)
}

func fetchOpenGraphData(url string) (*OpenGraphData, error) {
	ogFetcher := NewOpenGraphFetcher(nil)
	return ogFetcher.FetchOpenGraphData(url)
}

func parseOpenGraphTags(htmlContent string) (*OpenGraphData, error) {
	ogFetcher := NewOpenGraphFetcher(nil)
	return ogFetcher.parseOpenGraphTags(htmlContent)
}

func openBrowser(url string) error {
	return OpenBrowser(url)
}

func oauth2CallbackHandler(w http.ResponseWriter, r *http.Request) {
	OAuth2CallbackHandler(w, r)
}

// init function to set up default configuration values if not specified
func init() {
	InitializeDefaultConfig()
}
