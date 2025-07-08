package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// OpenGraphFetcher handles concurrent OpenGraph metadata fetching
type OpenGraphFetcher struct {
	client *http.Client
	mu     sync.RWMutex
	cache  map[string]*OpenGraphData
	db     *OpenGraphDB
}

// NewOpenGraphFetcher creates a new OpenGraph fetcher with database backing
func NewOpenGraphFetcher(db *OpenGraphDB) *OpenGraphFetcher {
	return &OpenGraphFetcher{
		client: &http.Client{
			Timeout: 8 * time.Second, // 8 second timeout as requested (5-10 seconds)
		},
		cache: make(map[string]*OpenGraphData),
		db:    db,
	}
}

// FetchOpenGraphData fetches OpenGraph metadata from a URL with enhanced error handling
func (ogf *OpenGraphFetcher) FetchOpenGraphData(url string) (*OpenGraphData, error) {
	// Validate URL format
	if !isValidURL(url) {
		return nil, fmt.Errorf("invalid URL format: %s", url)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set a comprehensive User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GoRedditFeedGenerator/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Connection", "keep-alive")

	resp, err := ogf.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml") {
		slog.Debug("Skipping non-HTML content", "url", url, "content_type", contentType)
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}

	// Handle compression (gzip/deflate)
	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer reader.Close()
	default:
		reader = resp.Body
	}

	// Read response body with size limit
	const maxBodySize = 1024 * 1024 // 1MB limit
	body, err := io.ReadAll(io.LimitReader(reader, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Convert body to UTF-8 string with proper encoding detection
	htmlContent, err := ogf.convertToUTF8(body, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("failed to convert content to UTF-8: %w", err)
	}

	// Parse OpenGraph tags
	og, err := ogf.parseOpenGraphTags(htmlContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenGraph tags: %w", err)
	}

	// Set metadata
	now := time.Now()
	og.URL = url
	og.FetchedAt = now
	og.ExpiresAt = now.Add(time.Duration(OpenGraphCacheHours) * time.Hour)

	// Validate and clean up the data
	og = ogf.cleanupOpenGraphData(og)

	return og, nil
}

// parseOpenGraphTags extracts OpenGraph meta tags from HTML with fallbacks
func (ogf *OpenGraphFetcher) parseOpenGraphTags(htmlContent string) (*OpenGraphData, error) {
	og := &OpenGraphData{}

	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract meta tags
	var extractMeta func(*html.Node)
	extractMeta = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "meta":
				ogf.processMetaTag(n, og)
			case "title":
				if og.Title == "" && n.FirstChild != nil {
					og.Title = strings.TrimSpace(n.FirstChild.Data)
				}
			}
		}

		// Recursively process child nodes
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractMeta(c)
		}
	}

	extractMeta(doc)

	// Apply fallbacks if primary OpenGraph tags are missing
	ogf.applyFallbacks(og, htmlContent)

	// Log successful extraction
	slog.Debug("OpenGraph extraction successful", "url", og.URL, "title", og.Title, "has_desc", og.Description != "", "has_image", og.Image != "")

	return og, nil
}

// processMetaTag processes individual meta tags
func (ogf *OpenGraphFetcher) processMetaTag(n *html.Node, og *OpenGraphData) {
	var property, content, name string

	for _, attr := range n.Attr {
		switch attr.Key {
		case "property":
			property = attr.Val
		case "content":
			content = attr.Val
		case "name":
			name = attr.Val
		}
	}

	// Process OpenGraph properties
	switch property {
	case "og:title":
		og.Title = content
	case "og:description":
		og.Description = content
	case "og:image":
		og.Image = content
	case "og:site_name":
		og.SiteName = content
	}

	// Process fallback meta tags
	if og.Description == "" {
		switch name {
		case "description":
			og.Description = content
		case "twitter:description":
			og.Description = content
		}
	}

	if og.Image == "" {
		switch name {
		case "twitter:image":
			og.Image = content
		}
	}

	if og.Title == "" {
		switch name {
		case "twitter:title":
			og.Title = content
		}
	}
}

// applyFallbacks applies fallback strategies for missing OpenGraph data
func (ogf *OpenGraphFetcher) applyFallbacks(og *OpenGraphData, htmlContent string) {
	// If no description, try to extract from first paragraph
	if og.Description == "" {
		og.Description = ogf.extractFirstParagraph(htmlContent)
	}

	// If no site name, try to extract from domain
	if og.SiteName == "" && og.URL != "" {
		if u, err := url.Parse(og.URL); err == nil {
			og.SiteName = u.Host
		}
	}
}

// extractFirstParagraph extracts the first paragraph from HTML content
func (ogf *OpenGraphFetcher) extractFirstParagraph(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}

	var findFirstP func(*html.Node) string
	findFirstP = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "p" {
			var text strings.Builder
			var extractText func(*html.Node)
			extractText = func(node *html.Node) {
				if node.Type == html.TextNode {
					text.WriteString(node.Data)
				}
				for c := node.FirstChild; c != nil; c = c.NextSibling {
					extractText(c)
				}
			}
			extractText(n)

			result := strings.TrimSpace(text.String())
			if len(result) > 20 { // Only return if it's substantial
				return result
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if result := findFirstP(c); result != "" {
				return result
			}
		}
		return ""
	}

	return findFirstP(doc)
}

