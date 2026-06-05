package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"linea/backend/internal/agent"
	"linea/backend/internal/llm"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
)

const (
	maxTextAttachmentBytes  = int64(512 * 1024)
	maxImageAttachmentBytes = int64(2 * 1024 * 1024)
	maxPromptBytes          = 512 * 1024
)

var errAttachmentTooLarge = errors.New("attached files must be 512 KB or smaller; images must be 2 MB or smaller")

type Assistant interface {
	GenerateStream(ctx context.Context, messages []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult, onChunk func(string) error) error
}

type Searcher interface {
	Search(ctx context.Context, query string) ([]search.Result, error)
}

type AgentRuntime interface {
	Status(context.Context) agent.Status
	CallMCPTool(context.Context, agent.MCPCallInput) (agent.MCPCall, error)
	ListDiagnostics(context.Context) ([]agent.Diagnostic, error)
	SearchFiles(context.Context, string) ([]agent.SearchResult, error)
	ReadFile(context.Context, string) (agent.FileResult, error)
	ListSymbols(context.Context, string) ([]agent.WorkspaceSymbol, error)
	ListReferences(context.Context, string) ([]agent.WorkspaceReference, error)
	StartAgentLoop(context.Context, agent.AgentLoopInput) (agent.AgentLoop, error)
	ContinueAgentLoop(context.Context, string, agent.AgentLoopContinueInput) (agent.AgentLoop, error)
	CancelAgentLoop(context.Context, string) (agent.AgentLoop, error)
	RunSubagent(context.Context, string, agent.SubagentRunInput) (agent.SubagentRun, error)
	CheckCommand(context.Context, agent.CommandCheckInput) (agent.CommandCheck, error)
	ListCommandApprovals(context.Context) []agent.CommandApproval
	AddCommandApproval(context.Context, agent.CommandApprovalInput) (agent.CommandApproval, error)
	RunCommand(context.Context, agent.CommandCheckInput) (agent.CommandRun, error)
	RunHook(context.Context, string, agent.HookExecutionInput) (agent.HookExecution, error)
	RunSkill(context.Context, string, agent.SkillExecutionInput) (agent.SkillExecution, error)
	ListEditProposals(context.Context) []agent.EditProposal
	ReviewEditProposal(context.Context, string, agent.EditProposalReviewInput) (agent.EditProposal, error)
	ApplyEditProposal(context.Context, string) (agent.EditProposal, error)
}

type App struct {
	store      store.Store
	assistant  Assistant
	searcher   Searcher
	agent      AgentRuntime
	in         *bufio.Scanner
	input      io.Reader
	out        io.Writer
	attachBase string
	theme      theme
}

func New(appStore store.Store, assistant Assistant, in io.Reader, out io.Writer) *App {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxPromptBytes)
	return &App{
		store:     appStore,
		assistant: assistant,
		in:        scanner,
		input:     in,
		out:       out,
		theme:     newTheme(out),
	}
}

func (a *App) WithSearcher(searcher Searcher) *App {
	a.searcher = searcher
	return a
}

func (a *App) WithAgentRuntime(runtime AgentRuntime) *App {
	a.agent = runtime
	return a
}

func (a *App) WithAttachmentBase(dir string) *App {
	a.attachBase = dir
	return a
}

func (a *App) Run(ctx context.Context) error {
	if a.isInteractive() {
		return a.runBubble(ctx)
	}
	return a.runHandRolled(ctx)
}

func (a *App) RunBeta(ctx context.Context) error {
	return a.runHandRolled(ctx)
}

func (a *App) isInteractive() bool {
	inFile, inOK := a.inReader().(*os.File)
	if !inOK || !isTerminalWriter(a.out) {
		return false
	}
	outFile, _ := a.out.(*os.File)
	inInfo, inErr := inFile.Stat()
	outInfo, outErr := outFile.Stat()
	return inErr == nil && outErr == nil && inInfo.Mode()&os.ModeCharDevice != 0 && outInfo.Mode()&os.ModeCharDevice != 0
}

func (a *App) inReader() io.Reader {
	return a.input
}

