package websearch

import (
	"context"
	"fmt"
	"net/http"

	"agentloop/agent/internal/biz/tool/shared"
	"agentloop/agent/internal/dtypes"
)

type Tool struct {
	schema  dtypes.ToolDef
	racer   *Racer
	factory func() *http.Client // injected for tests
}

func NewTool(schema dtypes.ToolDef) *Tool {
	client := defaultHTTPClient()
	return NewToolWithClient(schema, client)
}

func NewToolWithClient(schema dtypes.ToolDef, client *http.Client) *Tool {
	providers := []Provider{
		NewDuckDuckGoProvider(client),
		NewBingPublicProvider(client),
	}
	return &Tool{
		schema:  schema,
		racer:   NewRacer(DefaultOptions(), providers...),
		factory: func() *http.Client { return client },
	}
}

func (t *Tool) Name() string { return "web_search" }

func (t *Tool) Schema() dtypes.ToolDef { return t.schema }

func (t *Tool) Execute(ctx context.Context, invocation dtypes.ToolInvocation) (dtypes.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return dtypes.ToolResult{}, err
	}
	args, err := shared.DecodeArgs(invocation)
	if err != nil {
		return shared.ErrorResult("web_search", err), nil
	}
	single := shared.StringArg(args, "query", "q")
	batch := decodeStringSlice(args["queries"])

	queries := make([]string, 0, 1+len(batch))
	if single != "" {
		queries = append(queries, single)
	}
	queries = append(queries, batch...)
	if len(queries) == 0 {
		return shared.ErrorResult("web_search", fmt.Errorf("query (or queries) is required")), nil
	}

	// Single-query path: race providers, return first non-empty.
	if len(queries) == 1 {
		results, providers, err := t.racer.Search(ctx, queries[0])
		if err != nil {
			return shared.ErrorResult("web_search", err), nil
		}
		return dtypes.ToolResult{Value: map[string]any{
			"tool":      "web_search",
			"ok":        true,
			"query":     queries[0],
			"count":     len(results),
			"hits":      toHits(results),
			"providers": providers,
		}}, nil
	}

	// Batch path: each query gets its own racer round.
	type batchEntry struct {
		Query  string           `json:"query"`
		Hits   []map[string]any `json:"hits"`
		Count  int              `json:"count"`
		Failed bool             `json:"failed,omitempty"`
		Error  string           `json:"error,omitempty"`
	}
	entries := make([]batchEntry, len(queries))
	for i, q := range queries {
		results, _, err := t.racer.Search(ctx, q)
		if err != nil {
			entries[i] = batchEntry{Query: q, Failed: true, Error: err.Error()}
			continue
		}
		entries[i] = batchEntry{Query: q, Hits: toHits(results), Count: len(results)}
	}
	return dtypes.ToolResult{Value: map[string]any{
		"tool":    "web_search",
		"ok":      true,
		"count":   len(queries),
		"results": entries,
	}}, nil
}

func toHits(results []SearchResult) []map[string]any {
	hits := make([]map[string]any, 0, len(results))
	for _, r := range results {
		hits = append(hits, map[string]any{
			"title":   r.Title,
			"url":     r.URL,
			"snippet": r.Snippet,
		})
	}
	return hits
}

// decodeStringSlice extracts a JSON array of strings from an untyped argument.
// It accepts []any (typical) or a single string (used as a one-element batch).
func decodeStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	}
	return nil
}
