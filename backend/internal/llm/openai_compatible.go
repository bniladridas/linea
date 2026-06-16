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
	"strings"
	"time"

	"linea/backend/internal/store"
)

type OpenAICompatibleClient struct {
	name       string
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenAICompatibleClient(name, baseURL, apiKey, model string) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (c *OpenAICompatibleClient) GenerateStream(ctx context.Context, messages []store.Message, attachments []Attachment, searchResults []SearchResult, onChunk func(string) error) error {
	if HasImageAttachments(attachments) {
		return errors.New(c.name + " does not support image attachments in Linea")
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return fmt.Errorf("%s base URL is empty", c.name)
	}
	if strings.TrimSpace(c.model) == "" {
		return fmt.Errorf("%s model is empty", c.name)
	}

	body := openAIChatRequest{
		Model:       c.model,
		Messages:    buildOpenAIMessages(messages, attachments, searchResults),
		Stream:      true,
		Temperature: 0.2,
		TopP:        0.9,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
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
		if res.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("%w: %s returned %s: %s", ErrQuotaExceeded, c.name, res.Status, string(responseBody))
		}
		return fmt.Errorf("%s returned %s: %s", c.name, res.Status, string(responseBody))
	}
	NotifyProvider(ctx, ProviderInfo{Name: providerDisplayName(c.name), Model: c.model})

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
		chunk, err := openAIStreamContent(data)
		if err != nil {
			return err
		}
		if chunk == "" {
			continue
		}
		sawText = true
		if onChunk != nil {
			if err := onChunk(chunk); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !sawText {
		return errors.New(c.name + " returned no text")
	}
	return nil
}

func providerDisplayName(name string) string {
	switch strings.ToLower(name) {
	case "sambanova":
		return "SambaNova"
	case "cerebras":
		return "Cerebras"
	case "vllm":
		return "vLLM"
	case "mlx":
		return "MLX"
	case "openai-compatible":
		return "OpenAI Compatible"
	default:
		if name == "" {
			return "Provider"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

func buildOpenAIMessages(messages []store.Message, attachments []Attachment, searchResults []SearchResult) []openAIMessage {
	openAIMessages := []openAIMessage{{
		Role:    "system",
		Content: baseSystemPrompt(),
	}}
	if contextPrompt := buildContextPrompt(attachments, searchResults); contextPrompt != "" {
		openAIMessages = append(openAIMessages, openAIMessage{
			Role:    "system",
			Content: contextPrompt,
		})
	}
	for _, message := range messages {
		role := message.Role
		if role != "assistant" {
			role = "user"
		}
		openAIMessages = append(openAIMessages, openAIMessage{
			Role:    role,
			Content: message.Content,
		})
	}
	if hint := followUpTaskHint(messages); hint != "" {
		openAIMessages = append(openAIMessages, openAIMessage{
			Role:    "system",
			Content: hint,
		})
	}
	if len(messages) == 0 {
		openAIMessages = append(openAIMessages, openAIMessage{
			Role:    "user",
			Content: "Say hello briefly.",
		})
	}
	return openAIMessages
}

func openAIStreamContent(data string) (string, error) {
	var payload struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return "", err
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return "", errors.New(payload.Error.Message)
	}
	var b strings.Builder
	for _, choice := range payload.Choices {
		b.WriteString(choice.Delta.Content)
		b.WriteString(choice.Message.Content)
	}
	return b.String(), nil
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