func (a *App) runHandRolled(ctx context.Context) error {
	conversation, messages, err := a.pickConversation(ctx)
	if err != nil {
		return err
	}
	var attachments []llm.Attachment
	a.render(conversation, messages, attachments, "Ready.")
	for {
		fmt.Fprint(a.out, "\n"+a.theme.prompt("Message › "))
		if !a.in.Scan() {
			if err := a.in.Err(); err != nil {
				return err
			}
			return nil
		}
		input := a.in.Text()
		switch strings.TrimSpace(input) {
		case "":
			continue
		case ":quit", ":q", "exit":
			return nil
		case ":new":
			conversation = store.Conversation{}
			messages = nil
			attachments = nil
			a.render(conversation, messages, attachments, "Started new chat.")
			continue
		}
		if path, ok := strings.CutPrefix(strings.TrimSpace(input), ":attach "); ok {
			attachment, err := a.readAttachment(path)
			if err != nil {
				a.render(conversation, messages, attachments, fmt.Sprintf("Could not attach file: %v", err))
				continue
			}
			attachments = append(attachments, attachment)
			a.render(conversation, messages, attachments, fmt.Sprintf("Attached %s.", attachment.Name))
			continue
		}
		if attachment, ok, err := a.attachmentFromDroppedPath(input); ok {
			if err != nil {
				a.render(conversation, messages, attachments, fmt.Sprintf("Could not attach file: %v", err))
				continue
			}
			attachments = append(attachments, attachment)
			a.render(conversation, messages, attachments, fmt.Sprintf("Attached %s.", attachment.Name))
			continue
		}
		if output, ok := a.handleAgentCommand(ctx, input); ok {
			messages = append(messages, store.Message{ID: store.NewID(), Role: "assistant", Content: output})
			a.render(conversation, messages, attachments, "Ready.")
			continue
		}

		if conversation.ID == "" {
			created, err := a.store.CreateConversation(ctx, store.TitleFromMessage(input))
			if err != nil {
				return err
			}
			conversation = created
		}
		userMessage, err := a.store.AddMessage(ctx, conversation.ID, "user", input)
		if err != nil {
			return err
		}
		messages = append(messages, userMessage)

		searchResults, err := a.searchIfNeeded(ctx, input)
		if err != nil {
			a.render(conversation, messages, attachments, fmt.Sprintf("Search unavailable: %v", err))
			searchResults = nil
		}
		if len(searchResults) > 0 {
			a.render(conversation, messages, attachments, fmt.Sprintf("Using %d search result(s).", len(searchResults)))
		}
		fmt.Fprintf(a.out, "\n%s\n\n%s\n", a.theme.muted("Linea is writing..."), a.theme.accent("Linea"))
		var response strings.Builder
		err = a.assistant.GenerateStream(ctx, messages, attachments, searchResults, func(chunk string) error {
			response.WriteString(chunk)
			_, writeErr := io.WriteString(a.out, chunk)
			return writeErr
		})
		fmt.Fprintln(a.out)
		if err != nil {
			return err
		}
		attachments = nil
		content := response.String()
		if strings.TrimSpace(content) == "" {
			return errors.New("assistant returned an empty response")
		}
		assistantMessage, err := a.store.AddMessage(ctx, conversation.ID, "assistant", content)
		if err != nil {
			return err
		}
		messages = append(messages, assistantMessage)
		a.render(conversation, messages, attachments, "Ready.")
	}
}

func (a *App) pickConversation(ctx context.Context) (store.Conversation, []store.Message, error) {
	conversations, err := a.store.ListConversations(ctx)
	if err != nil {
		return store.Conversation{}, nil, err
	}
	if len(conversations) == 0 {
		return store.Conversation{}, nil, nil
	}
	limit := len(conversations)
	if limit > 5 {
		limit = 5
	}
	a.renderHeader("Choose Chat")
	fmt.Fprintln(a.out, a.theme.section("Recent"))
	for i := 0; i < limit; i++ {
		fmt.Fprintf(a.out, "%s %s %s\n", a.theme.accent(strconv.Itoa(i+1)+"."), a.theme.strong(conversations[i].Title), a.theme.muted(conversations[i].UpdatedAt.Local().Format("2 Jan 15:04")))
	}
	fmt.Fprintln(a.out)
	fmt.Fprint(a.out, a.theme.prompt("Open number, or press Enter for new: "))
	if !a.in.Scan() {
		if err := a.in.Err(); err != nil {
			return store.Conversation{}, nil, err
		}
		return store.Conversation{}, nil, nil
	}
	choice := strings.TrimSpace(a.in.Text())
	if choice == "" {
		return store.Conversation{}, nil, nil
	}
	index, err := strconv.Atoi(choice)
	if err != nil || index < 1 || index > limit {
		fmt.Fprintln(a.out, a.theme.muted("Starting new chat."))
		return store.Conversation{}, nil, nil
	}
	conversation := conversations[index-1]
	messages, err := a.store.ListMessages(ctx, conversation.ID)
	if err != nil {
		return store.Conversation{}, nil, err
	}
	return conversation, messages, nil
}

