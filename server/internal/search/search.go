// Package search provides DuckDuckGo Lite search, ported from Crush's
// implementation. No API key needed — scrapes the HTML results page.
package search

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// Result is a single search hit.
type Result struct {
	Title    string
	Link     string
	Snippet  string
	Position int
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15",
}

var acceptLanguages = []string{
	"en-US,en;q=0.9",
	"en-US,en;q=0.9,es;q=0.8",
	"en-GB,en;q=0.9,en-US;q=0.8",
}

// Rate-limit: enforce a minimum gap between searches to avoid getting blocked.
var (
	searchMu     sync.Mutex
	lastSearchAt time.Time
	minGap       = 500 * time.Millisecond
)

func maybeDelay() {
	searchMu.Lock()
	defer searchMu.Unlock()
	elapsed := time.Since(lastSearchAt)
	if elapsed < minGap {
		time.Sleep(minGap - elapsed)
	}
	lastSearchAt = time.Now()
}

// Search queries DuckDuckGo Lite and returns up to maxResults results.
func Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]Result, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 20 {
		maxResults = 20
	}

	maybeDelay()

	searchURL := "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("search: create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", acceptLanguages[rand.Intn(len(acceptLanguages))])
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Connection", "keep-alive")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search: execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("search: read body: %w", err)
	}

	return parseResults(string(body), maxResults), nil
}

func parseResults(htmlContent string, maxResults int) []Result {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	var results []Result
	var current *Result

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "a" && hasClass(n, "result-link") {
				if current != nil && current.Link != "" {
					current.Position = len(results) + 1
					results = append(results, *current)
					if len(results) >= maxResults {
						return
					}
				}
				current = &Result{Title: getText(n)}
				for _, attr := range n.Attr {
					if attr.Key == "href" {
						current.Link = cleanURL(attr.Val)
						break
					}
				}
			}
			if n.Data == "td" && hasClass(n, "result-snippet") && current != nil {
				current.Snippet = getText(n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if len(results) >= maxResults {
				return
			}
			traverse(c)
		}
	}

	traverse(doc)

	if current != nil && current.Link != "" && len(results) < maxResults {
		current.Position = len(results) + 1
		results = append(results, *current)
	}
	return results
}

func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" && slices.Contains(strings.Fields(attr.Val), class) {
			return true
		}
	}
	return false
}

func getText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

func cleanURL(rawURL string) string {
	if strings.HasPrefix(rawURL, "//duckduckgo.com/l/?uddg=") {
		if _, after, ok := strings.Cut(rawURL, "uddg="); ok {
			encoded := after
			if idx := strings.Index(encoded, "&"); idx != -1 {
				encoded = encoded[:idx]
			}
			if decoded, err := url.QueryUnescape(encoded); err == nil {
				return decoded
			}
		}
	}
	return rawURL
}

// FormatResults renders search results as text for LLM consumption.
func FormatResults(results []Result) string {
	if len(results) == 0 {
		return "No results found. Try rephrasing your search."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d search results:\n\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n", r.Position, r.Title)
		fmt.Fprintf(&sb, "   URL: %s\n", r.Link)
		fmt.Fprintf(&sb, "   Summary: %s\n\n", r.Snippet)
	}
	return sb.String()
}
