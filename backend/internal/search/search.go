package search

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Client struct {
	httpClient          *http.Client
	braveAPIKey         string
	searXNGURL          string
	braveEndpoint       string
	searXNGEndpoint     string
	duckInstantEndpoint string
	duckHTMLEndpoint    string
	wikipediaEndpoint   string
	openAlexEndpoint    string
	arxivEndpoint       string
}

type Option func(*Client)

func WithBraveAPIKey(key string) Option {
	return func(c *Client) {
		c.braveAPIKey = strings.TrimSpace(key)
	}
}

func WithSearXNGURL(value string) Option {
	return func(c *Client) {
		c.searXNGURL = strings.TrimRight(strings.TrimSpace(value), "/")
	}
}

func NewClient(options ...Option) *Client {
	client := &Client{
		httpClient:          &http.Client{Timeout: 8 * time.Second},
		braveEndpoint:       "https://api.search.brave.com/res/v1/web/search",
		duckInstantEndpoint: "https://api.duckduckgo.com/",
		duckHTMLEndpoint:    "https://html.duckduckgo.com/html/",
		wikipediaEndpoint:   "https://en.wikipedia.org/w/api.php",
		openAlexEndpoint:    "https://api.openalex.org/works",
		arxivEndpoint:       "https://export.arxiv.org/api/query",
	}
	for _, option := range options {
		option(client)
	}
	if client.searXNGURL != "" {
		client.searXNGEndpoint = client.searXNGURL + "/search"
	}
	return client
}

func ProviderName(braveAPIKey string, searXNGURL string) string {
	parts := []string{}
	if strings.TrimSpace(braveAPIKey) != "" {
		parts = append(parts, "Brave")
	}
	if strings.TrimSpace(searXNGURL) != "" {
		parts = append(parts, "SearXNG")
	}
	parts = append(parts, "DuckDuckGo", "Wikipedia")
	return strings.Join(parts, " + ")
}

func ShouldSearch(message string) bool {
	lower := strings.ToLower(message)
	triggers := []string{
		"search",
		"web",
		"online",
		"latest",
		"recent",
		"today",
		"current",
		"news",
		"price",
		"weather",
	}
	for _, trigger := range triggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

func QueryFromMessage(message string) string {
	query := strings.TrimSpace(message)
	lower := strings.ToLower(query)
	prefixes := []string{
		"can you search the web for ",
		"could you search the web for ",
		"please search the web for ",
		"search the web for ",
		"search online for ",
		"web search for ",
		"search for ",
		"search ",
		"look up ",
		"latest on ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			cleaned := strings.TrimSpace(query[len(prefix):])
			if cleaned != "" {
				return cleaned
			}
		}
	}
	return query
}

func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	results := []Result{}
	var searchErr error
	if c.braveAPIKey != "" {
		braveResults, err := c.searchBrave(ctx, query)
		if err != nil {
			searchErr = err
		}
		results = append(results, braveResults...)
	}
	if c.searXNGEndpoint != "" && len(uniqueResults(results)) < 8 {
		searXNGResults, err := c.searchSearXNG(ctx, query)
		if err != nil && searchErr == nil {
			searchErr = err
		}
		results = append(results, searXNGResults...)
	}
	if len(uniqueResults(results)) < 8 {
		instantResults, err := c.searchDuckInstant(ctx, query)
		if err != nil && searchErr == nil {
			searchErr = err
		}
		results = append(results, instantResults...)
	}
	if len(uniqueResults(results)) < 8 {
		htmlResults, err := c.searchDuckHTML(ctx, query)
		if err != nil && searchErr == nil {
			searchErr = err
		}
		results = append(results, htmlResults...)
	}
	if len(uniqueResults(results)) < 8 {
		wikipediaResults, err := c.searchWikipedia(ctx, query)
		if err != nil && searchErr == nil {
			searchErr = err
		}
		results = append(results, wikipediaResults...)
	}
	if len(uniqueResults(results)) < 8 && looksResearchQuery(query) {
		openAlexResults, err := c.searchOpenAlex(ctx, query)
		if err != nil && searchErr == nil {
			searchErr = err
		}
		results = append(results, openAlexResults...)
		arxivResults, err := c.searchArxiv(ctx, query)
		if err != nil && searchErr == nil {
			searchErr = err
		}
		results = append(results, arxivResults...)
	}
	results = limitResults(uniqueResults(results), 8)
	if len(results) == 0 && searchErr != nil {
		return nil, searchErr
	}
	return results, nil
}

