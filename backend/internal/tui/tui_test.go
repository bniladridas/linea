package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"linea/backend/internal/agent"
	"linea/backend/internal/llm"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
)

func TestMain(m *testing.M) {
	if os.Getenv("LINEA_FAKE_TUI_MCP_SERVER") == "1" {
		runFakeTUIMCPServer()
		return
	}
	os.Exit(m.Run())
}

func runFakeTUIMCPServer() {
	reader := bufio.NewReader(os.Stdin)
	for {
		message, err := readTUITestMCPMessage(reader)
		if err != nil {
			return
		}
		id := intTUITestMCPID(message["id"])
		method, _ := message["method"].(string)
		switch method {
		case "initialize":
			_ = writeTUITestMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
				},
			})
		case "tools/list":
			_ = writeTUITestMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "ping",
						"description": "Ping",
						"inputSchema": map[string]any{"type": "object"},
					}},
				},
			})
		case "resources/list":
			_ = writeTUITestMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"resources": []map[string]any{{
						"uri":         "docs://readme",
						"name":        "README",
						"description": "Project README",
						"mimeType":    "text/markdown",
					}},
				},
			})
		case "resources/read":
			params, _ := message["params"].(map[string]any)
			uri, _ := params["uri"].(string)
			_ = writeTUITestMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"contents": []map[string]any{{
						"uri":      uri,
						"mimeType": "text/markdown",
						"text":     "# README\n",
					}},
				},
			})
		case "resources/subscribe":
			params, _ := message["params"].(map[string]any)
			uri, _ := params["uri"].(string)
			_ = writeTUITestMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{},
			})
			_ = writeTUITestMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"method":  "notifications/resources/updated",
				"params":  map[string]any{"uri": uri},
			})
		case "resources/unsubscribe":
			_ = writeTUITestMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{},
			})
			return
		case "prompts/list":
			_ = writeTUITestMCPMessage(os.Stdout, map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"prompts": []map[string]any{{"name": "review", "description": "Review code"}},
				},
			})
		}
	}
}

func readTUITestMCPMessage(reader *bufio.Reader) (map[string]any, error) {
	var length int
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		const prefix = "Content-Length:"
		if strings.HasPrefix(line, prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			length = parsed
		}
	}
	if length <= 0 {
		return nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	var message map[string]any
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, err
	}
	return message, nil
}

