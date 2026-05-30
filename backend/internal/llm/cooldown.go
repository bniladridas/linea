package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"linea/backend/internal/store"
)

type CooldownClient struct {
	name     string
	streamer Streamer
	cooldown time.Duration
	now      func() time.Time

	mu       sync.Mutex
	nextTry  time.Time
	lastFail string
}

func NewCooldownClient(name string, streamer Streamer, cooldown time.Duration) *CooldownClient {
	return &CooldownClient{
		name:     name,
		streamer: streamer,
		cooldown: cooldown,
		now:      time.Now,
	}
}

func (c *CooldownClient) GenerateStream(ctx context.Context, messages []store.Message, attachments []Attachment, searchResults []SearchResult, onChunk func(string) error) error {
	if c.streamer == nil {
		return fmt.Errorf("%s provider is not configured", c.name)
	}
	if wait := c.wait(); wait > 0 {
		return fmt.Errorf("%s provider cooling down for %s after %s", c.name, wait.Round(time.Second), c.lastError())
	}
	err := c.streamer.GenerateStream(ctx, messages, attachments, searchResults, onChunk)
	if err != nil {
		c.recordFailure(err)
		return err
	}
	c.clearFailure()
	return nil
}

func (c *CooldownClient) wait() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	wait := time.Until(c.nextTry)
	if c.now != nil {
		wait = c.nextTry.Sub(c.now())
	}
	if wait < 0 {
		return 0
	}
	return wait
}

func (c *CooldownClient) recordFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cooldown <= 0 {
		return
	}
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	c.nextTry = now.Add(c.cooldown)
	c.lastFail = redactProviderError(err.Error())
}

func (c *CooldownClient) clearFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextTry = time.Time{}
	c.lastFail = ""
}

func (c *CooldownClient) lastError() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.lastFail == "" {
		return "a previous error"
	}
	return c.lastFail
}

func redactProviderError(message string) string {
	if len(message) > 120 {
		return message[:117] + "..."
	}
	return message
}
