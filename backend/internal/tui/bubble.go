package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"linea/backend/internal/llm"
	"linea/backend/internal/store"
)

type screenMode int

const (
	modePick screenMode = iota
	modeChat
)

const maxPickerItems = 8

type sentMsg struct {
	conversation store.Conversation
	user         store.Message
	assistant    store.Message
	status       string
	err          error
}

type bubbleModel struct {
	app          *App
	ctx          context.Context
	mode         screenMode
	conversation store.Conversation
	messages     []store.Message
	shareText    string
	attachments  []llm.Attachment
	recent       []store.Conversation
	input        textinput.Model
	viewport     viewport.Model
	status       string
	width        int
	height       int
	sending      bool
	escPending   bool
}

type bubbleStyles struct {
	frame      lipgloss.Style
	title      lipgloss.Style
	muted      lipgloss.Style
	accent     lipgloss.Style
	user       lipgloss.Style
	bubble     lipgloss.Style
	code       lipgloss.Style
	codeInline lipgloss.Style
	syntaxTag  lipgloss.Style
	syntaxStr  lipgloss.Style
	syntaxNum  lipgloss.Style
	syntaxKey  lipgloss.Style
	syntaxCom  lipgloss.Style
	status     lipgloss.Style
	composer   lipgloss.Style
	attachment lipgloss.Style
}

func (a *App) runBubble(ctx context.Context) error {
	inFile, _ := a.input.(*os.File)
	outFile, _ := a.out.(*os.File)
	model, err := newBubbleModel(ctx, a)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(
		model,
		tea.WithInput(inFile),
		tea.WithOutput(outFile),
		tea.WithAltScreen(),
	).Run()
	return err
}

func newBubbleModel(ctx context.Context, app *App) (bubbleModel, error) {
	recent, err := app.store.ListConversations(ctx)
	if err != nil {
		return bubbleModel{}, err
	}
	input := textinput.New()
	input.CharLimit = maxPromptBytes
	input.Width = 72
	input.Prompt = "Message › "
	input.Placeholder = "Ask Linea"
	input.Focus()
	vp := viewport.New(88, 18)
	mode := modeChat
	status := "Ready."
	if len(recent) > 0 {
		mode = modePick
		input.Prompt = "Open › "
		input.Placeholder = "Number or Enter for new"
		status = "Choose recent or press Enter."
	}
	model := bubbleModel{
		app:      app,
		ctx:      ctx,
		mode:     mode,
		recent:   recent,
		input:    input,
		viewport: vp,
		status:   status,
		width:    96,
		height:   28,
	}
	model.resize()
	model.syncViewport()
	return model, nil
}

