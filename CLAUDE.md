# Red RSS Project Guidelines

This project uses llm-shared for standardized development practices.

## Project Overview

A Go application that generates RSS/Atom feeds from Reddit data using OAuth2 authentication.

## Guidelines Reference

- Tech stack guidelines: llm-shared/project_tech_stack.md
- Function analysis tools: llm-shared/utils/
- Development best practices: llm-shared/USAGE.md

## Function Analysis

- For Go projects: `go run llm-shared/utils/gofuncs.go -dir .`

## Build Commands

Since this project doesn't have a Taskfile.yml yet, use standard Go commands:

- Build: `go build -o build/red-rss`
- Test: `go test ./...`
- Format: `gofmt -w .`
- Run: `go run main.go`

## Development Guidelines

- Always run `gofmt -w .` after making changes to Go code
- Follow the Go library preferences from llm-shared/project_tech_stack.md
- Use standard library packages when possible
- For OAuth2: using golang.org/x/oauth2 (already in project)
- For RSS/Atom feeds: using github.com/gorilla/feeds (already in project)

## Project Structure

- Main application: `main.go`
- Configuration: `reddit_feed_config.json`
- Output: RSS/Atom feeds generated based on config

## Testing

- Write unit tests for easily testable functions
- Focus on critical parts of the code rather than 100% coverage
- Run tests before building

## Configuration

The application uses OAuth2 for Reddit API access and generates feeds based on:

- Score filtering
- Comment filtering
- Feed type (RSS or Atom)
- Output path configuration
