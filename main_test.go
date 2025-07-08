package main

import (
	"testing"
)

func TestIsRedditURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.reddit.com/r/golang", true},
		{"https://old.reddit.com/r/programming", true},
		{"https://redd.it/abc123", true},
		{"https://example.com", false},
		{"https://github.com/golang/go", false},
		{"https://news.ycombinator.com", false},
	}

	for _, test := range tests {
		result := isRedditURL(test.url)
		if result != test.expected {
			t.Errorf("isRedditURL(%q) = %v; expected %v", test.url, result, test.expected)
		}
	}
}

func TestIsBlockedURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://x.com/user/status/123", true},
		{"https://www.x.com/user/status/123", true},
		{"https://twitter.com/user/status/123", true},
		{"https://www.twitter.com/user/status/123", true},
		{"https://example.com", false},
		{"https://github.com/golang/go", false},
		{"https://news.ycombinator.com", false},
		{"https://reddit.com/r/golang", false},
	}

	for _, test := range tests {
		result := isBlockedURL(test.url)
		if result != test.expected {
			t.Errorf("isBlockedURL(%q) = %v; expected %v", test.url, result, test.expected)
		}
	}
}

func TestParseOpenGraphTags(t *testing.T) {
	htmlContent := `
	<html>
	<head>
		<meta property="og:title" content="Test Title" />
		<meta property="og:description" content="Test Description" />
		<meta property="og:image" content="https://example.com/image.jpg" />
		<meta property="og:site_name" content="Test Site" />
	</head>
	<body>
		<h1>Test Page</h1>
	</body>
	</html>
	`

	og, err := parseOpenGraphTags(htmlContent)
	if err != nil {
		t.Fatalf("parseOpenGraphTags failed: %v", err)
	}

	if og.Title != "Test Title" {
		t.Errorf("Expected title 'Test Title', got '%s'", og.Title)
	}

	if og.Description != "Test Description" {
		t.Errorf("Expected description 'Test Description', got '%s'", og.Description)
	}

	if og.Image != "https://example.com/image.jpg" {
		t.Errorf("Expected image 'https://example.com/image.jpg', got '%s'", og.Image)
	}

	if og.SiteName != "Test Site" {
		t.Errorf("Expected site name 'Test Site', got '%s'", og.SiteName)
	}
}

func TestParseOpenGraphTagsEmpty(t *testing.T) {
	htmlContent := `
	<html>
	<head>
		<title>Regular Title</title>
	</head>
	<body>
		<h1>Test Page</h1>
	</body>
	</html>
	`

	og, err := parseOpenGraphTags(htmlContent)
	if err != nil {
		t.Fatalf("parseOpenGraphTags failed: %v", err)
	}

	// With enhanced fallback behavior, regular title should be extracted
	if og.Title != "Regular Title" {
		t.Errorf("Expected title 'Regular Title', got '%s'", og.Title)
	}

	if og.Description != "" {
		t.Errorf("Expected empty description, got '%s'", og.Description)
	}
}

func TestParseOpenGraphTagsNoTitle(t *testing.T) {
	htmlContent := `
	<html>
	<head>
	</head>
	<body>
		<h1>Test Page</h1>
	</body>
	</html>
	`

	og, err := parseOpenGraphTags(htmlContent)
	if err != nil {
		t.Fatalf("parseOpenGraphTags failed: %v", err)
	}

	// With no title tag, should be empty
	if og.Title != "" {
		t.Errorf("Expected empty title, got '%s'", og.Title)
	}

	if og.Description != "" {
		t.Errorf("Expected empty description, got '%s'", og.Description)
	}
}

func TestFilterPosts(t *testing.T) {
	posts := []RedditPost{
		{Data: struct {
			Title       string  `json:"title"`
			URL         string  `json:"url"`
			Permalink   string  `json:"permalink"`
			CreatedUTC  float64 `json:"created_utc"`
			Score       int     `json:"score"`
			NumComments int     `json:"num_comments"`
			Author      string  `json:"author"`
			Subreddit   string  `json:"subreddit"`
		}{
			Title: "High Score Post", Score: 100, NumComments: 50,
		}},
		{Data: struct {
			Title       string  `json:"title"`
			URL         string  `json:"url"`
			Permalink   string  `json:"permalink"`
			CreatedUTC  float64 `json:"created_utc"`
			Score       int     `json:"score"`
			NumComments int     `json:"num_comments"`
			Author      string  `json:"author"`
			Subreddit   string  `json:"subreddit"`
		}{
			Title: "Low Score Post", Score: 5, NumComments: 2,
		}},
	}

	filtered := filterPosts(posts, 50, 10)
	if len(filtered) != 1 {
		t.Errorf("Expected 1 filtered post, got %d", len(filtered))
	}

	if filtered[0].Data.Title != "High Score Post" {
		t.Errorf("Expected 'High Score Post', got '%s'", filtered[0].Data.Title)
	}
}