func (m bubbleModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m bubbleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		m.syncViewport()
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			return m.pressEscape()
		case tea.KeyEnter:
			m.escPending = false
			value := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			if m.mode == modePick {
				return m.pick(value)
			}
			return m.submit(value)
		default:
			m.escPending = false
		}
	case sentMsg:
		m.sending = false
		if msg.err != nil {
			if msg.conversation.ID != "" {
				m.conversation = msg.conversation
			}
			if msg.user.ID != "" {
				m.messages = append(m.messages, msg.user)
			}
			m.attachments = nil
			m.status = msg.err.Error()
			m.syncViewport()
			m.viewport.GotoBottom()
			return m, nil
		}
		m.conversation = msg.conversation
		m.messages = append(m.messages, msg.user, msg.assistant)
		m.attachments = nil
		m.status = msg.status
		m.syncViewport()
		m.viewport.GotoBottom()
		return m, nil
	}
	if m.mode == modeChat {
		var viewportCmd tea.Cmd
		m.viewport, viewportCmd = m.viewport.Update(msg)
		if viewportCmd != nil {
			return m, viewportCmd
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m bubbleModel) pressEscape() (tea.Model, tea.Cmd) {
	if m.mode == modeChat {
		if m.sending {
			m.status = "Wait for current response."
			m.escPending = false
			return m, nil
		}
		if recent, err := m.app.store.ListConversations(m.ctx); err == nil {
			m.recent = recent
		}
		m.mode = modePick
		m.input.Prompt = "Open › "
		m.input.Placeholder = "Number or Enter for new"
		m.input.SetValue("")
		m.status = "Back. Esc again quits."
		m.escPending = true
		return m, nil
	}
	if m.escPending {
		return m, tea.Quit
	}
	m.status = "Esc again quits."
	m.escPending = true
	return m, nil
}

func (m bubbleModel) pick(value string) (tea.Model, tea.Cmd) {
	switch value {
	case ":quit", ":q", "exit":
		return m, tea.Quit
	}
	if value == "" {
		m = m.startNewChat()
		m.syncViewport()
		return m, nil
	}
	index, err := strconv.Atoi(value)
	items := m.filteredRecent("")
	if err == nil && index >= 1 && index <= len(items) && index <= maxPickerItems {
		return m.openConversation(items[index-1])
	}
	if err == nil && len(m.filteredRecent(value)) == 0 {
		m.status = "No chat at that number."
		return m, nil
	}
	matches := m.filteredRecent(value)
	switch len(matches) {
	case 0:
		m.status = "No matching chat. Enter starts new."
		return m, nil
	case 1:
		return m.openConversation(matches[0])
	default:
		m.input.SetValue(value)
		m.status = fmt.Sprintf("%d matches. Refine search.", len(matches))
		return m, nil
	}
}

func (m bubbleModel) openConversation(conversation store.Conversation) (tea.Model, tea.Cmd) {
	m.conversation = conversation
	m.shareText = ""
	messages, err := m.app.store.ListMessages(m.ctx, m.conversation.ID)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.messages = messages
	m.mode = modeChat
	m.escPending = false
	m.input.Prompt = "Message › "
	m.input.Placeholder = "Ask Linea"
	m.status = "Ready."
	m.syncViewport()
	m.viewport.GotoBottom()
	return m, nil
}

func (m bubbleModel) filteredRecent(query string) []store.Conversation {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return append([]store.Conversation(nil), m.recent...)
	}
	filtered := []store.Conversation{}
	for _, conversation := range m.recent {
		if strings.Contains(strings.ToLower(conversation.Title), query) {
			filtered = append(filtered, conversation)
		}
	}
	return filtered
}

func (m bubbleModel) startNewChat() bubbleModel {
	m.mode = modeChat
	m.conversation = store.Conversation{}
	m.messages = nil
	m.shareText = ""
	m.attachments = nil
	m.escPending = false
	m.input.Prompt = "Message › "
	m.input.Placeholder = "Ask Linea"
	m.status = "Started new chat."
	return m
}

func (m bubbleModel) submit(value string) (tea.Model, tea.Cmd) {
	if m.sending {
		switch value {
		case ":quit", ":q", "exit":
			return m, tea.Quit
		default:
			m.status = "Wait for current response."
			return m, nil
		}
	}
	switch value {
	case "":
		return m, nil
	case ":quit", ":q", "exit":
		return m, tea.Quit
	case ":new":
		m.conversation = store.Conversation{}
		m.messages = nil
		m.shareText = ""
		m.attachments = nil
		m.status = "Started new chat."
		m.syncViewport()
		return m, nil
	}
	if nextTitle, ok := strings.CutPrefix(value, ":rename "); ok {
		return m.renameCurrentChat(nextTitle)
	}
	if value == ":delete" {
		m.status = "Use :delete confirm to delete this chat."
		return m, nil
	}
	if value == ":delete confirm" {
		return m.deleteCurrentChat()
	}
	if value == ":share" {
		return m.shareCurrentChat()
	}
	if path, ok := strings.CutPrefix(value, ":attach "); ok {
		return m.queueAttachment(path)
	}
	if attachment, ok, err := m.app.attachmentFromDroppedPath(value); ok {
		if err != nil {
			m.status = fmt.Sprintf("Could not attach file: %v", err)
			return m, nil
		}
		m.attachments = append(m.attachments, attachment)
		m.status = "Attached " + attachment.Name + "."
		return m, nil
	}
	if output, ok := m.app.handleAgentCommand(m.ctx, value); ok {
		m.shareText = ""
		m.messages = append(m.messages, store.Message{ID: store.NewID(), Role: "assistant", Content: output})
		m.status = "Ready."
		m.syncViewport()
		m.viewport.GotoBottom()
		return m, nil
	}
	m.shareText = ""
	m.sending = true
	m.status = "Writing"
	m.syncViewport()
	m.viewport.GotoBottom()
	return m, m.send(value)
}

