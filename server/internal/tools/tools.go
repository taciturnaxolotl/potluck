// Package tools defines the web search and fetch tools available to the
// LLM during chat, and executes them server-side.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/fetch"
	"github.com/taciturnaxolotl/potluck/internal/provider"
	"github.com/taciturnaxolotl/potluck/internal/search"
)

// httpClient is shared across search and fetch.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// Definitions returns the OpenAI-shaped tool definitions to send in the
// chat request so the model knows what tools are available.
func Definitions() []provider.ToolDef {
	return []provider.ToolDef{
		{
			Type: "function",
			Function: provider.Function{
				Name:        "web_search",
				Description: "Search the web using DuckDuckGo. Returns a list of results with titles, URLs, and snippets. Use this when you need to find current information, look up facts, or research a topic.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "The search query. Use specific, targeted queries for best results.",
						},
						"max_results": map[string]any{
							"type":        "integer",
							"description": "Maximum number of results to return (1-20, default 10).",
							"default":     10,
						},
					},
					"required":             []string{"query"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: provider.Function{
				Name:        "web_fetch",
				Description: "Fetch the content of a web page and extract its text. Use this to read a specific URL found via web_search, or any URL the user provides. Returns the page text content (up to 100KB).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "The URL to fetch. Must start with http:// or https://.",
						},
					},
					"required":             []string{"url"},
					"additionalProperties": false,
				},
			},
		},
	}
}

// Execute runs a tool call and returns the result string.
func Execute(ctx context.Context, name string, arguments string) (string, error) {
	switch name {
	case "web_search":
		var params struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		results, err := search.Search(ctx, httpClient, params.Query, params.MaxResults)
		if err != nil {
			return "", err
		}
		return search.FormatResults(results), nil

	case "web_fetch":
		var params struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return fetch.Fetch(ctx, httpClient, params.URL)

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}