func (a *App) render(conversation store.Conversation, messages []store.Message, attachments []llm.Attachment, status string) {
	title := conversation.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled"
	}
	a.renderHeader(title)
	if len(messages) == 0 {
		fmt.Fprintln(a.out, a.theme.muted("No messages yet."))
	} else {
		fmt.Fprintln(a.out, a.theme.section("Transcript"))
		for _, message := range messages {
			a.renderMessage(message)
		}
	}
	if len(attachments) > 0 {
		fmt.Fprintln(a.out)
		fmt.Fprintln(a.out, a.theme.section("Queued attachments"))
		for _, attachment := range attachments {
			fmt.Fprintf(a.out, "  %s %s %s\n", a.theme.accent("•"), a.theme.strong(attachment.Name), a.theme.muted(attachment.MIMEType))
		}
	}
	fmt.Fprintln(a.out)
	fmt.Fprintf(a.out, "%s %s\n", a.theme.muted("Status:"), status)
	fmt.Fprintln(a.out, a.theme.muted("Commands: :new · :attach <path> · :help · :agent · :diag · :symbols <q> · :refs <id> · :mcp · :loop <goal> · :quit"))
}

func (a *App) renderHeader(title string) {
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, a.theme.brand("Linea Terminal"))
	fmt.Fprintln(a.out, a.theme.rule())
	fmt.Fprintf(a.out, "%s %s\n", a.theme.muted("Chat:"), a.theme.strong(title))
	fmt.Fprintln(a.out)
}

func (a *App) renderMessage(message store.Message) {
	label := "Linea"
	labelColor := a.theme.accent
	if message.Role == "user" {
		label = "You"
		labelColor = a.theme.user
	}
	fmt.Fprintf(a.out, "\n%s\n", labelColor(label))
	a.renderMarkdown(strings.TrimRight(message.Content, "\n"))
}

func (a *App) renderMarkdown(content string) {
	lines := strings.Split(content, "\n")
	inFence := false
	fenceLanguage := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				fmt.Fprintln(a.out, a.theme.codeBorder("  └"+strings.Repeat("─", 52)))
				inFence = false
				fenceLanguage = ""
				continue
			}
			inFence = true
			fenceLanguage = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			label := " code"
			if fenceLanguage != "" {
				label = " " + fenceLanguage
			}
			fmt.Fprintln(a.out, a.theme.codeBorder("  ┌"+strings.Repeat("─", 18)+label+" "+strings.Repeat("─", 24)))
			continue
		}
		if inFence {
			fmt.Fprintf(a.out, "%s %s\n", a.theme.codeBorder("  │"), a.highlightCode(line, fenceLanguage))
			continue
		}
		a.renderMarkdownLine(line)
	}
	if inFence {
		fmt.Fprintln(a.out, a.theme.codeBorder("  └"+strings.Repeat("─", 52)))
	}
}

func (a *App) renderMarkdownLine(line string) {
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "":
		fmt.Fprintln(a.out)
	case strings.HasPrefix(trimmed, "### "):
		fmt.Fprintln(a.out, a.theme.strong(strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))))
	case strings.HasPrefix(trimmed, "## "):
		fmt.Fprintln(a.out, a.theme.strong(strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))))
	case strings.HasPrefix(trimmed, "# "):
		fmt.Fprintln(a.out, a.theme.brand(strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))))
	case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
		fmt.Fprintf(a.out, "  %s %s\n", a.theme.accent("•"), a.renderInline(strings.TrimSpace(trimmed[2:])))
	default:
		fmt.Fprintln(a.out, a.renderInline(line))
	}
}

func (a *App) searchIfNeeded(ctx context.Context, input string) ([]llm.SearchResult, error) {
	if a.searcher == nil || !search.ShouldSearch(input) {
		return nil, nil
	}
	results, err := a.searcher.Search(ctx, search.QueryFromMessage(input))
	if err != nil {
		return nil, err
	}
	searchResults := make([]llm.SearchResult, 0, len(results))
	for _, result := range results {
		searchResults = append(searchResults, llm.SearchResult{
			Title:   result.Title,
			URL:     result.URL,
			Snippet: result.Snippet,
		})
	}
	return searchResults, nil
}

func (a *App) renderInline(line string) string {
	if !a.theme.enabled {
		return line
	}
	var b strings.Builder
	for {
		start := strings.Index(line, "`")
		if start < 0 {
			b.WriteString(line)
			break
		}
		end := strings.Index(line[start+1:], "`")
		if end < 0 {
			b.WriteString(line)
			break
		}
		b.WriteString(line[:start])
		code := line[start+1 : start+1+end]
		b.WriteString(a.theme.inlineCode(code))
		line = line[start+1+end+1:]
	}
	return b.String()
}