func (m bubbleModel) renameCurrentChat(title string) (tea.Model, tea.Cmd) {
	title = strings.TrimSpace(title)
	if title == "" {
		m.status = "Choose a title."
		return m, nil
	}
	if m.conversation.ID == "" {
		m.status = "Send a message before renaming."
		return m, nil
	}
	conversation, err := m.app.store.UpdateConversationTitle(m.ctx, m.conversation.ID, title)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.conversation = conversation
	m.status = "Renamed."
	m.syncViewport()
	return m, nil
}

func (m bubbleModel) deleteCurrentChat() (tea.Model, tea.Cmd) {
	if m.conversation.ID == "" {
		m.status = "No saved chat to delete."
		return m, nil
	}
	if err := m.app.store.DeleteConversation(m.ctx, m.conversation.ID); err != nil {
		m.status = err.Error()
		return m, nil
	}
	if recent, err := m.app.store.ListConversations(m.ctx); err == nil {
		m.recent = recent
	}
	m = m.startNewChat()
	m.status = "Deleted chat."
	m.syncViewport()
	return m, nil
}

func (m bubbleModel) shareCurrentChat() (tea.Model, tea.Cmd) {
	if m.conversation.ID == "" || len(m.messages) == 0 {
		m.status = "No saved chat to share."
		return m, nil
	}
	content := formatConversationShareText(m.conversation, m.messages)
	m.shareText = content
	m.status = "Share text shown."
	m.syncViewport()
	m.viewport.GotoBottom()
	return m, nil
}

func (m bubbleModel) queueAttachment(path string) (tea.Model, tea.Cmd) {
	attachment, err := m.app.readAttachment(path)
	if err != nil {
		m.status = fmt.Sprintf("Could not attach file: %v", err)
		return m, nil
	}
	m.attachments = append(m.attachments, attachment)
	m.status = "Attached " + attachment.Name + "."
	return m, nil
}

func (m bubbleModel) send(input string) tea.Cmd {
	return func() tea.Msg {
		conversation := m.conversation
		if conversation.ID == "" {
			created, err := m.app.store.CreateConversation(m.ctx, store.TitleFromMessage(input))
			if err != nil {
				return sentMsg{err: err}
			}
			conversation = created
		}
		userMessage, err := m.app.store.AddMessage(m.ctx, conversation.ID, "user", input)
		if err != nil {
			return sentMsg{err: err}
		}
		messages := append(append([]store.Message(nil), m.messages...), userMessage)
		searchResults, searchErr := m.app.searchIfNeeded(m.ctx, input)
		status := "Ready."
		if searchErr != nil {
			status = fmt.Sprintf("Search unavailable: %v", searchErr)
			searchResults = nil
		} else if len(searchResults) > 0 {
			status = fmt.Sprintf("Used %d search result(s).", len(searchResults))
		}
		var response strings.Builder
		err = m.app.assistant.GenerateStream(m.ctx, messages, m.attachments, searchResults, func(chunk string) error {
			_, writeErr := io.WriteString(&response, chunk)
			return writeErr
		})
		if err != nil {
			return sentMsg{conversation: conversation, user: userMessage, err: err}
		}
		content := response.String()
		if strings.TrimSpace(content) == "" {
			return sentMsg{conversation: conversation, user: userMessage, err: errors.New("assistant returned an empty response")}
		}
		assistantMessage, err := m.app.store.AddMessage(m.ctx, conversation.ID, "assistant", content)
		if err != nil {
			return sentMsg{err: err}
		}
		return sentMsg{conversation: conversation, user: userMessage, assistant: assistantMessage, status: status}
	}
}

