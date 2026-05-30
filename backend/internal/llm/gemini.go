package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"linea/backend/internal/store"
)

var (
	ErrMissingAPIKey = errors.New("missing GEMINI_API_KEY")
	ErrQuotaExceeded = errors.New("llm quota exceeded")
)

type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (c *Client) GenerateStream(ctx context.Context, messages []store.Message, attachments []Attachment, searchResults []SearchResult, onChunk func(string) error) error {
	if c.apiKey == "" {
		return ErrMissingAPIKey
	}

	prompt := buildPrompt(messages, attachments, searchResults)
	parts := []part{{Text: prompt}}
	for _, attachment := range attachments {
		if !attachment.IsImage() {
			continue
		}
		parts = append(parts, part{
			InlineData: &inlineData{
				MIMEType: attachment.MIMEType,
				Data:     base64.StdEncoding.EncodeToString(attachment.Data),
			},
		})
	}
	body := generateRequest{
		Contents: []content{{
			Role:  "user",
			Parts: parts,
		}},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", url.PathEscape(c.model), url.QueryEscape(c.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		responseBody, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("%w: gemini returned %s: %s", ErrQuotaExceeded, res.Status, string(responseBody))
		}
		return fmt.Errorf("gemini returned %s: %s", res.Status, string(responseBody))
	}
	NotifyProvider(ctx, ProviderInfo{Name: "Gemini", Model: c.model})

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2<<20)
	sawText := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var generated generateResponse
		if err := json.Unmarshal([]byte(data), &generated); err != nil {
			return err
		}
		for _, candidate := range generated.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Text == "" {
					continue
				}
				sawText = true
				if onChunk != nil {
					if err := onChunk(part.Text); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !sawText {
		return errors.New("gemini returned no text")
	}
	return nil
}

type Attachment struct {
	Name     string
	Content  string
	MIMEType string
	Data     []byte
}

func (a Attachment) IsImage() bool {
	return strings.HasPrefix(a.MIMEType, "image/")
}

func HasImageAttachments(attachments []Attachment) bool {
	for _, attachment := range attachments {
		if attachment.IsImage() {
			return true
		}
	}
	return false
}

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

func buildPrompt(messages []store.Message, attachments []Attachment, searchResults []SearchResult) string {
	var b strings.Builder
	b.WriteString(baseSystemPrompt())
	b.WriteString("\n\n")
	if len(messages) > 0 {
		b.WriteString("Conversation:\n")
	}
	for _, message := range messages {
		b.WriteString(strings.ToUpper(message.Role))
		b.WriteString(": ")
		b.WriteString(message.Content)
		b.WriteString("\n")
	}
	if len(attachments) > 0 {
		b.WriteString("\nAttached files:\n")
		for _, attachment := range attachments {
			b.WriteString("\n--- ")
			b.WriteString(attachment.Name)
			b.WriteString(" ---\n")
			if attachment.IsImage() {
				b.WriteString("Image attachment. Read it directly when answering.")
			} else {
				b.WriteString(attachment.Content)
			}
			b.WriteString("\n")
		}
	}
	if len(searchResults) > 0 {
		b.WriteString("\nWeb search results:\n")
		for _, result := range searchResults {
			b.WriteString("- ")
			b.WriteString(result.Title)
			if result.URL != "" {
				b.WriteString(" (")
				b.WriteString(result.URL)
				b.WriteString(")")
			}
			if result.Snippet != "" {
				b.WriteString(": ")
				b.WriteString(result.Snippet)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func baseSystemPrompt() string {
	return strings.Join([]string{
		"You are Linea, a concise local-first AI assistant.",
		"Answer the latest user message directly and specifically.",
		"Your reply must complete the requested task, not merely acknowledge it.",
		"Never answer with only generic acknowledgements like hello, certainly, sure, okay, or I can help unless the user only asked for an acknowledgement.",
		"For writing, rewriting, translation, naming, or sentence-generation requests, output the requested content first without a sure/certainly/of course preface.",
		"If the latest user message is a short follow-up like do it, yes, please, or continue, infer the task from the recent conversation and carry it out.",
		"Do not introduce yourself.",
		"Do not give generic capability descriptions unless the user asks for them.",
		"Do not mention that you are an AI model unless it is directly relevant.",
		"If the user requests an exact format, obey it exactly.",
		"Use the conversation, attached files, and web search results only when relevant.",
		"If web results are present, cite useful URLs inline.",
	}, " ")
}

func buildContextPrompt(attachments []Attachment, searchResults []SearchResult) string {
	var b strings.Builder
	if len(attachments) > 0 {
		b.WriteString("Attached files:\n")
		for _, attachment := range attachments {
			b.WriteString("\n--- ")
			b.WriteString(attachment.Name)
			b.WriteString(" ---\n")
			if attachment.IsImage() {
				b.WriteString("Image attachment. This provider may not be able to read it.")
			} else {
				b.WriteString(attachment.Content)
			}
			b.WriteString("\n")
		}
	}
	if len(searchResults) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Web search results:\n")
		for _, result := range searchResults {
			b.WriteString("- ")
			b.WriteString(result.Title)
			if result.URL != "" {
				b.WriteString(" (")
				b.WriteString(result.URL)
				b.WriteString(")")
			}
			if result.Snippet != "" {
				b.WriteString(": ")
				b.WriteString(result.Snippet)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

type generateRequest struct {
	Contents []content `json:"contents"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inline_data,omitempty"`
}

type inlineData struct {
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
}

type generateResponse struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
}
