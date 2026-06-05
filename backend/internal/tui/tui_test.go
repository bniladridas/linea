package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"linea/backend/internal/llm"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
)

type fakeAssistant struct {
	response    string
	attachments []llm.Attachment
	search      []llm.SearchResult
	calls       int
	err         error
}

func (a *fakeAssistant) GenerateStream(_ context.Context, _ []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult, onChunk func(string) error) error {
	a.calls++
	a.attachments = append([]llm.Attachment(nil), attachments...)
	a.search = append([]llm.SearchResult(nil), searchResults...)
	if a.err != nil {
		return a.err
	}
	return onChunk(a.response)
}

type fakeSearcher struct {
	results []search.Result
	query   string
	err     error
}

func (s *fakeSearcher) Search(_ context.Context, query string) ([]search.Result, error) {
	s.query = query
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

func TestRunPersistsExchange(t *testing.T) {
	appStore := store.NewMemoryStore()
	var out strings.Builder
	app := New(appStore, &fakeAssistant{response: "hello"}, strings.NewReader("hi\n:quit\n"), &out)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 1 {
		t.Fatalf("conversations = %d, want 1", len(conversations))
	}
	messages, err := appStore.ListMessages(context.Background(), conversations[0].ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].Content != "hi" || messages[1].Content != "hello" {
		t.Fatalf("messages = %#v", messages)
	}
	if !strings.Contains(out.String(), "Linea\nhello") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunBetaUsesCustomTerminalPath(t *testing.T) {
	appStore := store.NewMemoryStore()
	var out strings.Builder
	app := New(appStore, &fakeAssistant{response: "hello"}, strings.NewReader("hi\n:quit\n"), &out)

	if err := app.RunBeta(context.Background()); err != nil {
		t.Fatalf("RunBeta() error = %v", err)
	}
	if !strings.Contains(out.String(), "Linea Terminal") || !strings.Contains(out.String(), "Message ›") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestThemeDisablesANSIForRedirectedFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "linea-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	output := newTheme(file).brand("Linea")
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("redirected output includes ANSI styling: %q", output)
	}
}

func TestRunAcceptsPromptLargerThanDefaultScannerLimit(t *testing.T) {
	appStore := store.NewMemoryStore()
	prompt := strings.Repeat("a", 70*1024)
	var out strings.Builder
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader(prompt+"\n:quit\n"), &out)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 1 {
		t.Fatalf("conversations = %d, want 1", len(conversations))
	}
	messages, err := appStore.ListMessages(context.Background(), conversations[0].ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].Content != prompt {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestRunNewStartsSeparateConversation(t *testing.T) {
	appStore := store.NewMemoryStore()
	var out strings.Builder
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader("one\n:new\ntwo\n:quit\n"), &out)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 2 {
		t.Fatalf("conversations = %d, want 2", len(conversations))
	}
}

func TestRunCanPickExistingConversation(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Existing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.AddMessage(context.Background(), conversation.ID, "user", "old"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	app := New(appStore, &fakeAssistant{response: "new"}, strings.NewReader("1\ncontinue\n:quit\n"), &out)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	messages, err := appStore.ListMessages(context.Background(), conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 3 || messages[1].Content != "continue" || messages[2].Content != "new" {
		t.Fatalf("messages = %#v", messages)
	}
	if !strings.Contains(out.String(), "Chat: Existing") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunForwardsSearchResults(t *testing.T) {
	assistant := &fakeAssistant{response: "ok"}
	searcher := &fakeSearcher{results: []search.Result{{Title: "Go", URL: "https://go.dev", Snippet: "Go language"}}}
	var out strings.Builder
	app := New(store.NewMemoryStore(), assistant, strings.NewReader("search the web for Go\n:quit\n"), &out).WithSearcher(searcher)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if searcher.query != "Go" {
		t.Fatalf("query = %q, want Go", searcher.query)
	}
	if len(assistant.search) != 1 || assistant.search[0].Title != "Go" {
		t.Fatalf("search results = %#v", assistant.search)
	}
}

func TestRunContinuesWhenSearchFails(t *testing.T) {
	assistant := &fakeAssistant{response: "ok"}
	searcher := &fakeSearcher{err: errors.New("network down")}
	var out strings.Builder
	app := New(store.NewMemoryStore(), assistant, strings.NewReader("latest Go news\n:quit\n"), &out).WithSearcher(searcher)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if assistant.calls != 1 {
		t.Fatalf("assistant calls = %d, want 1", assistant.calls)
	}
	if len(assistant.search) != 0 {
		t.Fatalf("search results = %#v, want none", assistant.search)
	}
	if !strings.Contains(out.String(), "Search unavailable: network down") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunForwardsAttachedFileToNextMessage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("file body"), 0o600); err != nil {
		t.Fatal(err)
	}
	assistant := &fakeAssistant{response: "ok"}
	var out strings.Builder
	app := New(store.NewMemoryStore(), assistant, strings.NewReader(":attach note.txt\nread it\n:quit\n"), &out).WithAttachmentBase(dir)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(assistant.attachments) != 1 || assistant.attachments[0].Name != "note.txt" || assistant.attachments[0].Content != "file body" {
		t.Fatalf("attachments = %#v", assistant.attachments)
	}
	if !strings.Contains(out.String(), "Queued attachments") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunTreatsDroppedPathAsAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drop.txt")
	if err := os.WriteFile(path, []byte("dropped body"), 0o600); err != nil {
		t.Fatal(err)
	}
	assistant := &fakeAssistant{response: "ok"}
	var out strings.Builder
	app := New(store.NewMemoryStore(), assistant, strings.NewReader(path+"\nread it\n:quit\n"), &out)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(assistant.attachments) != 1 || assistant.attachments[0].Name != "drop.txt" || assistant.attachments[0].Content != "dropped body" {
		t.Fatalf("attachments = %#v", assistant.attachments)
	}
}

func TestRunRejectsLargeAttachment(t *testing.T) {
	dir := t.TempDir()
	large := make([]byte, maxTextAttachmentBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), large, 0o600); err != nil {
		t.Fatal(err)
	}
	assistant := &fakeAssistant{response: "ok"}
	var out strings.Builder
	app := New(store.NewMemoryStore(), assistant, strings.NewReader(":attach large.txt\nread it\n:quit\n"), &out).WithAttachmentBase(dir)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if assistant.calls != 1 {
		t.Fatalf("assistant calls = %d, want 1", assistant.calls)
	}
	if len(assistant.attachments) != 0 {
		t.Fatalf("attachments = %#v, want none", assistant.attachments)
	}
	if !strings.Contains(out.String(), errAttachmentTooLarge.Error()) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunRendersMarkdownAndFencedCode(t *testing.T) {
	response := strings.Join([]string{
		"# Result",
		"",
		"- Use `linea`",
		"",
		"```html",
		"<button id=\"ok\">OK</button>",
		"```",
	}, "\n")
	var out strings.Builder
	app := New(store.NewMemoryStore(), &fakeAssistant{response: response}, strings.NewReader("show markdown\n:quit\n"), &out)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"Result",
		"• Use `linea`",
		"┌",
		"html",
		"│ <button id=\"ok\">OK</button>",
		"└",
		"Message ›",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestBubbleModelSendsMessage(t *testing.T) {
	assistant := &fakeAssistant{response: "hello"}
	app := New(store.NewMemoryStore(), assistant, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.input.SetValue("hi")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd == nil {
		t.Fatal("expected send command")
	}
	msg := cmd()
	updated, _ = model.Update(msg)
	model = updated.(bubbleModel)
	if len(model.messages) != 2 || model.messages[0].Content != "hi" || model.messages[1].Content != "hello" {
		t.Fatalf("messages = %#v", model.messages)
	}
	view := model.View()
	if !strings.Contains(view, "Linea") || !strings.Contains(view, "hello") {
		t.Fatalf("view = %q", view)
	}
}

func TestBubbleModelIgnoresMessageSubmitWhileSending(t *testing.T) {
	assistant := &fakeAssistant{response: "hello"}
	app := New(store.NewMemoryStore(), assistant, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.sending = true
	model.input.SetValue("second")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil {
		t.Fatal("expected no send command while response is pending")
	}
	if assistant.calls != 0 {
		t.Fatalf("assistant calls = %d, want 0", assistant.calls)
	}
	if model.status != "Wait for the current response to finish." {
		t.Fatalf("status = %q", model.status)
	}
}

func TestBubbleModelKeepsUserMessageVisibleAfterGenerationError(t *testing.T) {
	appStore := store.NewMemoryStore()
	assistant := &fakeAssistant{err: errors.New("provider unavailable")}
	app := New(appStore, assistant, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.input.SetValue("hi")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd == nil {
		t.Fatal("expected send command")
	}
	msg := cmd()
	updated, _ = model.Update(msg)
	model = updated.(bubbleModel)
	if model.conversation.ID == "" {
		t.Fatal("conversation was not kept visible after error")
	}
	if len(model.messages) != 1 || model.messages[0].Content != "hi" {
		t.Fatalf("messages = %#v", model.messages)
	}
	if model.status != "provider unavailable" {
		t.Fatalf("status = %q", model.status)
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 1 || conversations[0].ID != model.conversation.ID {
		t.Fatalf("conversations = %#v", conversations)
	}
}

func TestBubblePickerHonorsQuitCommand(t *testing.T) {
	appStore := store.NewMemoryStore()
	if _, err := appStore.CreateConversation(context.Background(), "Existing"); err != nil {
		t.Fatal(err)
	}
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	if model.mode != modePick {
		t.Fatalf("mode = %v, want picker", model.mode)
	}
	model.input.SetValue(":quit")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	model = updated.(bubbleModel)
	if model.mode != modePick {
		t.Fatalf("mode = %v, want picker to stay unchanged before quit", model.mode)
	}
}

func TestBubbleModelResizesViewportAndComposer(t *testing.T) {
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "ok"}, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(bubbleModel)
	if model.viewport.Width <= 0 || model.viewport.Height <= 0 {
		t.Fatalf("viewport size = %dx%d", model.viewport.Width, model.viewport.Height)
	}
	if model.input.Width <= 0 || model.input.Width >= model.contentWidth() {
		t.Fatalf("input width = %d content = %d", model.input.Width, model.contentWidth())
	}
}

func TestBubbleMarkdownRendersInlineMarkers(t *testing.T) {
	styles := (bubbleModel{}).styles()
	output := renderMarkdownForBubble("- **HTML:** use `<h1>`", styles)
	if strings.Contains(output, "**HTML:**") || strings.Contains(output, "`<h1>`") {
		t.Fatalf("inline markdown was not rendered: %q", output)
	}
	if !strings.Contains(output, "HTML:") || !strings.Contains(output, "<h1>") {
		t.Fatalf("output = %q", output)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("output did not include syntax color: %q", output)
	}
}

func TestSmokeCoversPickerNewSearchAndAttachments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("file body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), make([]byte, maxTextAttachmentBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Existing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.AddMessage(context.Background(), conversation.ID, "user", "old"); err != nil {
		t.Fatal(err)
	}
	assistant := &fakeAssistant{response: "ok"}
	searcher := &fakeSearcher{err: errors.New("offline")}
	input := strings.Join([]string{
		"1",
		"continue existing",
		":new",
		":attach large.txt",
		":attach note.txt",
		"latest Go news",
		":quit",
		"",
	}, "\n")
	var out strings.Builder
	app := New(appStore, assistant, strings.NewReader(input), &out).WithSearcher(searcher).WithAttachmentBase(dir)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if assistant.calls != 2 {
		t.Fatalf("assistant calls = %d, want 2", assistant.calls)
	}
	if len(assistant.attachments) != 1 || assistant.attachments[0].Name != "note.txt" {
		t.Fatalf("attachments = %#v", assistant.attachments)
	}
	output := out.String()
	for _, want := range []string{
		"Linea Terminal",
		"Recent",
		"Chat: Existing",
		"Transcript",
		"Started new chat.",
		errAttachmentTooLarge.Error(),
		"Queued attachments",
		"Attached note.txt.",
		"Search unavailable: offline",
		"Commands: :new",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 2 {
		t.Fatalf("conversations = %d, want 2", len(conversations))
	}
}
