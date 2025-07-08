package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv" // Import strconv for string to int conversion
	"sync"
	"time"

	"github.com/gorilla/feeds" // For RSS/Atom feed generation
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

const (
	configFileName = "reddit_feed_config.json"
	authPort       = "8080" // Port for the local authentication server
)

var (
	oauth2Config *oauth2.Config
	token        *oauth2.Token
	config       Config
	authCodeChan = make(chan string) // Channel to receive the authorization code
	serverWg     sync.WaitGroup      // WaitGroup to manage the HTTP server lifecycle
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile) // Add file and line number to logs

	// Load configuration
	err := loadConfig()
	if err != nil {
		log.Printf("Could not load config, creating new one: %v", err)
		// Prompt user for client ID
		if config.ClientID == "" {
			fmt.Print("Enter Reddit Client ID (from your Reddit app settings): ")
			fmt.Scanln(&config.ClientID)
		}
		config.ClientSecret = "" // Ensure it's empty for installed apps

		// Prompt user for score filter
		var scoreInput string
		fmt.Print("Enter minimum post score (e.g., 50 for posts with 50+ score, 0 for no filter): ")
		fmt.Scanln(&scoreInput)
		config.ScoreFilter, err = strconv.Atoi(scoreInput)
		if err != nil {
			log.Printf("Invalid score filter input, defaulting to 0: %v", err)
			config.ScoreFilter = 0
		}

		// Prompt user for comment filter
		var commentInput string
		fmt.Print("Enter minimum comment count (e.g., 10 for posts with 10+ comments, 0 for no filter): ")
		fmt.Scanln(&commentInput)
		config.CommentFilter, err = strconv.Atoi(commentInput)
		if err != nil {
			log.Printf("Invalid comment filter input, defaulting to 0: %v", err)
			config.CommentFilter = 0
		}

		config.RedirectURI = fmt.Sprintf("http://localhost:%s/callback", authPort)
		config.FeedType = "rss"                        // Default feed type
		config.OutputPath = "reddit_homepage_feed.xml" // Default output path
		saveConfig()                                   // Save initial config
	}

	// Define Reddit's OAuth2 endpoints manually
	redditEndpoint := oauth2.Endpoint{
		AuthURL:  "https://www.reddit.com/api/v1/authorize",
		TokenURL: "https://www.reddit.com/api/v1/access_token",
	}

	// Initialize OAuth2 config
	oauth2Config = &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret, // This will be an empty string for installed apps
		RedirectURL:  config.RedirectURI,
		Scopes:       []string{"identity", "read", "history"}, // Request necessary scopes
		Endpoint:     redditEndpoint,                          // Use the manually defined endpoint
	}

	// Authenticate or refresh token
	if config.RefreshToken == "" {
		log.Println("No refresh token found. Starting browser authentication...")
		authenticateUser()
	} else {
		log.Println("Refresh token found. Attempting to refresh access token...")
		token = &oauth2.Token{
			RefreshToken: config.RefreshToken,
			AccessToken:  config.AccessToken, // Use existing access token if still valid
			Expiry:       config.ExpiresAt,
		}
		if !token.Valid() {
			log.Println("Access token expired or invalid. Refreshing...")
			err = refreshAccessToken()
			if err != nil {
				log.Fatalf("Failed to refresh access token: %v", err)
			}
			log.Println("Access token refreshed successfully.")
		} else {
			log.Println("Access token is still valid.")
		}
	}

	// Create an authenticated HTTP client
	client := oauth2Config.Client(context.Background(), token)

	// Fetch Reddit homepage posts
	log.Println("Fetching Reddit homepage posts...")
	posts, err := fetchRedditHomepage(client)
	if err != nil {
		log.Fatalf("Failed to fetch Reddit homepage: %v", err)
	}
	log.Printf("Fetched %d posts.", len(posts))

	// Filter posts
	filteredPosts := filterPosts(posts, config.ScoreFilter, config.CommentFilter)
	log.Printf("Filtered down to %d posts (score >= %d, comments >= %d).", len(filteredPosts), config.ScoreFilter, config.CommentFilter)

	// Generate feed
	log.Printf("Generating %s feed...", config.FeedType)
	feed, err := generateFeed(filteredPosts, config.FeedType)
	if err != nil {
		log.Fatalf("Failed to generate feed: %v", err)
	}

	// Save feed to file
	err = saveFeedToFile(feed, config.FeedType, config.OutputPath)
	if err != nil {
		log.Fatalf("Failed to save feed to file: %v", err)
	}

	log.Printf("Successfully generated %s feed and saved to %s", config.FeedType, config.OutputPath)
}