func (a *App) highlightCode(line string, language string) string {
	if !a.theme.enabled {
		return line
	}
	normalized := strings.ToLower(language)
	if strings.Contains(normalized, "html") || strings.Contains(normalized, "xml") {
		return htmlTagPattern.ReplaceAllStringFunc(line, a.theme.syntaxTag)
	}
	line = stringPattern.ReplaceAllStringFunc(line, a.theme.syntaxString)
	line = numberPattern.ReplaceAllStringFunc(line, a.theme.syntaxNumber)
	return keywordPattern.ReplaceAllStringFunc(line, func(token string) string {
		if isCodeKeyword(token) {
			return a.theme.syntaxKeyword(token)
		}
		return token
	})
}

func (a *App) attachmentFromDroppedPath(input string) (llm.Attachment, bool, error) {
	path := cleanDroppedPath(input)
	if path == "" {
		return llm.Attachment{}, false, nil
	}
	if !isLikelyPath(path) {
		return llm.Attachment{}, false, nil
	}
	if a.attachBase != "" && !filepath.IsAbs(path) {
		path = filepath.Join(a.attachBase, path)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return llm.Attachment{}, false, nil
	}
	attachment, err := a.readAttachment(path)
	return attachment, true, err
}

func (a *App) readAttachment(path string) (llm.Attachment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return llm.Attachment{}, errors.New("missing path")
	}
	if a.attachBase != "" && !filepath.IsAbs(path) {
		path = filepath.Join(a.attachBase, path)
	}
	name := filepath.Base(path)
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mimeType == "" {
		mimeType = "text/plain; charset=utf-8"
	}
	limit := maxTextAttachmentBytes
	if strings.HasPrefix(mimeType, "image/") {
		limit = maxImageAttachmentBytes
	}
	info, err := os.Stat(path)
	if err != nil {
		return llm.Attachment{}, err
	}
	if info.Size() > limit {
		return llm.Attachment{}, errAttachmentTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return llm.Attachment{}, err
	}
	attachment := llm.Attachment{Name: name, MIMEType: mimeType}
	if strings.HasPrefix(mimeType, "image/") {
		attachment.Data = data
		return attachment, nil
	}
	attachment.Content = string(data)
	return attachment, nil
}

func cleanDroppedPath(input string) string {
	input = strings.TrimSpace(input)
	if input == "" || strings.Contains(input, "\n") {
		return ""
	}
	if strings.HasPrefix(input, "file://") {
		input = strings.TrimPrefix(input, "file://")
	}
	input = strings.Trim(input, `"'`)
	input = strings.ReplaceAll(input, `\ `, " ")
	return input
}

type theme struct {
	enabled bool
}

func newTheme(out io.Writer) theme {
	return theme{enabled: isTerminalWriter(out) && os.Getenv("NO_COLOR") == ""}
}

func isTerminalWriter(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (t theme) paint(code string, value string) string {
	if !t.enabled || value == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func (t theme) brand(value string) string         { return t.paint("1;38;5;252", value) }
func (t theme) strong(value string) string        { return t.paint("1;38;5;250", value) }
func (t theme) muted(value string) string         { return t.paint("38;5;244", value) }
func (t theme) accent(value string) string        { return t.paint("38;5;109", value) }
func (t theme) user(value string) string          { return t.paint("38;5;180", value) }
func (t theme) section(value string) string       { return t.paint("1;38;5;245", value) }
func (t theme) prompt(value string) string        { return t.paint("38;5;250", value) }
func (t theme) codeBorder(value string) string    { return t.paint("38;5;238", value) }
func (t theme) inlineCode(value string) string    { return t.paint("38;5;223", "`"+value+"`") }
func (t theme) syntaxTag(value string) string     { return t.paint("38;5;109", value) }
func (t theme) syntaxString(value string) string  { return t.paint("38;5;180", value) }
func (t theme) syntaxNumber(value string) string  { return t.paint("38;5;150", value) }
func (t theme) syntaxKeyword(value string) string { return t.paint("38;5;147", value) }
func (t theme) rule() string                      { return t.muted(strings.Repeat("─", 56)) }

var (
	htmlTagPattern = regexp.MustCompile(`</?[-A-Za-z0-9:_]+(?:\s+[^<>]*)?/?>`)
	stringPattern  = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	numberPattern  = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	commentPattern = regexp.MustCompile(`//.*|<!--.*?-->`)
	keywordPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)
)

func isCodeKeyword(token string) bool {
	switch token {
	case "break", "case", "const", "continue", "default", "else", "for", "func", "function", "if", "import", "let", "package", "return", "select", "struct", "switch", "type", "var":
		return true
	}
	return false
}

func isLikelyPath(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") || strings.HasPrefix(input, "file://") {
		return true
	}
	if strings.ContainsRune(input, os.PathSeparator) {
		return true
	}
	return strings.Contains(input, ".") && !strings.ContainsFunc(input, unicode.IsSpace)
}