func (m bubbleModel) View() string {
	styles := m.styles()
	if m.mode == modePick {
		return m.viewPicker(styles)
	}
	return m.viewChat(styles)
}

func (m bubbleModel) viewPicker(styles bubbleStyles) string {
	var b strings.Builder
	b.WriteString(styles.title.Render("Linea"))
	b.WriteString("\n")
	b.WriteString(styles.muted.Render("Recent"))
	b.WriteString("\n\n")
	items := m.filteredRecent(m.pickerQuery())
	limit := len(items)
	if limit > maxPickerItems {
		limit = maxPickerItems
	}
	if len(m.recent) == 0 {
		b.WriteString(styles.muted.Render("No recent chats."))
		b.WriteString("\n")
	} else if limit == 0 {
		b.WriteString(styles.muted.Render("No matches."))
		b.WriteString("\n")
	}
	for i := 0; i < limit; i++ {
		item := fmt.Sprintf("%d. %s", i+1, items[i].Title)
		date := items[i].UpdatedAt.Local().Format("2 Jan 15:04")
		b.WriteString(styles.bubble.Render(item + styles.muted.Render(" · "+date)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styles.composer.Width(m.composerWidth()).Render(lipgloss.NewStyle().Width(m.input.Width).Render(m.input.View())))
	b.WriteString("\n")
	b.WriteString(styles.status.Render(m.pickerStatus()))
	return styles.frame.Width(m.contentWidth()).Render(b.String())
}

func (m bubbleModel) viewChat(styles bubbleStyles) string {
	var b strings.Builder
	title := m.conversation.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled"
	}
	b.WriteString(styles.title.Render("Linea"))
	b.WriteString(styles.muted.Render("  " + title))
	b.WriteString("\n\n")
	b.WriteString(m.viewport.View())
	if len(m.attachments) > 0 {
		b.WriteString("\n")
		for _, attachment := range m.attachments {
			b.WriteString(styles.attachment.Render("• " + attachment.Name + " · " + attachment.MIMEType))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(styles.composer.Width(m.composerWidth()).Render(lipgloss.NewStyle().Width(m.input.Width).Render(m.input.View())))
	b.WriteString("\n")
	b.WriteString(styles.status.Render(m.footerStatus()))
	return styles.frame.Width(m.contentWidth()).Render(b.String())
}

func (m bubbleModel) pickerStatus() string {
	status := strings.TrimSpace(m.status)
	if status == "" {
		status = "Choose recent or press Enter."
	}
	return status + "  ·  :quit"
}

func (m bubbleModel) pickerQuery() string {
	value := strings.TrimSpace(m.input.Value())
	if value == "" || strings.HasPrefix(value, ":") {
		return ""
	}
	if _, err := strconv.Atoi(value); err == nil {
		return ""
	}
	return value
}

func (m bubbleModel) footerStatus() string {
	status := strings.TrimSpace(m.status)
	if status == "" || status == "Ready." {
		status = "Ready"
	}
	if m.sending || status == "Writing" {
		return strings.Join(compactParts("Writing", m.scrollHint(), "Esc waits", ":help"), "  ·  ")
	}
	return strings.Join(compactParts(status, m.scrollHint(), ":help"), "  ·  ")
}

func (m bubbleModel) scrollHint() string {
	if m.viewport.AtTop() && m.viewport.AtBottom() {
		return ""
	}
	switch {
	case m.viewport.AtTop():
		return "top"
	case m.viewport.AtBottom():
		return "bottom"
	default:
		return "scroll"
	}
}

func compactParts(parts ...string) []string {
	next := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			next = append(next, part)
		}
	}
	return next
}

func (m bubbleModel) transcript(styles bubbleStyles) string {
	if len(m.messages) == 0 && m.shareText == "" {
		return styles.muted.Render("No messages yet.")
	}
	var b strings.Builder
	for _, message := range m.messages {
		b.WriteString(m.renderBubbleMessage(styles, message))
		b.WriteString("\n")
	}
	if m.shareText != "" {
		b.WriteString(m.renderBubbleMessage(styles, store.Message{Role: "assistant", Content: m.shareText}))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m bubbleModel) renderBubbleMessage(styles bubbleStyles, message store.Message) string {
	label := styles.accent.Render("Linea")
	if message.Role == "user" {
		label = styles.user.Render("You")
	}
	body := renderMarkdownForBubble(strings.TrimRight(message.Content, "\n"), styles)
	return label + "\n" + styles.bubble.Width(m.bubbleWidth()).Render(body)
}

func (m *bubbleModel) resize() {
	contentWidth := m.contentWidth()
	m.input.Width = max(24, m.composerWidth()-6)
	m.viewport.Width = contentWidth
	m.viewport.Height = max(6, m.height-10)
}

func (m *bubbleModel) syncViewport() {
	m.viewport.SetContent(m.transcript(m.styles()))
}

func (m bubbleModel) styles() bubbleStyles {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.ANSI256)
	renderer.SetHasDarkBackground(true)
	style := func() lipgloss.Style {
		return lipgloss.NewStyle().Renderer(renderer)
	}
	return bubbleStyles{
		frame:      style().Padding(1, 2),
		title:      style().Bold(true).Foreground(lipgloss.Color("252")),
		muted:      style().Foreground(lipgloss.Color("244")),
		accent:     style().Foreground(lipgloss.Color("109")),
		user:       style().Foreground(lipgloss.Color("180")),
		bubble:     style().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("237")).Padding(0, 1),
		code:       style().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1).Foreground(lipgloss.Color("250")),
		codeInline: style().Foreground(lipgloss.Color("223")),
		syntaxTag:  style().Foreground(lipgloss.Color("109")),
		syntaxStr:  style().Foreground(lipgloss.Color("180")),
		syntaxNum:  style().Foreground(lipgloss.Color("150")),
		syntaxKey:  style().Foreground(lipgloss.Color("147")),
		syntaxCom:  style().Foreground(lipgloss.Color("244")),
		status:     style().Foreground(lipgloss.Color("244")),
		composer:   style().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("239")).Padding(0, 1),
		attachment: style().Foreground(lipgloss.Color("250")),
	}
}

