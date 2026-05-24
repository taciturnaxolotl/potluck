// Package fetch retrieves web pages and extracts text content.
// Intentionally minimal — no JS rendering, no cookies, 100KB cap.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const maxSize = 100 * 1024 // 100KB

// Fetch retrieves a URL and returns the text content.
// For HTML pages, extracts visible text. For other content types,
// returns the raw body (up to maxSize bytes).
func Fetch(ctx context.Context, client *http.Client, urlStr string) (string, error) {
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return "", fmt.Errorf("fetch: URL must start with http:// or https://")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("fetch: create request: %w", err)
	}
	req.Header.Set("User-Agent", "potluck/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch: execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return "", fmt.Errorf("fetch: read body: %w", err)
	}

	content := string(body)
	if !utf8.ValidString(content) {
		return "", fmt.Errorf("fetch: response is not valid UTF-8")
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		text, err := extractText(content)
		if err != nil {
			// Fall back to raw content if extraction fails
			return truncate(content), nil
		}
		return truncate(text), nil
	}

	return truncate(content), nil
}

func extractText(htmlContent string) (string, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", err
	}

	// Remove script, style, nav, footer elements
	var remove []*html.Node
	collectToRemove(doc, &remove)
	for _, n := range remove {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}

	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(text)
			}
		}
		// Add newlines for block elements
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr":
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return b.String(), nil
}

func collectToRemove(n *html.Node, out *[]*html.Node) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "nav", "footer", "header", "aside", "noscript":
			*out = append(*out, n)
			return // don't recurse into removed elements
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectToRemove(c, out)
	}
}

func truncate(s string) string {
	if len(s) > maxSize {
		return s[:maxSize] + fmt.Sprintf("\n\n[Content truncated to %d bytes]", maxSize)
	}
	return s
}