func writeTUITestMCPMessage(writer io.Writer, message map[string]any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

func intTUITestMCPID(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

type fakeAssistant struct {
	response    string
	messages    []store.Message
	attachments []llm.Attachment
	search      []llm.SearchResult
	calls       int
	err         error
}

func (a *fakeAssistant) GenerateStream(_ context.Context, messages []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult, onChunk func(string) error) error {
	a.calls++
	a.messages = append([]store.Message(nil), messages...)
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

type fakeTUIEditPlanner struct {
	plan agent.EditPlan
	err  error
}

func (p *fakeTUIEditPlanner) PlanEdit(context.Context, agent.EditPlanRequest) (agent.EditPlan, error) {
	return p.plan, p.err
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

func TestRunCanRenameShareAndDeleteCurrentConversation(t *testing.T) {
	appStore := store.NewMemoryStore()
	var out strings.Builder
	input := strings.Join([]string{
		"hello",
		":rename Better title",
		":share",
		":delete",
		":delete confirm",
		":quit",
		"",
	}, "\n")
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader(input), &out)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := out.String()
	for _, want := range []string{"Renamed.", "Better title", "User: hello", "Linea: ok", "Use :delete confirm", "Deleted chat."} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("conversations = %#v", conversations)
	}
}

func TestRunShareDoesNotEnterModelContext(t *testing.T) {
	appStore := store.NewMemoryStore()
	assistant := &fakeAssistant{response: "ok"}
	var out strings.Builder
	input := strings.Join([]string{
		"hello",
		":share",
		"again",
		":quit",
		"",
	}, "\n")
	app := New(appStore, assistant, strings.NewReader(input), &out)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if assistant.calls != 2 {
		t.Fatalf("calls = %d, want 2", assistant.calls)
	}
	for _, message := range assistant.messages {
		if strings.Contains(message.Content, "User: hello") {
			t.Fatalf("share text entered model context: %#v", assistant.messages)
		}
	}
	if !strings.Contains(out.String(), "User: hello") {
		t.Fatalf("share text not rendered: %q", out.String())
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

func TestRunHandlesAgentCommands(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("agent notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc Run() {}\nfunc main() { Run() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(dir), agent.WithCommandAllowlist([]string{"printf ok"}))
	var out strings.Builder
	input := strings.Join([]string{
		":help",
		":agent",
		":symbols run",
		":refs Run",
		":mcp",
		":subagent search agent",
		":subagent plan review docs and search agent",
		":subagent plans",
		":search agent",
		":read notes.md",
		":loop search agent",
		":check printf ok",
		":approve printf ok",
		":run printf ok",
		":quit",
		"",
	}, "\n")
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(input), &out).WithAgentRuntime(runtime)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"Agent ready",
		":new",
		":attach <path>",
		":symbols [query]",
		":refs <identifier>",
		":proposal approve <id>",
		":proposal reject <id>",
		":proposal apply <id>",
		":quit",
		"func Run",
		"main.go:4 func main() { Run() }",
		"No MCP entries",
		"Subagent search: completed",
		"Subagent plan",
		"review completed",
		"notes.md:1 agent notes",
		"notes.md ·",
		"search agent",
		"printf ok · allowed",
		"Approved printf ok.",
		"printf ok · exit 0",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestMCPResourceReadInputAcceptsDisplayedURI(t *testing.T) {
	input := mcpResourceReadInput("docs://readme")
	if input.URI != "docs://readme" || input.ResourceID != "" {
		t.Fatalf("input = %#v", input)
	}

	input = mcpResourceReadInput("memory:foo")
	if input.URI != "memory:foo" || input.ResourceID != "" {
		t.Fatalf("input = %#v", input)
	}

	input = mcpResourceReadInput("docs/docs_readme")
	if input.ResourceID != "docs/docs_readme" || input.URI != "" {
		t.Fatalf("input = %#v", input)
	}
}

func TestRunMCPDiscoversToolsResourcesAndPrompts(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_TUI_MCP_SERVER":"1"}
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := agent.NewRuntime("", agent.WithMCPConfigPath(configPath))
	var out strings.Builder
	input := strings.Join([]string{":mcp", ":quit", ""}, "\n")
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(input), &out).WithAgentRuntime(runtime)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"Server docs · ready",
		"Tool docs/ping",
		"Resource docs/README · docs://readme",
		"Prompt docs/review",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
}

func TestRunMCPSubscribesToResource(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_TUI_MCP_SERVER":"1"},
      "resources": [{"uri":"docs://readme","name":"README"}]
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := agent.NewRuntime("", agent.WithMCPConfigPath(configPath))
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(""), &strings.Builder{}).WithAgentRuntime(runtime)

	output, err := app.runAgentCommand(context.Background(), ":mcp subscribe docs://readme")
	if err != nil {
		t.Fatalf("runAgentCommand() error = %v", err)
	}
	if !strings.Contains(output, "MCP subscription") || !strings.Contains(output, "active") {
		t.Fatalf("output = %q", output)
	}
}

func TestRunMCPListsCallsSubscriptionsAndEvents(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "docs": {
      "command": "`+os.Args[0]+`",
      "env": {"LINEA_FAKE_TUI_MCP_SERVER":"1"},
      "resources": [{"uri":"docs://readme","name":"README"}]
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := agent.NewRuntime("", agent.WithMCPConfigPath(configPath))
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(""), &strings.Builder{}).WithAgentRuntime(runtime)

	if _, err := app.runAgentCommand(context.Background(), ":mcp read docs://readme"); err != nil {
		t.Fatalf("read command error = %v", err)
	}
	if _, err := app.runAgentCommand(context.Background(), ":mcp subscribe docs://readme"); err != nil {
		t.Fatalf("subscribe command error = %v", err)
	}
	waitForTUIMCPEvent(t, runtime, "docs://readme")

	calls, err := app.runAgentCommand(context.Background(), ":mcp calls")
	if err != nil {
		t.Fatalf("calls command error = %v", err)
	}
	if !strings.Contains(calls, "resource:docs://readme") || !strings.Contains(calls, "completed") {
		t.Fatalf("calls = %q", calls)
	}

	subscriptions, err := app.runAgentCommand(context.Background(), ":mcp subscriptions")
	if err != nil {
		t.Fatalf("subscriptions command error = %v", err)
	}
	if !strings.Contains(subscriptions, "active") || !strings.Contains(subscriptions, "docs://readme") {
		t.Fatalf("subscriptions = %q", subscriptions)
	}

	events, err := app.runAgentCommand(context.Background(), ":mcp events")
	if err != nil {
		t.Fatalf("events command error = %v", err)
	}
	if !strings.Contains(events, "notifications/resources/updated") || !strings.Contains(events, "docs://readme") {
		t.Fatalf("events = %q", events)
	}
}

func waitForTUIMCPEvent(t *testing.T, runtime *agent.Runtime, uri string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range runtime.ListMCPEvents(context.Background()) {
			if event.URI == uri {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for MCP event %s", uri)
}

func TestRunSkillUsesDefaultCommandApproval(t *testing.T) {
	workspace := t.TempDir()
	skillsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillsDir, "review-change.md"), []byte("# Review change\n\nCommand: printf ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := agent.NewRuntime(
		"",
		agent.WithSkillsDir(skillsDir),
		agent.WithWorkspaceRoot(workspace),
		agent.WithCommandAllowlist([]string{"printf ok"}),
	)
	var out strings.Builder
	input := strings.Join([]string{
		":approve printf ok",
		":skill review_change",
		":quit",
		"",
	}, "\n")
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(input), &out).WithAgentRuntime(runtime)

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"Approved printf ok.",
		"Skill review_change: completed",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %q", want, output)
		}
	}
	runs := runtime.ListCommandRuns(context.Background())
	if len(runs) != 1 || runs[0].Command != "printf ok" || runs[0].ExitCode != 0 {
		t.Fatalf("command runs = %#v", runs)
	}
}

func TestRunAgentCommandControlsLoops(t *testing.T) {
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(t.TempDir()), agent.WithCommandAllowlist([]string{"printf ok"}))
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(""), &strings.Builder{}).WithAgentRuntime(runtime)
	loop, err := runtime.StartAgentLoop(context.Background(), agent.AgentLoopInput{Goal: "run command", Command: "printf ok"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if _, err := runtime.AddCommandApproval(context.Background(), agent.CommandApprovalInput{Command: "printf ok", State: "approved"}); err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}

	output, err := app.runAgentCommand(context.Background(), ":loop continue "+loop.ID)
	if err != nil {
		t.Fatalf("runAgentCommand() error = %v", err)
	}
	if !strings.Contains(output, "completed") || !strings.Contains(output, "Run command") {
		t.Fatalf("output = %q", output)
	}

	editLoop, err := runtime.StartAgentLoop(context.Background(), agent.AgentLoopInput{Goal: "edit file"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	output, err = app.runAgentCommand(context.Background(), ":loop cancel "+editLoop.ID)
	if err != nil {
		t.Fatalf("runAgentCommand() error = %v", err)
	}
	if !strings.Contains(output, "canceled") {
		t.Fatalf("output = %q", output)
	}
}

func TestRunAgentCommandStartsAutoLoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tprintf ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root), agent.WithCommandAllowlist([]string{"make test"}))
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(""), &strings.Builder{}).WithAgentRuntime(runtime)

	output, err := app.runAgentCommand(context.Background(), ":loop auto fix and test")
	if err != nil {
		t.Fatalf("runAgentCommand() error = %v", err)
	}
	if !strings.Contains(output, "Auto loop completed") || !strings.Contains(output, "Run command") || !strings.Contains(output, "make test") {
		t.Fatalf("output = %q", output)
	}
}

