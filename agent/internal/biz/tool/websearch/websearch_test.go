package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDuckDuckGoProviderParsesResultPage(t *testing.T) {
	const body = `<html><body>
<div class="result">
  <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa&abc=1">Result One Title</a>
  <a class="result__snippet">First snippet text.</a>
</div>
<div class="result">
  <a class="result__a" href="https://other.example.org/b">Result Two</a>
  <a class="result__snippet">Second snippet text.</a>
</div>
</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	// Point the provider at the test server via a transport shim.
	client := server.Client()
	provider := NewDuckDuckGoProvider(client)
	// We cannot easily redirect DDG's hardcoded host; instead swap the endpoint
	// indirectly by monkey-patching the URL inside Search. To keep this test
	// purely unit, we exercise parseDuckDuckGoHTML directly.
	results := parseDuckDuckGoHTML(body, 8)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d (%#v)", len(results), results)
	}
	if results[0].Title != "Result One Title" {
		t.Fatalf("title not extracted: %#v", results[0])
	}
	if !strings.HasPrefix(results[0].URL, "https://example.com/") {
		t.Fatalf("URL not unwrapped from DDG click tracker: %q", results[0].URL)
	}
	if results[0].Snippet != "First snippet text." {
		t.Fatalf("snippet not captured: %#v", results[0])
	}
	_ = provider
	_ = context.Background()
}

func TestBingProviderParsesBAlgoEntries(t *testing.T) {
	const body = `<html><body>
<li class="b_algo">
  <h2><a href="https://example.com/page">Example Page Title</a></h2>
  <p class="b_lineclamp4 b_algoSlug">Snippet for example.com.</p>
</li>
<li class="b_algo">
  <h2><a href="https://other.example.org/path">Second Result</a></h2>
  <p>This entry uses plain paragraph tags.</p>
</li>
</body></html>`

	results := parseBingHTML(body, 8)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d (%#v)", len(results), results)
	}
	if results[0].Title != "Example Page Title" || results[0].URL != "https://example.com/page" {
		t.Fatalf("first result wrong: %#v", results[0])
	}
	if results[1].URL != "https://other.example.org/path" {
		t.Fatalf("second result URL wrong: %#v", results[1])
	}
	if !strings.Contains(results[1].Snippet, "plain paragraph") {
		t.Fatalf("fallback snippet extraction failed: %#v", results[1])
	}
}

func TestCanonicalizeURLStripsFragment(t *testing.T) {
	got := CanonicalizeURL("https://Example.com/foo?bar=1#frag")
	want := "https://example.com/foo?bar=1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRacerReturnsFirstValid(t *testing.T) {
	// Two providers, one fast with results, one slow without — fast wins.
	fast := &fakeProvider{
		name: "fast",
		results: []SearchResult{
			{Title: "Fast A", URL: "https://a.example/"},
			{Title: "Fast B", URL: "https://b.example/"},
		},
	}
	slow := &fakeProvider{
		name:    "slow",
		delay:   200 * time.Millisecond,
		results: []SearchResult{{Title: "Slow X", URL: "https://x.example/"}},
	}
	r := NewRacer(Options{PerProviderTimeout: 500 * time.Millisecond, OverallTimeout: 1 * time.Second, MaxResults: 8}, fast, slow)
	results, providers, err := r.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) < 1 || results[0].Title != "Fast A" {
		t.Fatalf("expected fast winner, got %#v", results)
	}
	if !contains(providers, "fast") {
		t.Fatalf("providers should include 'fast', got %#v", providers)
	}
}

type fakeProvider struct {
	name    string
	delay   time.Duration
	results []SearchResult
	err     error
}

func (p *fakeProvider) Name() string { return p.name }
func (p *fakeProvider) Search(ctx context.Context, query string, n int) ([]SearchResult, error) {
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.results, nil
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