// cleanupOpenGraphData validates and cleans up OpenGraph data
func (ogf *OpenGraphFetcher) cleanupOpenGraphData(og *OpenGraphData) *OpenGraphData {
	// Truncate long descriptions
	if len(og.Description) > 500 {
		og.Description = og.Description[:497] + "..."
	}

	// Truncate long titles
	if len(og.Title) > 200 {
		og.Title = og.Title[:197] + "..."
	}

	// Validate image URL
	if og.Image != "" && !isValidURL(og.Image) {
		slog.Warn("Invalid image URL found, clearing", "url", og.Image)
		og.Image = ""
	}

	// Clean up whitespace and normalize
	og.Title = strings.TrimSpace(og.Title)
	og.Description = strings.TrimSpace(og.Description)
	og.SiteName = strings.TrimSpace(og.SiteName)

	// Remove any null bytes or control characters that might cause issues
	og.Title = strings.ReplaceAll(og.Title, "\x00", "")
	og.Description = strings.ReplaceAll(og.Description, "\x00", "")
	og.SiteName = strings.ReplaceAll(og.SiteName, "\x00", "")

	return og
}

// GetOpenGraphPreview gets OpenGraph data for a URL, using cache when possible
func (ogf *OpenGraphFetcher) GetOpenGraphPreview(url string) *OpenGraphData {
	// Check if it's a Reddit URL - skip OpenGraph for Reddit links
	if isRedditURL(url) {
		slog.Debug("Skipping Reddit URL", "url", url)
		return nil
	}

	// Check if it's a blocked URL - skip OpenGraph for blocked domains
	if isBlockedURL(url) {
		slog.Debug("Skipping blocked URL", "url", url)
		return nil
	}

	// Try to get from database cache first
	if ogf.db != nil {
		cached, err := ogf.db.GetCachedOpenGraph(url)
		if err != nil {
			slog.Warn("Error reading OpenGraph cache", "url", url, "error", err)
		}
		if cached != nil {
			return cached
		}
	}

	// Fetch new OpenGraph data
	slog.Info("Fetching OpenGraph data", "url", url)
	og, err := ogf.FetchOpenGraphData(url)
	if err != nil {
		slog.Warn("Failed to fetch OpenGraph data", "url", url, "error", err)
		return nil
	}

	slog.Debug("OpenGraph data fetched successfully", "url", url, "title", og.Title, "description_length", len(og.Description))

	// Save to database cache
	if ogf.db != nil {
		err = ogf.db.SaveCachedOpenGraph(og)
		if err != nil {
			slog.Warn("Failed to cache OpenGraph data", "url", url, "error", err)
		}
	}

	return og
}

// FetchConcurrentOpenGraph fetches OpenGraph data for multiple URLs concurrently
func (ogf *OpenGraphFetcher) FetchConcurrentOpenGraph(urls []string) map[string]*OpenGraphData {
	if len(urls) == 0 {
		return nil
	}

	type result struct {
		url string
		og  *OpenGraphData
	}

	results := make(chan result, len(urls))
	var wg sync.WaitGroup

	// Limit concurrent requests
	const maxConcurrent = 5
	semaphore := make(chan struct{}, maxConcurrent)

	slog.Info("Starting concurrent OpenGraph fetch", "total_urls", len(urls))
	for _, url := range urls {
		if url == "" {
			continue
		}

		wg.Add(1)
		go func(u string) {
			defer wg.Done()

			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			slog.Debug("Processing URL for OpenGraph", "url", u)
			og := ogf.GetOpenGraphPreview(u)
			if og != nil {
				slog.Debug("OpenGraph preview obtained", "url", u, "title", og.Title)
			} else {
				slog.Debug("No OpenGraph preview obtained", "url", u)
			}
			results <- result{url: u, og: og}
		}(url)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	data := make(map[string]*OpenGraphData)
	for res := range results {
		if res.og != nil {
			data[res.url] = res.og
		}
	}

	return data
}

// isValidURL checks if a URL is valid
func isValidURL(urlStr string) bool {
	u, err := url.Parse(urlStr)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// isRedditURL checks if a URL is a Reddit URL
func isRedditURL(url string) bool {
	return strings.Contains(url, "reddit.com") || strings.Contains(url, "redd.it")
}

// isBlockedURL checks if a URL is from a domain that blocks external access
func isBlockedURL(url string) bool {
	blockedDomains := []string{
		"x.com",
		"twitter.com",
		"facebook.com",
		"instagram.com",
		"linkedin.com",
		"i.redd.it",          // Reddit image URLs don't have useful OpenGraph
		"v.redd.it",          // Reddit video URLs don't have useful OpenGraph
		"reddit.com/gallery", // Reddit gallery URLs don't have useful OpenGraph
	}

	for _, domain := range blockedDomains {
		if strings.Contains(url, domain) {
			return true
		}
	}
	return false
}

// convertToUTF8 converts response body to UTF-8 string with proper encoding detection
func (ogf *OpenGraphFetcher) convertToUTF8(body []byte, contentType string) (string, error) {
	// Try to detect encoding from content type or HTML meta tags
	reader := strings.NewReader(string(body))

	// Use charset package to detect and convert encoding
	utf8Reader, err := charset.NewReader(reader, contentType)
	if err != nil {
		// If charset detection fails, assume UTF-8
		slog.Warn("Failed to detect charset, assuming UTF-8", "error", err)
		return string(body), nil
	}

	// Read the UTF-8 converted content
	utf8Bytes, err := io.ReadAll(utf8Reader)
	if err != nil {
		return "", fmt.Errorf("failed to convert to UTF-8: %w", err)
	}

	return string(utf8Bytes), nil
}