func TestRunAgentCommandStartsDeveloperLoop(t *testing.T) {
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(t.TempDir()))
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(""), &strings.Builder{}).WithAgentRuntime(runtime)

	output, err := app.runAgentCommand(context.Background(), ":loop developer inspect workspace")
	if err != nil {
		t.Fatalf("runAgentCommand() error = %v", err)
	}
	if !strings.Contains(output, "Developer loop completed") {
		t.Fatalf("output = %q", output)
	}
	loops := runtime.ListAgentLoops(context.Background())
	if len(loops) != 1 || loops[0].Mode != "developer" || !loops[0].AutoApply {
		t.Fatalf("loops = %#v", loops)
	}
}

func TestRunAgentCommandStartsAutoApplyLoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tprintf ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.go"), []byte("package main\nfunc broken( {\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	planner := &fakeTUIEditPlanner{
		plan: agent.EditPlan{
			Path:    "broken.go",
			Content: "package main\nfunc fixed() {}\n",
			Summary: "Fix parse error",
		},
	}
	runtime := agent.NewRuntime(
		"",
		agent.WithWorkspaceRoot(root),
		agent.WithCommandAllowlist([]string{"make test"}),
		agent.WithEditPlanner(planner),
	)
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(""), &strings.Builder{}).WithAgentRuntime(runtime)

	output, err := app.runAgentCommand(context.Background(), ":loop auto fix diagnostics and run tests")
	if err != nil {
		t.Fatalf("runAgentCommand() error = %v", err)
	}
	if !strings.Contains(output, "Auto loop completed") || !strings.Contains(output, "Apply edit proposal") || !strings.Contains(output, "make test") {
		t.Fatalf("output = %q", output)
	}
	loops := runtime.ListAgentLoops(context.Background())
	if len(loops) != 1 || !loops[0].AutoApply || loops[0].State != "completed" {
		t.Fatalf("loops = %#v", loops)
	}
}

