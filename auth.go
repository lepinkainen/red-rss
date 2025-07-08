package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
)

// AuthenticateUser starts a local web server, opens the browser for authentication,
// and retrieves the access and refresh tokens.
func AuthenticateUser() error {
	// Create a context for the HTTP server to allow graceful shutdown
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel() // Ensure context is always cancelled

	// Start a local HTTP server to handle the OAuth2 callback
	ServerWg.Add(1)
	go func() {
		defer ServerWg.Done()
		http.HandleFunc("/callback", OAuth2CallbackHandler)
		slog.Info("Starting local HTTP server for OAuth2 callback", "port", AuthPort)
		server := &http.Server{Addr: ":" + AuthPort}

		// Goroutine to listen for server shutdown signal
		go func() {
			<-serverCtx.Done() // Wait for the main goroutine to cancel the context
			slog.Info("Received shutdown signal for local HTTP server")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				slog.Error("Error shutting down HTTP server", "error", err)
			}
		}()

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	// Construct the authorization URL
	authURL := OAuth2Config.AuthCodeURL("state", oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("duration", "permanent"))

	// Open the URL in the user's default browser
	slog.Info("Opening browser for Reddit authentication", "url", authURL)
	err := OpenBrowser(authURL)
	if err != nil {
		return fmt.Errorf("failed to open browser: %w. Please open the URL manually: %s", err, authURL)
	}

	// Wait for the authorization code to be sent via the channel
	authCode := <-AuthCodeChan

	if authCode == "" {
		return fmt.Errorf("authentication failed: no authorization code received")
	}

	// Exchange the authorization code for tokens with retry logic
	err = exchangeAuthCodeForTokens(authCode)
	if err != nil {
		return fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	// Store tokens in config
	GlobalConfig.AccessToken = Token.AccessToken
	GlobalConfig.RefreshToken = Token.RefreshToken
	GlobalConfig.ExpiresAt = Token.Expiry
	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	slog.Info("Authentication successful, tokens saved")

	// Ensure the server goroutine has finished before proceeding
	ServerWg.Wait()
	return nil
}

// exchangeAuthCodeForTokens exchanges authorization code for tokens with retry logic
func exchangeAuthCodeForTokens(authCode string) error {
	const maxRetries = 5
	initialBackoff := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// For "installed app" type, ClientSecret is an empty string.
		// The oauth2.Config.Exchange method handles this correctly by not sending
		// a client_secret parameter in the request body if it's empty.
		token, err := OAuth2Config.Exchange(ctx, authCode)
		if err == nil {
			Token = token
			return nil
		}

		// Check if it's a rate limit error (429 Too Many Requests)
		if oe, ok := err.(*oauth2.RetrieveError); ok && oe.Response.StatusCode == http.StatusTooManyRequests {
			slog.Warn("Rate limited, retrying", "backoff", initialBackoff)
			time.Sleep(initialBackoff)
			initialBackoff *= 2 // Exponential backoff
			continue
		}

		return fmt.Errorf("failed to exchange authorization code for token after %d attempts: %w", i+1, err)
	}

	return fmt.Errorf("failed to exchange authorization code for token after %d retries", maxRetries)
}

// OAuth2CallbackHandler handles the redirect from Reddit after user authentication.
func OAuth2CallbackHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	errorParam := query.Get("error")

	if errorParam != "" {
		slog.Error("OAuth2 callback error", "error", errorParam)
		fmt.Fprintf(w, "Authentication failed: %s. Please check the console for details.", errorParam)
		AuthCodeChan <- "" // Send empty string to unblock main goroutine
		return
	}

	if state != "state" { // Simple state check, you might want a more robust one
		slog.Error("State mismatch", "expected", "state", "got", state)
		fmt.Fprint(w, "Authentication failed: State mismatch.")
		AuthCodeChan <- ""
		return
	}

	if code == "" {
		slog.Error("No authorization code received in callback")
		fmt.Fprint(w, "Authentication failed: No code received.")
		AuthCodeChan <- ""
		return
	}

	slog.Info("Authorization code received successfully")
	fmt.Fprint(w, "Authentication successful! You can close this browser tab.")
	AuthCodeChan <- code // Send the code to the main goroutine
}

// OpenBrowser opens the given URL in the default web browser.
func OpenBrowser(url string) error {
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

// RefreshAccessToken uses the refresh token to obtain a new access token.
func RefreshAccessToken() error {
	if Token == nil || Token.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a token source from the existing refresh token
	// The oauth2.Config.TokenSource correctly handles the empty ClientSecret for installed apps.
	tokenSource := OAuth2Config.TokenSource(ctx, Token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to get new token from refresh token: %w", err)
	}

	Token = newToken // Update the global token
	GlobalConfig.AccessToken = Token.AccessToken
	GlobalConfig.RefreshToken = Token.RefreshToken // Refresh token might also be updated
	GlobalConfig.ExpiresAt = Token.Expiry

	if err := SaveConfig(); err != nil {
		return fmt.Errorf("failed to save updated config: %w", err)
	}

	slog.Info("Access token refreshed successfully")
	return nil
}

// InitializeOAuth2Config initializes the OAuth2 configuration
func InitializeOAuth2Config() {
	// Define Reddit's OAuth2 endpoints manually
	redditEndpoint := oauth2.Endpoint{
		AuthURL:  "https://www.reddit.com/api/v1/authorize",
		TokenURL: "https://www.reddit.com/api/v1/access_token",
	}

	// Initialize OAuth2 config
	OAuth2Config = &oauth2.Config{
		ClientID:     GlobalConfig.ClientID,
		ClientSecret: GlobalConfig.ClientSecret, // This will be an empty string for installed apps
		RedirectURL:  GlobalConfig.RedirectURI,
		Scopes:       []string{"identity", "read", "history"}, // Request necessary scopes
		Endpoint:     redditEndpoint,                          // Use the manually defined endpoint
	}
}
