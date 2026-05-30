package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"linea/backend/internal/store"
)

func TestCooldownClientSkipsProviderAfterFailure(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	streamer := fakeStreamer{err: errors.New("payment required")}
	client := NewCooldownClient("test", streamer, time.Minute)
	client.now = func() time.Time { return now }

	err := client.GenerateStream(t.Context(), nil, nil, nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("GenerateStream() error = nil, want failure")
	}

	err = client.GenerateStream(t.Context(), nil, nil, nil, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("GenerateStream() error = %v, want cooldown", err)
	}
}

func TestCooldownClientRetriesAfterWindow(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	streamer := &countingStreamer{err: errors.New("temporary")}
	client := NewCooldownClient("test", streamer, time.Minute)
	client.now = func() time.Time { return now }

	_ = client.GenerateStream(t.Context(), nil, nil, nil, func(string) error { return nil })
	now = now.Add(2 * time.Minute)
	_ = client.GenerateStream(t.Context(), nil, nil, nil, func(string) error { return nil })

	if streamer.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", streamer.calls)
	}
}

type countingStreamer struct {
	calls int
	err   error
}

func (s *countingStreamer) GenerateStream(_ context.Context, _ []store.Message, _ []Attachment, _ []SearchResult, _ func(string) error) error {
	s.calls++
	return s.err
}
