package llm

import (
	"context"
	"errors"
	"testing"

	"linea/backend/internal/store"
)

func TestFallbackClientUsesFallbackOnQuotaBeforeChunks(t *testing.T) {
	client := NewFallbackClient(
		fakeStreamer{err: ErrQuotaExceeded},
		fakeStreamer{chunks: []string{"local"}},
	)

	var got string
	err := client.GenerateStream(t.Context(), nil, nil, nil, func(chunk string) error {
		got += chunk
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if got != "local" {
		t.Fatalf("streamed content = %q, want local", got)
	}
}

func TestFallbackClientDoesNotFallbackAfterChunks(t *testing.T) {
	client := NewFallbackClient(
		fakeStreamer{chunks: []string{"partial"}, err: ErrQuotaExceeded},
		fakeStreamer{chunks: []string{"local"}},
	)

	var got string
	err := client.GenerateStream(t.Context(), nil, nil, nil, func(chunk string) error {
		got += chunk
		return nil
	})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("GenerateStream() error = %v, want ErrQuotaExceeded", err)
	}
	if got != "partial" {
		t.Fatalf("streamed content = %q, want partial", got)
	}
}

func TestFallbackClientUsesFallbackOnOrdinaryErrorBeforeChunks(t *testing.T) {
	ordinary := errors.New("ordinary")
	client := NewFallbackClient(
		fakeStreamer{err: ordinary},
		fakeStreamer{chunks: []string{"local"}},
	)

	var got string
	err := client.GenerateStream(t.Context(), nil, nil, nil, func(string) error {
		got += "local"
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if got != "local" {
		t.Fatalf("streamed content = %q, want local", got)
	}
}

func TestFallbackClientDoesNotFallbackForImageAttachments(t *testing.T) {
	ordinary := errors.New("ordinary")
	client := NewFallbackClient(
		fakeStreamer{err: ordinary},
		fakeStreamer{chunks: []string{"local"}},
	)

	var got string
	err := client.GenerateStream(
		t.Context(),
		nil,
		[]Attachment{{Name: "image.png", MIMEType: "image/png", Data: []byte("png")}},
		nil,
		func(chunk string) error {
			got += chunk
			return nil
		},
	)
	if !errors.Is(err, ordinary) {
		t.Fatalf("GenerateStream() error = %v, want ordinary", err)
	}
	if got != "" {
		t.Fatalf("streamed content = %q, want empty", got)
	}
}

type fakeStreamer struct {
	chunks []string
	err    error
}

func (f fakeStreamer) GenerateStream(_ context.Context, _ []store.Message, _ []Attachment, _ []SearchResult, onChunk func(string) error) error {
	for _, chunk := range f.chunks {
		if err := onChunk(chunk); err != nil {
			return err
		}
	}
	return f.err
}
