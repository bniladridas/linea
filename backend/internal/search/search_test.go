package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShouldSearch(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "latest", message: "what is the latest Go release", want: true},
		{name: "web", message: "search the web for Linea", want: true},
		{name: "ordinary chat", message: "help me write a function", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ShouldSearch(test.message); got != test.want {
				t.Fatalf("ShouldSearch(%q) = %v, want %v", test.message, got, test.want)
			}
		})
	}
}

func TestQueryFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "search", message: "search OpenAI", want: "OpenAI"},
		{name: "web search", message: "search the web for OpenAI", want: "OpenAI"},
		{name: "polite web search", message: "please search the web for Go release", want: "Go release"},
		{name: "plain latest", message: "what is the latest Go release", want: "what is the latest Go release"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := QueryFromMessage(test.message); got != test.want {
				t.Fatalf("QueryFromMessage(%q) = %q, want %q", test.message, got, test.want)
			}
		})
	}
}

func TestSearchUsesBraveWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/brave":
			if got := r.Header.Get("X-Subscription-Token"); got != "token" {
				t.Fatalf("brave token = %q, want token", got)
			}
			_, _ = w.Write([]byte(`{"web":{"results":[{"title":"Linea","url":"https://example.com/linea","description":"Local assistant"}]}}`))
		case "/instant":
			_, _ = w.Write([]byte(`{"Heading":"","AbstractText":"","RelatedTopics":[]}`))
		case "/html":
			_, _ = w.Write([]byte(`<html><body></body></html>`))
		case "/wiki":
			_, _ = w.Write([]byte(`{"query":{"search":[]}}`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(WithBraveAPIKey("token"))
	client.httpClient = server.Client()
	client.braveEndpoint = server.URL + "/brave"
	client.duckInstantEndpoint = server.URL + "/instant"
	client.duckHTMLEndpoint = server.URL + "/html"
	client.wikipediaEndpoint = server.URL + "/wiki"

	results, err := client.Search(context.Background(), "linea")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com/linea" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSearchFallsBackToDuckHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/instant":
			_, _ = w.Write([]byte(`{"Heading":"","AbstractText":"","RelatedTopics":[]}`))
		case "/html":
			_, _ = w.Write([]byte(`<html><body>
				<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fdocs">Docs &amp; Search</a>
				<a class="result__snippet" href="/x">Useful <b>documentation</b>.</a>
			</body></html>`))
		case "/wiki":
			_, _ = w.Write([]byte(`{"query":{"search":[]}}`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient()
	client.httpClient = server.Client()
	client.duckInstantEndpoint = server.URL + "/instant"
	client.duckHTMLEndpoint = server.URL + "/html"
	client.wikipediaEndpoint = server.URL + "/wiki"

	results, err := client.Search(context.Background(), "linea docs")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1: %#v", len(results), results)
	}
	if results[0].Title != "Docs & Search" || results[0].URL != "https://example.com/docs" || results[0].Snippet != "Useful documentation." {
		t.Fatalf("result = %#v", results[0])
	}
}

func TestSearchUsesSearXNGWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			if got := r.URL.Query().Get("format"); got != "json" {
				t.Fatalf("format = %q, want json", got)
			}
			_, _ = w.Write([]byte(`{"results":[{"title":"SearXNG result","url":"https://example.com/search","content":"Metasearch result"}]}`))
		case "/instant":
			_, _ = w.Write([]byte(`{"Heading":"","AbstractText":"","RelatedTopics":[]}`))
		case "/html":
			_, _ = w.Write([]byte(`<html><body></body></html>`))
		case "/wiki":
			_, _ = w.Write([]byte(`{"query":{"search":[]}}`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(WithSearXNGURL(server.URL))
	client.httpClient = server.Client()
	client.duckInstantEndpoint = server.URL + "/instant"
	client.duckHTMLEndpoint = server.URL + "/html"
	client.wikipediaEndpoint = server.URL + "/wiki"

	results, err := client.Search(context.Background(), "linea")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com/search" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSearchSkipsDuckDuckGoWhenConfiguredProvidersFillLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{"results":[
				{"title":"Result 1","url":"https://example.com/1","content":"one"},
				{"title":"Result 2","url":"https://example.com/2","content":"two"},
				{"title":"Result 3","url":"https://example.com/3","content":"three"},
				{"title":"Result 4","url":"https://example.com/4","content":"four"},
				{"title":"Result 5","url":"https://example.com/5","content":"five"},
				{"title":"Result 6","url":"https://example.com/6","content":"six"},
				{"title":"Result 7","url":"https://example.com/7","content":"seven"},
				{"title":"Result 8","url":"https://example.com/8","content":"eight"}
			]}`))
		case "/instant", "/html", "/wiki":
			t.Fatalf("unexpected fallback request to %s", r.URL.Path)
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(WithSearXNGURL(server.URL))
	client.httpClient = server.Client()
	client.duckInstantEndpoint = server.URL + "/instant"
	client.duckHTMLEndpoint = server.URL + "/html"
	client.wikipediaEndpoint = server.URL + "/wiki"

	results, err := client.Search(context.Background(), "linea")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 8 {
		t.Fatalf("result count = %d, want 8: %#v", len(results), results)
	}
}

func TestSearchAddsNoKeyKnowledgeSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/instant":
			_, _ = w.Write([]byte(`{"Heading":"","AbstractText":"","RelatedTopics":[]}`))
		case "/html":
			_, _ = w.Write([]byte(`<html><body></body></html>`))
		case "/wiki":
			_, _ = w.Write([]byte(`{"query":{"search":[{"title":"Artificial intelligence","snippet":"Machine intelligence"}]}}`))
		case "/openalex":
			_, _ = w.Write([]byte(`{"results":[{"title":"AI survey","doi":"https://doi.org/10.1/example","abstract":"Research overview"}]}`))
		case "/arxiv":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><feed><entry><title>AI paper</title><id>https://arxiv.org/abs/1</id><summary>Preprint summary</summary></entry></feed>`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient()
	client.httpClient = server.Client()
	client.duckInstantEndpoint = server.URL + "/instant"
	client.duckHTMLEndpoint = server.URL + "/html"
	client.wikipediaEndpoint = server.URL + "/wiki"
	client.openAlexEndpoint = server.URL + "/openalex"
	client.arxivEndpoint = server.URL + "/arxiv"

	results, err := client.Search(context.Background(), "artificial intelligence research papers")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("result count = %d, want 3: %#v", len(results), results)
	}
	if results[0].Title != "Wikipedia: Artificial intelligence" || results[1].Title != "OpenAlex: AI survey" || results[2].Title != "arXiv: AI paper" {
		t.Fatalf("results = %#v", results)
	}
}

func TestCleanDuckURLDoesNotDoubleDecode(t *testing.T) {
	raw := `//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fencoded%253Fq%3Da%2Bb`
	got := cleanDuckURL(raw)
	want := "https://example.com/encoded%3Fq=a+b"
	if got != want {
		t.Fatalf("cleanDuckURL() = %q, want %q", got, want)
	}
}

func TestProviderName(t *testing.T) {
	if got := ProviderName("", ""); got != "DuckDuckGo + Wikipedia" {
		t.Fatalf("ProviderName(empty) = %q", got)
	}
	if got := ProviderName("token", "http://127.0.0.1:8888"); got != "Brave + SearXNG + DuckDuckGo + Wikipedia" {
		t.Fatalf("ProviderName(token) = %q", got)
	}
}
