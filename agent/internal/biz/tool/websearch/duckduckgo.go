package websearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// DuckDuckGoProvider scrapes https://html.duckduckgo.com/html/?q=... The HTML
// variant requires no JS and no API key; rate-limiting kicks in around 30+
// requests/minute per IP, so callers should debounce. Inspired by dsh's
// approach of treating HTML providers as a fallback transport.
type DuckDuckGoProvider struct {
	client *http.Client
}

func NewDuckDuckGoProvider(client *http.Client) *DuckDuckGoProvider {
	if client == nil {
		client = defaultHTTPClient()
	}
	return &DuckDuckGoProvider{client: client}
}

func (p *DuckDuckGoProvider) Name() string { return "duckduckgo_html" }

func (p *DuckDuckGoProvider) Search(ctx context.Context, query string, n int) ([]SearchResult, error) {
	if n <= 0 {
		n = 8
	}
	form := url.Values{}
	form.Set("q", query)
	form.Set("kl", "us-en")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build ddg request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ddg request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ddg returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("ddg read body: %w", err)
	}
	results := parseDuckDuckGoHTML(string(body), n)
	if len(results) == 0 {
		return nil, fmt.Errorf("ddg returned no parseable hits (possibly bot-detection page)")
	}
	return results, nil
}

// parseDuckDuckGoHTML extracts result entries from a DDG HTML page. The layout
// we target puts the result title in <a class="result__a" href="..."> and
// the snippet in <a class="result__snippet">. URLs are wrapped in a click-
// tracker (//duckduckgo.com/l/?uddg=...) which we unwrap to the real target.
func parseDuckDuckGoHTML(body string, n int) []SearchResult {
	anchors := findAnchors(body)
	results := make([]SearchResult, 0, n)
	for _, a := range anchors {
		if !hasClassToken(a.Class, "result__a") {
			continue
		}
		r := SearchResult{
			Title: strings.TrimSpace(a.Text),
			URL:   unwrapDDGClick(a.Href),
		}
		r.URL = CanonicalizeURL(r.URL)
		if r.URL == "" || r.Title == "" {
			continue
		}
		// Look ahead in the anchors list for the matching snippet. The DDG
		// HTML layout puts the result__snippet anchor immediately after the
		// title anchor; scanning nearby keeps the parser cheap.
		for j := 0; j < len(anchors) && j < 32; j++ {
			if hasClassToken(anchors[j].Class, "result__snippet") {
				r.Snippet = strings.TrimSpace(anchors[j].Text)
				break
			}
		}
		results = append(results, r)
		if len(results) >= n {
			break
		}
	}
	return results
}

func unwrapDDGClick(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Host == "duckduckgo.com" && u.Path == "/l/" {
		if target := u.Query().Get("uddg"); target != "" {
			return target
		}
	}
	return raw
}
