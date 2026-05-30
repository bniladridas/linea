package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"linea/backend/internal/store"
)

type OllamaClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpClient: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

func (c *OllamaClient) GenerateStream(ctx context.Context, messages []store.Message, attachments []Attachment, searchResults []SearchResult, onChunk func(string) error) error {
	if HasImageAttachments(attachments) {
		return errors.New("ollama does not support image attachments with the configured model")
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return errors.New("OLLAMA_BASE_URL is empty")
	}
	if strings.TrimSpace(c.model) == "" {
		return errors.New("OLLAMA_MODEL is empty")
	}

	body := ollamaChatRequest{
		Model:  c.model,
		Stream: true,
		Options: ollamaOptions{
			Temperature: 0.2,
			TopP:        0.9,
		},
		Messages: buildOllamaMessages(messages, attachments, searchResults),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(payload))
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
		responseBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if err != nil {
			return err
		}
		return fmt.Errorf("ollama returned %s: %s", res.Status, string(responseBody))
	}
	NotifyProvider(ctx, ProviderInfo{Name: "Ollama", Model: c.model})

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2<<20)
	var generated strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk ollamaChatResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return err
		}
		if chunk.Error != "" {
			return errors.New(chunk.Error)
		}
		if chunk.Message.Content != "" {
			generated.WriteString(chunk.Message.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	rawText := generated.String()
	if rawText == "" {
		return errors.New("ollama returned no text")
	}
	if onChunk != nil {
		if err := onChunk(cleanOllamaResponse(rawText, messages)); err != nil {
			return err
		}
	}
	return nil
}

func buildOllamaMessages(messages []store.Message, attachments []Attachment, searchResults []SearchResult) []ollamaMessage {
	ollamaMessages := []ollamaMessage{{
		Role:    "system",
		Content: baseSystemPrompt() + " You are currently running as Linea's local Ollama fallback, so be especially literal. If the latest user asks you to write, create, explain, summarize, fix, translate, list, or answer something, output the requested result immediately. Do not respond with only an acknowledgement or a sure/certainly/of course preface.",
	}}
	if contextPrompt := buildContextPrompt(attachments, searchResults); contextPrompt != "" {
		ollamaMessages = append(ollamaMessages, ollamaMessage{
			Role:    "system",
			Content: contextPrompt,
		})
	}
	for _, message := range messages {
		role := message.Role
		if role != "assistant" {
			role = "user"
		}
		ollamaMessages = append(ollamaMessages, ollamaMessage{
			Role:    role,
			Content: message.Content,
		})
	}
	if hint := followUpTaskHint(messages); hint != "" {
		ollamaMessages = append(ollamaMessages, ollamaMessage{
			Role:    "system",
			Content: hint,
		})
	}
	if len(messages) == 0 {
		ollamaMessages = append(ollamaMessages, ollamaMessage{
			Role:    "user",
			Content: "Say hello briefly.",
		})
	}
	return ollamaMessages
}

func followUpTaskHint(messages []store.Message) string {
	if len(messages) < 2 {
		return ""
	}
	latest := strings.ToLower(strings.TrimSpace(messages[len(messages)-1].Content))
	latest = strings.Trim(latest, ".!? ")
	if latest != "do it" && latest != "please do it" && latest != "do it please" && latest != "yes" && latest != "please" && latest != "continue" {
		return ""
	}
	for i := len(messages) - 2; i >= 0; i-- {
		message := messages[i]
		if message.Role != "user" {
			continue
		}
		request := strings.TrimSpace(message.Content)
		if request == "" {
			continue
		}
		return "The latest user message is a follow-up. Carry out this earlier user request now instead of asking for more instructions: " + request
	}
	return ""
}

func cleanOllamaResponse(text string, messages []store.Message) string {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return cleaned
	}
	if isWritingRequest(effectiveUserRequest(messages)) {
		if quoted := firstQuotedText(cleaned); quoted != "" {
			return quoted
		}
	}
	cleaned = stripAcknowledgementPrefix(cleaned)
	if cleaned == "" {
		return strings.TrimSpace(text)
	}
	return cleaned
}

func effectiveUserRequest(messages []store.Message) string {
	if len(messages) == 0 {
		return ""
	}
	latest := strings.ToLower(strings.Trim(strings.TrimSpace(messages[len(messages)-1].Content), ".!? "))
	if latest == "do it" || latest == "please do it" || latest == "do it please" || latest == "yes" || latest == "please" || latest == "continue" {
		for i := len(messages) - 2; i >= 0; i-- {
			if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
				return messages[i].Content
			}
		}
	}
	return messages[len(messages)-1].Content
}

func isWritingRequest(request string) bool {
	normalized := strings.ToLower(request)
	for _, marker := range []string{"write", "sentence", "rewrite", "translate", "name", "title", "caption"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func firstQuotedText(text string) string {
	match := regexp.MustCompile(`"([^"]+)"`).FindStringSubmatch(text)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func stripAcknowledgementPrefix(text string) string {
	cleaned := strings.TrimSpace(text)
	prefixes := []string{
		"certainly!", "certainly,", "certainly.",
		"sure!", "sure,", "sure.",
		"of course!", "of course,", "of course.",
		"okay!", "okay,", "okay.",
		"ok!", "ok,", "ok.",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(strings.ToLower(cleaned), prefix) {
			return strings.TrimSpace(cleaned[len(prefix):])
		}
	}
	return cleaned
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []ollamaMessage `json:"messages"`
	Options  ollamaOptions   `json:"options,omitempty"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"top_p"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
	Error   string        `json:"error"`
}