// loadConfig loads the configuration from a JSON file
func loadConfig() error {
	file, err := os.ReadFile(configFileName)
	if err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}
	err = json.Unmarshal(file, &config)
	if err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}
	return nil
}

// saveConfig saves the current configuration to a JSON file
func saveConfig() error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling config: %w", err)
	}
	err = os.WriteFile(configFileName, data, 0600) // Permissions 0600 for security
	if err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}
	return nil
}

// authenticateUser starts a local web server, opens the browser for authentication,
// and retrieves the access and refresh tokens.
func authenticateUser() {
	// Create a context for the HTTP server to allow graceful shutdown
	serverCtx, serverCancel := context.WithCancel(context.Background())

	// Start a local HTTP server to handle the OAuth2 callback
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		http.HandleFunc("/callback", oauth2CallbackHandler)
		log.Printf("Starting local HTTP server on :%s for OAuth2 callback...", authPort)
		server := &http.Server{Addr: ":" + authPort}

		// Goroutine to listen for server shutdown signal
		go func() {
			<-serverCtx.Done() // Wait for the main goroutine to cancel the context
			log.Println("Received shutdown signal for local HTTP server.")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				log.Printf("Error shutting down HTTP server: %v", err)
			}
		}()

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Construct the authorization URL
	authURL := oauth2Config.AuthCodeURL("state", oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("duration", "permanent"))

	// Open the URL in the user's default browser
	log.Printf("Opening browser for Reddit authentication at: %s", authURL)
	err := openBrowser(authURL)
	if err != nil {
		log.Fatalf("Failed to open browser: %v. Please open the URL manually.", err)
	}

	// Wait for the authorization code to be sent via the channel
	authCode := <-authCodeChan

	// Signal the HTTP server to shut down
	serverCancel()

	// Exchange the authorization code for tokens with retry logic
	const maxRetries = 5
	initialBackoff := 1 * time.Second
	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel() // Ensure cancel is called for each context

		// For "installed app" type, ClientSecret is an empty string.
		// The oauth2.Config.Exchange method handles this correctly by not sending
		// a client_secret parameter in the request body if it's empty.
		token, err = oauth2Config.Exchange(ctx, authCode)
		if err == nil {
			break // Success!
		}

		// Check if it's a rate limit error (429 Too Many Requests)
		if oe, ok := err.(*oauth2.RetrieveError); ok && oe.Response.StatusCode == http.StatusTooManyRequests {
			log.Printf("Received 429 Too Many Requests. Retrying in %v...", initialBackoff)
			time.Sleep(initialBackoff)
			initialBackoff *= 2 // Exponential backoff
			continue
		} else {
			log.Fatalf("Failed to exchange authorization code for token after %d retries: %v", i+1, err)
		}
	}

	if token == nil {
		log.Fatalf("Failed to exchange authorization code for token after %d retries.", maxRetries)
	}

	// Store tokens in config
	config.AccessToken = token.AccessToken
	config.RefreshToken = token.RefreshToken
	config.ExpiresAt = token.Expiry
	saveConfig()
	log.Println("Authentication successful. Tokens saved.")

	// Ensure the server goroutine has finished before proceeding
	serverWg.Wait()
}

// oauth2CallbackHandler handles the redirect from Reddit after user authentication.
func oauth2CallbackHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	errorParam := query.Get("error")

	if errorParam != "" {
		log.Printf("OAuth2 callback error: %s", errorParam)
		fmt.Fprintf(w, "Authentication failed: %s. Please check the console for details.", errorParam)
		authCodeChan <- "" // Send empty string to unblock main goroutine
		return
	}

	if state != "state" { // Simple state check, you might want a more robust one
		log.Printf("State mismatch: expected 'state', got '%s'", state)
		fmt.Fprint(w, "Authentication failed: State mismatch.")
		authCodeChan <- ""
		return
	}

	if code == "" {
		log.Println("No authorization code received in callback.")
		fmt.Fprint(w, "Authentication failed: No code received.")
		authCodeChan <- ""
		return
	}

	log.Println("Authorization code received successfully.")
	fmt.Fprint(w, "Authentication successful! You can close this browser tab.")
	authCodeChan <- code // Send the code to the main goroutine
}