func (c *Client) searchBrave(ctx context.Context, query string) ([]Result, error) {
	endpoint := c.braveEndpoint + "?" + url.Values{
		"q":     {query},
		"count": {"5"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", c.braveAPIKey)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("brave search returned an error")
	}
	var payload struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(payload.Web.Results))
	for _, item := range payload.Web.Results {
		if strings.TrimSpace(item.Title) == "" && strings.TrimSpace(item.Description) == "" {
			continue
		}
		results = append(results, Result{
			Title:   cleanHTML(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Snippet: cleanHTML(item.Description),
		})
	}
	return results, nil
}

func (c *Client) searchSearXNG(ctx context.Context, query string) ([]Result, error) {
	endpoint := c.searXNGEndpoint + "?" + url.Values{
		"q":      {query},
		"format": {"json"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("searxng search returned an error")
	}
	var payload struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(payload.Results))
	for _, item := range payload.Results {
		if strings.TrimSpace(item.Title) == "" && strings.TrimSpace(item.Content) == "" {
			continue
		}
		results = append(results, Result{
			Title:   cleanHTML(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Snippet: cleanHTML(item.Content),
		})
	}
	return results, nil
}

func (c *Client) searchDuckInstant(ctx context.Context, query string) ([]Result, error) {
	endpoint := c.duckInstantEndpoint + "?" + url.Values{
		"q":             {query},
		"format":        {"json"},
		"no_html":       {"1"},
		"skip_disambig": {"1"},
		"no_redirect":   {"1"},
		"t":             {"linea"},
		"pretty":        {"0"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("duckduckgo instant answer returned an error")
	}

	var payload struct {
		AbstractText string `json:"AbstractText"`
		AbstractURL  string `json:"AbstractURL"`
		Heading      string `json:"Heading"`
		Related      []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
			Result   string `json:"Result"`
			Icon     any    `json:"Icon"`
			Topics   []struct {
				Text     string `json:"Text"`
				FirstURL string `json:"FirstURL"`
			} `json:"Topics"`
		} `json:"RelatedTopics"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}

	results := make([]Result, 0, 5)
	if payload.AbstractText != "" {
		results = append(results, Result{
			Title:   firstNonEmpty(payload.Heading, "Search result"),
			URL:     payload.AbstractURL,
			Snippet: payload.AbstractText,
		})
	}
	for _, topic := range payload.Related {
		if len(results) >= 5 {
			break
		}
		if topic.Text != "" {
			results = append(results, Result{Title: firstSentence(topic.Text), URL: topic.FirstURL, Snippet: topic.Text})
			continue
		}
		for _, nested := range topic.Topics {
			if len(results) >= 5 {
				break
			}
			if nested.Text != "" {
				results = append(results, Result{Title: firstSentence(nested.Text), URL: nested.FirstURL, Snippet: nested.Text})
			}
		}
	}
	return results, nil
}

func (c *Client) searchDuckHTML(ctx context.Context, query string) ([]Result, error) {
	endpoint := c.duckHTMLEndpoint + "?" + url.Values{"q": {query}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Linea/1.0")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("duckduckgo html search returned an error")
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return parseDuckHTMLResults(string(body)), nil
}

func (c *Client) searchWikipedia(ctx context.Context, query string) ([]Result, error) {
	endpoint := c.wikipediaEndpoint + "?" + url.Values{
		"action":   {"query"},
		"list":     {"search"},
		"srsearch": {query},
		"srlimit":  {"3"},
		"format":   {"json"},
		"utf8":     {"1"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("wikipedia search returned an error")
	}
	var payload struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(payload.Query.Search))
	for _, item := range payload.Query.Search {
		title := cleanHTML(item.Title)
		if title == "" {
			continue
		}
		results = append(results, Result{
			Title:   "Wikipedia: " + title,
			URL:     "https://en.wikipedia.org/wiki/" + strings.ReplaceAll(url.PathEscape(title), "%20", "_"),
			Snippet: cleanHTML(item.Snippet),
		})
	}
	return results, nil
}

func (c *Client) searchOpenAlex(ctx context.Context, query string) ([]Result, error) {
	endpoint := c.openAlexEndpoint + "?" + url.Values{
		"search":   {query},
		"per-page": {"3"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Linea/1.0")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("openalex search returned an error")
	}
	var payload struct {
		Results []struct {
			Title       string           `json:"title"`
			DisplayName string           `json:"display_name"`
			DOI         string           `json:"doi"`
			ID          string           `json:"id"`
			Abstract    string           `json:"abstract"`
			AbstractMap map[string][]int `json:"abstract_inverted_index"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(payload.Results))
	for _, item := range payload.Results {
		title := firstNonEmpty(item.Title, item.DisplayName)
		if strings.TrimSpace(title) == "" {
			continue
		}
		results = append(results, Result{
			Title:   "OpenAlex: " + cleanHTML(title),
			URL:     strings.TrimSpace(firstNonEmpty(item.DOI, item.ID)),
			Snippet: cleanHTML(firstNonEmpty(item.Abstract, openAlexAbstract(item.AbstractMap))),
		})
	}
	return results, nil
}

func (c *Client) searchArxiv(ctx context.Context, query string) ([]Result, error) {
	endpoint := c.arxivEndpoint + "?" + url.Values{
		"search_query": {"all:" + query},
		"start":        {"0"},
		"max_results":  {"3"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("arxiv search returned an error")
	}
	var feed struct {
		Entries []struct {
			Title   string `xml:"title"`
			ID      string `xml:"id"`
			Summary string `xml:"summary"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(res.Body).Decode(&feed); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(feed.Entries))
	for _, item := range feed.Entries {
		title := cleanHTML(item.Title)
		if title == "" {
			continue
		}
		results = append(results, Result{
			Title:   "arXiv: " + title,
			URL:     strings.TrimSpace(item.ID),
			Snippet: cleanHTML(item.Summary),
		})
	}
	return results, nil
}

var duckResultPattern = regexp.MustCompile(`(?is)<a[^>]+class="[^"]*\bresult__a\b[^"]*"[^>]+href="([^"]+)"[^>]*>(.*?)</a>.*?<a[^>]+class="[^"]*\bresult__snippet\b[^"]*"[^>]*>(.*?)</a>`)

func parseDuckHTMLResults(body string) []Result {
	matches := duckResultPattern.FindAllStringSubmatch(body, 10)
	results := make([]Result, 0, len(matches))
	for _, match := range matches {
		title := cleanHTML(match[2])
		snippet := cleanHTML(match[3])
		resultURL := cleanDuckURL(match[1])
		if title == "" && snippet == "" {
			continue
		}
		results = append(results, Result{Title: title, URL: resultURL, Snippet: snippet})
	}
	return results
}

func cleanDuckURL(value string) string {
	value = htmlstd.UnescapeString(strings.TrimSpace(value))
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	if escaped := parsed.Query().Get("uddg"); escaped != "" {
		return escaped
	}
	return value
}

func cleanHTML(value string) string {
	value = htmlstd.UnescapeString(value)
	value = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(value, " ")
	value = strings.Join(strings.Fields(value), " ")
	value = regexp.MustCompile(`\s+([.,;:!?])`).ReplaceAllString(value, "$1")
	return value
}

func uniqueResults(results []Result) []Result {
	seen := map[string]bool{}
	out := make([]Result, 0, len(results))
	for _, result := range results {
		result.Title = strings.TrimSpace(result.Title)
		result.URL = strings.TrimSpace(result.URL)
		result.Snippet = strings.TrimSpace(result.Snippet)
		key := result.URL
		if key == "" {
			key = strings.ToLower(result.Title + "\n" + result.Snippet)
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, result)
	}
	return out
}

func limitResults(results []Result, limit int) []Result {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}

func looksResearchQuery(query string) bool {
	lower := strings.ToLower(query)
	triggers := []string{
		"arxiv",
		"citation",
		"doi",
		"journal",
		"literature",
		"openalex",
		"paper",
		"papers",
		"preprint",
		"research",
		"scholar",
		"study",
		"survey",
	}
	for _, trigger := range triggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

func openAlexAbstract(index map[string][]int) string {
	if len(index) == 0 {
		return ""
	}
	words := map[int]string{}
	maxPosition := -1
	for word, positions := range index {
		for _, position := range positions {
			words[position] = word
			if position > maxPosition {
				maxPosition = position
			}
		}
	}
	out := make([]string, 0, maxPosition+1)
	for position := 0; position <= maxPosition; position++ {
		if word := words[position]; word != "" {
			out = append(out, word)
		}
	}
	return strings.Join(out, " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstSentence(value string) string {
	if index := strings.Index(value, " - "); index > 0 {
		return value[:index]
	}
	if len(value) > 80 {
		return strings.TrimSpace(value[:77]) + "..."
	}
	return value
}