func TestRunAgentCommandStopsAutoLoopAtProposalReview(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(""), &strings.Builder{}).WithAgentRuntime(runtime)
	loop, err := runtime.StartAgentLoop(context.Background(), agent.AgentLoopInput{
		Goal:            "edit note",
		Mode:            "auto",
		MaxIterations:   1,
		ProposalPath:    "note.txt",
		ProposalContent: "new\n",
	})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	if loop.State != "waiting_approval" || !tuiLoopHasStep(loop, "edit_review", "waiting_approval") {
		t.Fatalf("loop = %#v", loop)
	}

	output, err := app.runAgentCommand(context.Background(), ":loop continue "+loop.ID)
	if err != nil {
		t.Fatalf("runAgentCommand() error = %v", err)
	}
	loops := runtime.ListAgentLoops(context.Background())
	if len(loops) != 1 || loops[0].State != "waiting_approval" || !strings.Contains(output, "Review edit proposal") {
		t.Fatalf("loops = %#v\noutput = %q", loops, output)
	}
}

func tuiLoopHasStep(loop agent.AgentLoop, kind string, state string) bool {
	for _, step := range loop.Steps {
		if step.Kind == kind && step.State == state {
			return true
		}
	}
	return false
}

func TestFormatAgentLoopIgnoresStaleAutoLimit(t *testing.T) {
	output := formatAgentLoop(agent.AgentLoop{
		Goal:    "fix",
		Mode:    "auto",
		State:   "completed",
		Summary: "Done.",
		Steps: []agent.AgentLoopStep{
			{Kind: "auto_limit", Title: "Auto limit", State: "waiting_input", Detail: "Reached 1 iteration(s)."},
			{Kind: "review_result", Title: "Review result", State: "completed", Detail: "Command completed successfully."},
		},
	})
	if strings.Contains(output, "Next · continue explicitly") {
		t.Fatalf("output = %q", output)
	}
}

