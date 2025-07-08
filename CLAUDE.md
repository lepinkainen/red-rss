# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Guidelines

This project follows the standards defined in `llm-shared/project_tech_stack.md`. Key references:
- Tech stack guidelines: `llm-shared/project_tech_stack.md`
- Function analysis tools: `llm-shared/utils/`
- Development best practices as defined in llm-shared

## Build and Development Commands

### Task-based Commands (Preferred)
```bash
task build       # Build the reddit-feed-generator binary
task build-linux # Build for Linux
task build-ci    # Build for CI environment
task test        # Run tests
task test-ci     # Run tests with CI tags and coverage
task lint        # Run linter and formatter
task run         # Run the application
task clean       # Clean build artifacts
```

### Legacy Commands (for reference)

### Build

```bash
go build -o build/reddit-feed-generator
```

### Test

```bash
go test -v
```

### Install Dependencies

```bash
go mod download
```

### Format Code

```bash
gofmt -w *.go
```

### Run the Application

```bash
./build/reddit-feed-generator
```

## Architecture Overview

This is a Go application that generates RSS/Atom feeds from a user's personalized Reddit homepage with OpenGraph metadata enhancement.

### Core Components

1. **OAuth2 Authentication Flow**: Uses Reddit's OAuth2 API with "installed app" flow (no client secret required)

   - Starts local HTTP server on port 8080 for callback
   - Stores tokens in `reddit_feed_config.json`
   - Automatically refreshes expired tokens

2. **Reddit API Integration**:

   - Fetches posts from `/best` endpoint (user's personalized feed)
   - Processes `RedditListing` and `RedditPost` structs
   - Applies configurable score and comment count filters

3. **OpenGraph Data Enhancement**:

   - Fetches OpenGraph metadata for external (non-Reddit) links
   - Uses SQLite database (`opengraph_cache.db`) for 24-hour caching
   - Skips Reddit URLs and blocked domains (x.com, twitter.com)
   - 8-second timeout for HTTP requests

4. **Feed Generation**:
   - Uses `github.com/gorilla/feeds` library
   - Supports both RSS and Atom formats
   - Enriches descriptions with OpenGraph previews

### Key Files

- `main.go`: Single-file application with all functionality
- `main_test.go`: Unit tests for URL filtering, OpenGraph parsing, and post filtering
- `reddit_feed_config.json`: User configuration and OAuth tokens
- `opengraph_cache.db`: SQLite cache for OpenGraph metadata

### Data Flow

1. Load config → Authenticate with Reddit → Fetch homepage posts
2. Filter posts by score/comments → Fetch OpenGraph data (with caching)
3. Generate RSS/Atom feed → Save to `reddit_homepage_feed.xml`

### Dependencies

- `github.com/gorilla/feeds`: Feed generation
- `golang.org/x/oauth2`: Reddit OAuth2 authentication
- `modernc.org/sqlite`: SQLite database (no CGO)
- `golang.org/x/net/html`: HTML parsing for OpenGraph extraction

### Configuration

The application creates `reddit_feed_config.json` with:

- Reddit OAuth credentials
- Score/comment filters
- Feed type (RSS/Atom)
- Output path

First run prompts for Reddit Client ID and filter preferences.

## Function Analysis

Use the llm-shared function analysis tool to understand the codebase:
```bash
go run llm-shared/utils/gofuncs.go -dir .
```
