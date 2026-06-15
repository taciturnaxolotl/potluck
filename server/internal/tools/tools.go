// Package tools defines the web search and fetch tools available to the
// LLM during chat, and executes them server-side.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/taciturnaxolotl/potluck/internal/fetch"
	"github.com/taciturnaxolotl/potluck/internal/provider"
	"github.com/taciturnaxolotl/potluck/internal/search"
	"github.com/taciturnaxolotl/potluck/internal/store"
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
		{
			Type: "function",
			Function: provider.Function{
				Name:        "set_memory",
				Description: "Store a persistent memory key-value pair for this user. Use this to remember stable user preferences, profile details, or recurring constraints that should carry across chats.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"key": map[string]any{
							"type":        "string",
							"description": "Memory key (1-64 chars), e.g. preferred_name, timezone, coding_style.",
						},
						"value": map[string]any{
							"type":        "string",
							"description": "Memory value (up to 2048 chars).",
						},
					},
					"required":             []string{"key", "value"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: provider.Function{
				Name:        "get_memory",
				Description: "Read persistent memory values for this user. If key is omitted, returns all stored memory rows.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"key": map[string]any{
							"type":        "string",
							"description": "Optional key to read a single memory value.",
						},
					},
					"additionalProperties": false,
				},
			},
		},
	}
}

// FantasyDefinitions returns tool definitions in fantasy's format.
func FantasyDefinitions() []fantasy.Tool {
	defs := Definitions()
	out := make([]fantasy.Tool, len(defs))
	for i, d := range defs {
		out[i] = fantasy.FunctionTool{
			Name:        d.Function.Name,
			Description: d.Function.Description,
			InputSchema: d.Function.Parameters,
		}
	}
	return out
}

// Execute runs a tool call and returns the result string.
func Execute(ctx context.Context, q *store.Queries, userID string, name string, arguments string) (string, error) {
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

	case "set_memory":
		var params struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(params.Key) == "" || len(params.Key) > 64 {
			return "", fmt.Errorf("invalid key: must be 1-64 chars")
		}
		if len(params.Value) > 2048 {
			return "", fmt.Errorf("invalid value: must be <= 2048 chars")
		}
		if err := q.UpsertUserMemory(ctx, store.UpsertUserMemoryParams{
			UserID: userID,
			Key:    strings.TrimSpace(params.Key),
			Value:  params.Value,
		}); err != nil {
			return "", fmt.Errorf("set memory: %w", err)
		}
		return "ok", nil

	case "get_memory":
		var params struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		key := strings.TrimSpace(params.Key)
		if key != "" {
			row, err := q.GetUserMemoryKey(ctx, store.GetUserMemoryKeyParams{UserID: userID, Key: key})
			if err != nil {
				return "", fmt.Errorf("get memory key: %w", err)
			}
			return row.Key + "=" + row.Value, nil
		}
		rows, err := q.GetUserMemory(ctx, userID)
		if err != nil {
			return "", fmt.Errorf("get memory: %w", err)
		}
		if len(rows) == 0 {
			return "no memory stored", nil
		}
		var b strings.Builder
		for _, r := range rows {
			fmt.Fprintf(&b, "%s=%s\n", r.Key, r.Value)
		}
		return strings.TrimSpace(b.String()), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}