func TestRunAgentCommandAppliesProposalAndContinuesAutoLoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tprintf ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.go"), []byte("package main\nfunc broken( {\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	planner := &fakeTUIEditPlanner{
		plan: agent.EditPlan{
			Path:    "broken.go",
			Content: "package main\nfunc fixed() {}\n",
			Summary: "Fix parse error",
		},
	}
	runtime := agent.NewRuntime(
		"",
		agent.WithWorkspaceRoot(root),
		agent.WithCommandAllowlist([]string{"make test"}),
		agent.WithEditPlanner(planner),
	)
	loop, err := runtime.StartAgentLoop(context.Background(), agent.AgentLoopInput{Goal: "fix diagnostics and run tests", Mode: "auto"})
	if err != nil {
		t.Fatalf("StartAgentLoop() error = %v", err)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 {
		t.Fatalf("proposals = %#v", proposals)
	}
	if proposals[0].Status != "pending" {
		t.Fatalf("proposal status = %q", proposals[0].Status)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposals[0].ID, agent.EditProposalReviewInput{Status: "approved"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	if _, err := runtime.ApplyEditProposal(context.Background(), proposals[0].ID); err != nil {
		t.Fatalf("ApplyEditProposal() error = %v", err)
	}
	loop, err = runtime.ContinueAgentLoop(context.Background(), loop.ID, agent.AgentLoopContinueInput{})
	if err != nil {
		t.Fatalf("ContinueAgentLoop() error = %v", err)
	}
	output := formatAgentLoop(loop)
	if !strings.Contains(output, "Auto loop completed") || !strings.Contains(output, "Run command") || !strings.Contains(output, "make test") {
		t.Fatalf("output = %q", output)
	}
	loops := runtime.ListAgentLoops(context.Background())
	if len(loops) != 1 || loops[0].ID != loop.ID || loops[0].State != "completed" {
		t.Fatalf("loops = %#v", loops)
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

func TestBubbleModelHandlesAgentCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("agent notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(dir))
	app := New(store.NewMemoryStore(), &fakeAssistant{response: "unused"}, strings.NewReader(""), &strings.Builder{}).WithAgentRuntime(runtime)
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.input.SetValue(":search agent")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil {
		t.Fatal("agent command should not start async model send")
	}
	if len(model.messages) != 1 || !strings.Contains(model.messages[0].Content, "notes.md:1 agent notes") {
		t.Fatalf("messages = %#v", model.messages)
	}
	if !strings.Contains(model.View(), "notes.md") {
		t.Fatalf("view = %q", model.View())
	}
}

func TestBubbleModelHandlesConversationControls(t *testing.T) {
	appStore := store.NewMemoryStore()
	assistant := &fakeAssistant{response: "hello"}
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
	updated, _ = model.Update(cmd())
	model = updated.(bubbleModel)

	model.input.SetValue(":rename Better title")
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil || model.conversation.Title != "Better title" || model.status != "Renamed." {
		t.Fatalf("conversation=%#v status=%q cmd=%v", model.conversation, model.status, cmd)
	}
	model.input.SetValue(":share")
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil || len(model.messages) != 2 || !strings.Contains(model.shareText, "User: hi") {
		t.Fatalf("messages=%#v share=%q status=%q cmd=%v", model.messages, model.shareText, model.status, cmd)
	}
	model.input.SetValue("again")
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd == nil || model.shareText != "" {
		t.Fatalf("share=%q cmd=%v", model.shareText, cmd)
	}
	updated, _ = model.Update(cmd())
	model = updated.(bubbleModel)
	for _, message := range assistant.messages {
		if strings.Contains(message.Content, "User: hi") {
			t.Fatalf("share text entered model context: %#v", assistant.messages)
		}
	}
	model.input.SetValue(":delete")
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil || !strings.Contains(model.status, ":delete confirm") {
		t.Fatalf("status=%q cmd=%v", model.status, cmd)
	}
	model.input.SetValue(":delete confirm")
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil || model.conversation.ID != "" || len(model.messages) != 0 || model.status != "Deleted chat." {
		t.Fatalf("conversation=%#v messages=%#v status=%q cmd=%v", model.conversation, model.messages, model.status, cmd)
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("conversations = %#v", conversations)
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
	if model.status != "Wait for current response." {
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

func TestBubbleEscapeBacksOutBeforeQuit(t *testing.T) {
	appStore := store.NewMemoryStore()
	if _, err := appStore.CreateConversation(context.Background(), "Existing"); err != nil {
		t.Fatal(err)
	}
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.input.SetValue("1")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected no command while opening chat")
	}
	model = updated.(bubbleModel)
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want chat", model.mode)
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(bubbleModel)
	if cmd != nil {
		t.Fatal("expected first Escape from chat to avoid quitting")
	}
	if model.mode != modePick {
		t.Fatalf("mode = %v, want picker", model.mode)
	}
	if !strings.Contains(model.status, "Esc again") {
		t.Fatalf("status = %q", model.status)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected second Escape to quit")
	}
}

func TestBubbleEscapeWaitsForInFlightSend(t *testing.T) {
	appStore := store.NewMemoryStore()
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.input.SetValue("hi")
	updated, sendCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if sendCmd == nil {
		t.Fatal("expected send command")
	}
	if !model.sending {
		t.Fatal("model was not marked sending")
	}

	updated, escCmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(bubbleModel)
	if escCmd != nil {
		t.Fatal("expected Escape during send to avoid quitting")
	}
	if model.mode != modeChat {
		t.Fatalf("mode = %v, want chat", model.mode)
	}
	if model.status != "Wait for current response." {
		t.Fatalf("status = %q", model.status)
	}

	msg := sendCmd()
	updated, _ = model.Update(msg)
	model = updated.(bubbleModel)
	if model.conversation.ID == "" || len(model.messages) != 2 {
		t.Fatalf("conversation=%#v messages=%#v", model.conversation, model.messages)
	}
}

func TestBubblePickerNewClearsCurrentChat(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Existing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.AddMessage(context.Background(), conversation.ID, "user", "old"); err != nil {
		t.Fatal(err)
	}
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.input.SetValue("1")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if model.conversation.ID == "" || len(model.messages) == 0 {
		t.Fatalf("expected existing chat, got conversation=%#v messages=%#v", model.conversation, model.messages)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(bubbleModel)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil {
		t.Fatal("expected no command while starting blank chat")
	}
	if model.conversation.ID != "" || len(model.messages) != 0 || len(model.attachments) != 0 {
		t.Fatalf("new chat kept stale state: conversation=%#v messages=%#v attachments=%#v", model.conversation, model.messages, model.attachments)
	}
}

func TestBubblePickerFiltersRecentChats(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Existing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.AddMessage(context.Background(), conversation.ID, "user", "old transcript"); err != nil {
		t.Fatal(err)
	}
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.input.SetValue("1")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if !strings.Contains(model.viewport.View(), "old transcript") {
		t.Fatalf("expected old transcript in viewport: %q", model.viewport.View())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(bubbleModel)
	model.input.SetValue("missing")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil {
		t.Fatal("expected no command while filtering picker")
	}
	if model.mode != modePick || !strings.Contains(model.status, "No matching chat") {
		t.Fatalf("mode=%v status=%q", model.mode, model.status)
	}
	model.input.SetValue("existing")
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil {
		t.Fatal("expected no command while opening filtered chat")
	}
	if model.mode != modeChat || model.conversation.ID != conversation.ID || len(model.messages) != 1 {
		t.Fatalf("conversation=%#v messages=%#v", model.conversation, model.messages)
	}
}

func TestBubblePickerDoesNotFilterNumberInput(t *testing.T) {
	model := bubbleModel{}
	model.input.SetValue("1")
	if got := model.pickerQuery(); got != "" {
		t.Fatalf("query = %q", got)
	}
	model.input.SetValue("existing")
	if got := model.pickerQuery(); got != "existing" {
		t.Fatalf("query = %q", got)
	}
}

func TestBubblePickerSearchesNumericTitles(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "2026")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.AddMessage(context.Background(), conversation.ID, "user", "numeric title"); err != nil {
		t.Fatal(err)
	}
	app := New(appStore, &fakeAssistant{response: "ok"}, strings.NewReader(""), &strings.Builder{})
	model, err := newBubbleModel(context.Background(), app)
	if err != nil {
		t.Fatalf("newBubbleModel() error = %v", err)
	}
	model.input.SetValue("2026")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(bubbleModel)
	if cmd != nil {
		t.Fatal("expected no command while opening numeric title")
	}
	if model.mode != modeChat || model.conversation.ID != conversation.ID || len(model.messages) != 1 {
		t.Fatalf("conversation=%#v messages=%#v status=%q", model.conversation, model.messages, model.status)
	}
}

func TestBubbleEscapeRequiresConfirmInPicker(t *testing.T) {
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

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(bubbleModel)
	if cmd != nil {
		t.Fatal("expected first Escape from picker to avoid quitting")
	}
	if !strings.Contains(model.status, "Esc again") {
		t.Fatalf("status = %q", model.status)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected second Escape to quit")
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
	output := renderMarkdownForBubble(strings.Join([]string{
		"- **HTML:** use `<h1>`",
		"1. Run checks",
		"> quoted",
	}, "\n"), styles)
	if strings.Contains(output, "**HTML:**") || strings.Contains(output, "`<h1>`") {
		t.Fatalf("inline markdown was not rendered: %q", output)
	}
	if !strings.Contains(output, "HTML:") || !strings.Contains(output, "<h1>") || !strings.Contains(output, "1.") || !strings.Contains(output, "quoted") {
		t.Fatalf("output = %q", output)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("output did not include syntax color: %q", output)
	}
}

func TestBubbleFooterUsesCompactStatus(t *testing.T) {
	model := bubbleModel{status: "Ready."}
	if got := model.footerStatus(); got != "Ready  ·  :help" {
		t.Fatalf("footer = %q", got)
	}
	model.sending = true
	model.status = "Writing"
	if got := model.footerStatus(); got != "Writing  ·  Esc waits  ·  :help" {
		t.Fatalf("footer = %q", got)
	}
	if strings.Contains(model.footerStatus(), "Linea is writing") || strings.Contains(model.footerStatus(), "for commands") {
		t.Fatalf("footer is too loud: %q", model.footerStatus())
	}
	model = bubbleModel{status: "Ready."}
	model.viewport = viewport.New(20, 2)
	model.viewport.SetContent(strings.Join([]string{"one", "two", "three", "four"}, "\n"))
	if got := model.footerStatus(); got != "Ready  ·  top  ·  :help" {
		t.Fatalf("footer = %q", got)
	}
	model.viewport.GotoBottom()
	if got := model.footerStatus(); got != "Ready  ·  bottom  ·  :help" {
		t.Fatalf("footer = %q", got)
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
		"Ready · :help",
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
