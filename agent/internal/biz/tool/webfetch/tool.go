// Package webfetch implements the web_fetch local tool. It does a constrained
// HTTP(S) GET and converts HTML responses to readable text. Inspired by dsh
// `packages/web/web-fetch-http` but stripped of the cordis / ctx.web plumbing
// so it can run inside Fairy's flat LocalBackend.
package webfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"agentloop/agent/internal/biz/tool/shared"
	"agentloop/agent/internal/dtypes"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

type Options struct {
	MaxURLLength    int
	MaxResponseBytes int64
	MaxBodyChars    int
	Timeout         time.Duration
	MaxRedirects    int
	UserAgent       string
}

func DefaultOptions() Options {
	return Options{
		MaxURLLength:     2048,
		MaxResponseBytes: 5_000_000,
		MaxBodyChars:     100_000,
		Timeout:          30 * time.Second,
		MaxRedirects:     5,
		UserAgent:        defaultUserAgent,
	}
}

type Tool struct {
	schema  dtypes.ToolDef
	opts    Options
	client  *http.Client
}

func NewTool(schema dtypes.ToolDef) *Tool {
	return NewToolWithOptions(schema, DefaultOptions())
}

func NewToolWithOptions(schema dtypes.ToolDef, opts Options) *Tool {
	if opts.MaxURLLength <= 0 {
		opts.MaxURLLength = 2048
	}
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = 5_000_000
	}
	if opts.MaxBodyChars <= 0 {
		opts.MaxBodyChars = 100_000
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxRedirects < 0 {
		opts.MaxRedirects = 0
	}
	if opts.UserAgent == "" {
		opts.UserAgent = defaultUserAgent
	}
	return &Tool{
		schema: schema,
		opts:   opts,
		client: &http.Client{
			Timeout: opts.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        8,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

func (t *Tool) Name() string { return "web_fetch" }

func (t *Tool) Schema() dtypes.ToolDef { return t.schema }

func (t *Tool) Execute(ctx context.Context, invocation dtypes.ToolInvocation) (dtypes.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return dtypes.ToolResult{}, err
	}
	args, err := shared.DecodeArgs(invocation)
	if err != nil {
		return shared.ErrorResult("web_fetch", err), nil
	}
	url := strings.TrimSpace(shared.StringArg(args, "url"))
	if url == "" {
		return shared.ErrorResult("web_fetch", fmt.Errorf("url is required")), nil
	}
	if len(url) > t.opts.MaxURLLength {
		return shared.ErrorResult("web_fetch", fmt.Errorf("url length %d exceeds limit %d", len(url), t.opts.MaxURLLength)), nil
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return shared.ErrorResult("web_fetch", fmt.Errorf("only http(s) urls are supported")), nil
	}

	timeout := t.opts.Timeout
	if invocation.Timeout > 0 && invocation.Timeout < timeout {
		timeout = invocation.Timeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return shared.ErrorResult("web_fetch", fmt.Errorf("build request: %w", err)), nil
	}
	req.Header.Set("User-Agent", t.opts.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json")

	redirectsLeft := t.opts.MaxRedirects
	client := *t.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if redirectsLeft <= 0 {
			return fmt.Errorf("stopped after %d redirects", len(via))
		}
		redirectsLeft--
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return shared.ErrorResult("web_fetch", fmt.Errorf("request failed: %w", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to surface a tiny preview of the body so the model can diagnose.
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return shared.ErrorResult("web_fetch", fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(preview)))), nil
	}

	limited := io.LimitReader(resp.Body, t.opts.MaxResponseBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return shared.ErrorResult("web_fetch", fmt.Errorf("read body: %w", err)), nil
	}

	contentType := resp.Header.Get("Content-Type")
	bodyText := renderBody(contentType, body, t.opts.MaxBodyChars)

	return dtypes.ToolResult{Value: map[string]any{
		"tool":         "web_fetch",
		"ok":           true,
		"url":          url,
		"status":       resp.StatusCode,
		"content_type": contentType,
		"bytes":        len(body),
		"truncated":    int64(len(body)) >= t.opts.MaxResponseBytes,
		"text":         bodyText,
	}}, nil
}

// renderBody turns an HTTP response body into readable text. HTML is
// stripped of tags; everything else is returned as-is (truncated at the
// configured character cap).
func renderBody(contentType string, body []byte, maxChars int) string {
	ct := strings.ToLower(contentType)
	text := string(body)
	if strings.Contains(ct, "html") {
		text = htmlToText(text)
	}
	if maxChars > 0 && len(text) > maxChars {
		text = text[:maxChars] + "\n... [truncated]"
	}
	return text
}

// htmlToText does a single pass over an HTML document, removing tags and
// decoding a small set of entities. It is good enough for extracting article
// prose; for clean Markdown conversion use a dedicated library.
func htmlToText(s string) string {
	var b strings.Builder
	var pendingBlock strings.Builder
	inTag := false
	flushBlock := func() {
		t := collapseWS(pendingBlock.String())
		if t != "" {
			b.WriteString(t)
			b.WriteString("\n\n")
		}
		pendingBlock.Reset()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			if inTag {
				// Detect block-level closing tags to insert paragraph breaks.
				tagStart := lastOpenTag(s, i)
				if tagStart >= 0 {
					tag := strings.ToLower(s[tagStart+1 : i])
					switch tag {
					case "p", "/p", "br", "/br", "li", "/li", "h1", "/h1", "h2", "/h2", "h3", "/h3", "div", "/div":
						flushBlock()
					}
					if tag == "script" || tag == "style" {
						// Skip until matching close.
						closeName := tag
						idx := strings.Index(strings.ToLower(s[i:]), "</"+closeName+">")
						if idx > 0 {
							i += idx + len("</"+closeName+">")
						}
					}
				}
			}
			inTag = false
		case !inTag:
			if c == '&' {
				if rest, ok := decodeEntity(s[i:]); ok {
					pendingBlock.WriteString(rest)
					advance := strings.IndexAny(s[i:], ";")
					if advance > 0 {
						i += advance
					}
					continue
				}
			}
			pendingBlock.WriteByte(c)
		}
	}
	flushBlock()
	return strings.TrimSpace(b.String())
}

func lastOpenTag(s string, before int) int {
	// Walk backwards from `before` to find the matching '<' for the tag we
	// just closed. Cheap because tags are short.
	depth := 0
	for j := before - 1; j >= 0 && before-j < 200; j-- {
		if s[j] == '>' {
			depth++
		}
		if s[j] == '<' {
			if depth == 0 {
				return j
			}
			depth--
		}
	}
	return -1
}

func collapseWS(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func decodeEntity(s string) (string, bool) {
	if len(s) < 4 || s[0] != '&' {
		return "", false
	}
	end := strings.IndexByte(s, ';')
	if end < 0 || end > 8 {
		return "", false
	}
	switch s[:end+1] {
	case "&amp;":
		return "&", true
	case "&lt;":
		return "<", true
	case "&gt;":
		return ">", true
	case "&quot;":
		return `"`, true
	case "&apos;":
		return "'", true
	case "&nbsp;":
		return " ", true
	}
	return "", false
}