func (m bubbleModel) contentWidth() int {
	if m.width <= 0 {
		return 92
	}
	return max(48, m.width-8)
}

func (m bubbleModel) bubbleWidth() int {
	return max(32, m.contentWidth()-8)
}

func (m bubbleModel) composerWidth() int {
	return max(32, m.contentWidth()-8)
}

func renderMarkdownForBubble(content string, styles bubbleStyles) string {
	var b strings.Builder
	lines := strings.Split(content, "\n")
	inFence := false
	var code strings.Builder
	codeLanguage := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				label := strings.TrimSpace(codeLanguage)
				if label != "" {
					label += "\n"
				}
				b.WriteString(styles.code.Render(label + highlightCodeForBubble(strings.TrimRight(code.String(), "\n"), codeLanguage, styles)))
				b.WriteString("\n")
				code.Reset()
				codeLanguage = ""
				inFence = false
				continue
			}
			inFence = true
			codeLanguage = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			continue
		}
		if inFence {
			code.WriteString(line)
			code.WriteString("\n")
			continue
		}
		switch {
		case trimmed == "":
			b.WriteString("\n")
		case strings.HasPrefix(trimmed, "# "):
			b.WriteString(styles.title.Render(strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))))
			b.WriteString("\n")
		case strings.HasPrefix(trimmed, "## "):
			b.WriteString(styles.title.Render(strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))))
			b.WriteString("\n")
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			b.WriteString(styles.accent.Render("• "))
			b.WriteString(renderInlineForBubble(strings.TrimSpace(trimmed[2:]), styles))
			b.WriteString("\n")
		case isOrderedMarkdownLine(trimmed):
			number, text := splitOrderedMarkdownLine(trimmed)
			b.WriteString(styles.accent.Render(number + ". "))
			b.WriteString(renderInlineForBubble(text, styles))
			b.WriteString("\n")
		case strings.HasPrefix(trimmed, ">"):
			b.WriteString(styles.muted.Render("│ "))
			b.WriteString(renderInlineForBubble(strings.TrimSpace(strings.TrimPrefix(trimmed, ">")), styles))
			b.WriteString("\n")
		case trimmed == "---" || trimmed == "***":
			b.WriteString(styles.muted.Render(strings.Repeat("─", 24)))
			b.WriteString("\n")
		default:
			b.WriteString(renderInlineForBubble(line, styles))
			b.WriteString("\n")
		}
	}
	if inFence {
		b.WriteString(styles.code.Render(highlightCodeForBubble(strings.TrimRight(code.String(), "\n"), codeLanguage, styles)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderInlineForBubble(line string, styles bubbleStyles) string {
	line = replaceDelimited(line, "`", func(value string) string {
		return styles.codeInline.Inline(true).Render(highlightCodeForBubble(value, "", styles))
	})
	line = replaceDelimited(line, "**", func(value string) string {
		return styles.title.Inline(true).Render(value)
	})
	return line
}

func isOrderedMarkdownLine(line string) bool {
	index := 0
	for index < len(line) && unicode.IsDigit(rune(line[index])) {
		index++
	}
	return index > 0 && index+1 < len(line) && line[index] == '.' && unicode.IsSpace(rune(line[index+1]))
}

func splitOrderedMarkdownLine(line string) (string, string) {
	number, rest, _ := strings.Cut(line, ".")
	return number, strings.TrimSpace(rest)
}

func replaceDelimited(line string, delimiter string, render func(string) string) string {
	var b strings.Builder
	for {
		start := strings.Index(line, delimiter)
		if start < 0 {
			b.WriteString(line)
			break
		}
		end := strings.Index(line[start+len(delimiter):], delimiter)
		if end < 0 {
			b.WriteString(line)
			break
		}
		b.WriteString(line[:start])
		valueStart := start + len(delimiter)
		valueEnd := valueStart + end
		b.WriteString(render(line[valueStart:valueEnd]))
		line = line[valueEnd+len(delimiter):]
	}
	return b.String()
}

func highlightCodeForBubble(code string, language string, styles bubbleStyles) string {
	normalized := strings.ToLower(language)
	if strings.Contains(normalized, "html") || strings.Contains(normalized, "xml") || strings.Contains(code, "<") {
		return htmlTagPattern.ReplaceAllStringFunc(code, func(token string) string { return styles.syntaxTag.Render(token) })
	}
	code = commentPattern.ReplaceAllStringFunc(code, func(token string) string { return styles.syntaxCom.Render(token) })
	code = stringPattern.ReplaceAllStringFunc(code, func(token string) string { return styles.syntaxStr.Render(token) })
	code = numberPattern.ReplaceAllStringFunc(code, func(token string) string { return styles.syntaxNum.Render(token) })
	return keywordPattern.ReplaceAllStringFunc(code, func(token string) string {
		if isCodeKeyword(token) {
			return styles.syntaxKey.Render(token)
		}
		return token
	})
}
