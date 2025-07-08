# Reddit RSS Feed Generator with OpenGraph Preview

A Go application that generates RSS/Atom feeds from your personalized Reddit homepage, enhanced with OpenGraph metadata previews for external links.

## Features

- **Reddit Authentication**: OAuth2 authentication to access your personalized Reddit feed
- **Feed Generation**: Creates RSS or Atom feeds from your Reddit homepage
- **Post Filtering**: Filter posts by minimum score and comment count
- **OpenGraph Preview**: Automatically fetches and caches OpenGraph metadata for external links
- **SQLite Caching**: Caches OpenGraph data for 24 hours to avoid repeated requests
- **Smart Link Detection**: Only fetches OpenGraph data for non-Reddit URLs

## Installation

1. Clone the repository
2. Install dependencies:
   ```bash
   go mod download
   ```
3. Build the application:
   ```bash
   go build -o build/reddit-feed-generator
   ```

## Usage

1. **First Run Setup**: The application will prompt you for:

   - Reddit Client ID (from your Reddit app settings)
   - Minimum post score filter
   - Minimum comment count filter

2. **Reddit App Setup**:

   - Go to https://www.reddit.com/prefs/apps
   - Create a new "installed app"
   - Copy the Client ID (the string under your app name)

3. **Run the Application**:

   ```bash
   ./build/reddit-feed-generator
   ```

4. **Authentication**: On first run, the app will:
   - Open your browser for Reddit authentication
   - Save authentication tokens for future use

## OpenGraph Enhancement

The application now enhances feed descriptions with OpenGraph metadata for external links:

### Before (Original):

```
Score: 150, Comments: 45, Subreddit: r/technology
```

### After (With OpenGraph):

```
Score: 150, Comments: 45, Subreddit: r/technology

🔗 Link Preview:
Title: Revolutionary AI Breakthrough Announced
Description: Scientists at MIT have developed a new AI system that can...
Site: MIT News
```

### Features:

- **Automatic Detection**: Only processes non-Reddit URLs
- **Blocked Domain Filtering**: Skips x.com and twitter.com URLs that block external access
- **Caching**: SQLite database caches OpenGraph data for 24 hours
- **Timeout Protection**: 8-second timeout prevents hanging requests
- **Graceful Fallback**: Falls back to original format if OpenGraph fetch fails
- **User-Friendly Logging**: Shows progress with emojis (🔍 fetching, ⚠️ warnings)

## Configuration

The application creates a `reddit_feed_config.json` file with your settings:

```json
{
  "client_id": "your_reddit_client_id",
  "score_filter": 50,
  "comment_filter": 10,
  "feed_type": "rss",
  "output_path": "reddit_homepage_feed.xml"
}
```

## Files Created

- `reddit_feed_config.json`: Application configuration
- `reddit_homepage_feed.xml`: Generated RSS/Atom feed
- `opengraph_cache.db`: SQLite database for OpenGraph caching

## Technical Details

### Dependencies

- `github.com/gorilla/feeds`: RSS/Atom feed generation
- `golang.org/x/oauth2`: Reddit OAuth2 authentication
- `modernc.org/sqlite`: SQLite database (no CGO dependency)
- `golang.org/x/net/html`: HTML parsing for OpenGraph extraction

### OpenGraph Cache Schema

```sql
CREATE TABLE opengraph_cache (
    url TEXT PRIMARY KEY,
    title TEXT,
    description TEXT,
    image TEXT,
    site_name TEXT,
    fetched_at DATETIME,
    expires_at DATETIME
);
```

### Supported OpenGraph Properties

- `og:title`: Page title
- `og:description`: Page description
- `og:image`: Preview image URL
- `og:site_name`: Site name

## Testing

Run the test suite:

```bash
go test -v
```

Tests cover:

- Reddit URL detection
- Blocked URL detection (x.com, twitter.com)
- OpenGraph HTML parsing
- Post filtering logic

## Security

- Configuration file uses 0600 permissions
- No client secret required (uses "installed app" OAuth2 flow)
- Reasonable request timeouts prevent abuse
- User-Agent headers identify the application

## License

This project follows standard Go project conventions and uses only permissive open-source dependencies.