// openBrowser opens the given URL in the default web browser.
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "netbsd", "openbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// refreshAccessToken uses the refresh token to obtain a new access token.
func refreshAccessToken() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a token source from the existing refresh token
	// The oauth2.Config.TokenSource correctly handles the empty ClientSecret for installed apps.
	tokenSource := oauth2Config.TokenSource(ctx, token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to get new token from refresh token: %w", err)
	}

	token = newToken // Update the global token
	config.AccessToken = token.AccessToken
	config.RefreshToken = token.RefreshToken // Refresh token might also be updated
	config.ExpiresAt = token.Expiry
	return saveConfig()
}

// fetchRedditHomepage fetches posts from the authenticated user's homepage.
func fetchRedditHomepage(client *http.Client) ([]RedditPost, error) {
	// Reddit API endpoint for user's front page. Limit to 100 posts for a good sample.
	// You can adjust 'limit' as needed.
	// For a logged-in user, this is usually accessed via /hot or /best without a subreddit prefix.
	// Let's use /best as it's often the default sorted homepage.
	apiURL := "https://oauth.reddit.com/best?limit=100" // User's personalized "best" feed

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "GoRedditFeedGenerator/1.0 by YourRedditUsername") // IMPORTANT: Set a unique User-Agent

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Reddit API returned non-OK status: %s", resp.Status)
	}

	var listing RedditListing
	err = json.NewDecoder(resp.Body).Decode(&listing)
	if err != nil {
		return nil, fmt.Errorf("failed to decode Reddit API response: %w", err)
	}

	return listing.Data.Children, nil
}

// filterPosts applies score and comment count filters to a list of Reddit posts.
func filterPosts(posts []RedditPost, minScore, minComments int) []RedditPost {
	var filtered []RedditPost
	for _, post := range posts {
		if post.Data.Score >= minScore && post.Data.NumComments >= minComments {
			filtered = append(filtered, post)
		}
	}
	return filtered
}

// generateFeed creates an RSS or Atom feed from the filtered Reddit posts.
func generateFeed(posts []RedditPost, feedType string) (*feeds.Feed, error) {
	now := time.Now()
	feed := &feeds.Feed{
		Title:       "My Reddit Homepage Feed",
		Link:        &feeds.Link{Href: "https://www.reddit.com/"},
		Description: "Filtered Reddit homepage posts generated by GoRedditFeedGenerator",
		Author:      &feeds.Author{Name: "GoRedditFeedGenerator"},
		Created:     now,
		Updated:     now,
	}

	for _, post := range posts {
		item := &feeds.Item{
			Title:       post.Data.Title,
			Link:        &feeds.Link{Href: post.Data.URL},
			Description: fmt.Sprintf("Score: %d, Comments: %d, Subreddit: r/%s", post.Data.Score, post.Data.NumComments, post.Data.Subreddit),
			Author:      &feeds.Author{Name: post.Data.Author},
			Created:     time.Unix(int64(post.Data.CreatedUTC), 0),
			Id:          fmt.Sprintf("https://www.reddit.com%s", post.Data.Permalink), // Unique ID for the item
		}
		feed.Items = append(feed.Items, item)
	}
	return feed, nil
}

// saveFeedToFile saves the generated feed to a specified file.
func saveFeedToFile(feed *feeds.Feed, feedType, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	if feedType == "rss" {
		return feed.WriteRss(file)
	} else if feedType == "atom" {
		return feed.WriteAtom(file)
	}
	return fmt.Errorf("unsupported feed type: %s", feedType)
}

// init function to set up default configuration values if not specified
func init() {
	config.ScoreFilter = 0
	config.CommentFilter = 0
	config.FeedType = "rss"
	config.OutputPath = "reddit_homepage_feed.xml"
}
