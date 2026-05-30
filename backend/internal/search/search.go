package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 8 * time.Second},
	}
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
	endpoint := "https://api.duckduckgo.com/?" + url.Values{
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
		return nil, errors.New("search provider returned an error")
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
