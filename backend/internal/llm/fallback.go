package llm

import (
	"context"

	"linea/backend/internal/store"
)

type Streamer interface {
	GenerateStream(ctx context.Context, messages []store.Message, attachments []Attachment, searchResults []SearchResult, onChunk func(string) error) error
}

type FallbackClient struct {
	primary  Streamer
	fallback Streamer
}

func NewFallbackClient(primary, fallback Streamer) *FallbackClient {
	return &FallbackClient{primary: primary, fallback: fallback}
}

func (c *FallbackClient) GenerateStream(ctx context.Context, messages []store.Message, attachments []Attachment, searchResults []SearchResult, onChunk func(string) error) error {
	if c.primary == nil {
		return c.fallback.GenerateStream(ctx, messages, attachments, searchResults, onChunk)
	}
	emitted := false
	err := c.primary.GenerateStream(ctx, messages, attachments, searchResults, func(chunk string) error {
		emitted = true
		if onChunk == nil {
			return nil
		}
		return onChunk(chunk)
	})
	if err == nil {
		return nil
	}
	if c.fallback == nil || emitted || HasImageAttachments(attachments) {
		return err
	}
	return c.fallback.GenerateStream(ctx, messages, attachments, searchResults, onChunk)
}
