import React, { FormEvent, KeyboardEvent as ReactKeyboardEvent, useEffect, useMemo, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  ArrowUpRight,
  BadgeHelp,
  BellOff,
  Bookmark,
  Brush,
  Check,
  Cloud,
  Crown,
  Cpu,
  Database,
  Eye,
  FileText,
  Handshake,
  Heart,
  Info,
  Moon,
  MoreHorizontal,
  Monitor,
  PanelRight,
  Paperclip,
  PartyPopper,
  PenLine,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Route,
  Search as SearchIcon,
  Share2,
  Smile,
  Trash2,
  Sun,
} from 'lucide-react';
import './styles.css';

const API_BASE = import.meta.env.VITE_API_BASE ?? '';
const ICON_STROKE = 1.5;
const PIN_ICON_SIZE = 13;
const PINNED_STORAGE_KEY = 'linea:pinned-conversations';
const SEARCH_RESULTS_STORAGE_KEY = 'linea:search-results';
const UI_PREFS_STORAGE_KEY = 'linea:ui-prefs';

type Conversation = {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
};

type Message = {
  id?: string;
  clientId?: string;
  provider?: ProviderInfo;
  conversationId?: string;
  role: 'user' | 'assistant';
  content: string;
  createdAt?: string;
};

type SearchResult = {
  Title: string;
  URL: string;
  Snippet: string;
};

type SystemStatus = {
  storage: string;
  search: string;
  providers: ProviderStatus[];
};

type ProviderStatus = {
  name: string;
  model?: string;
  enabled: boolean;
  role: string;
};

type ProviderInfo = {
  name: string;
  model: string;
};

type StreamChunk = {
  content: string;
  provider?: ProviderInfo;
};

type UIPrefs = {
  showModelBadge: boolean;
  showSleepAlert: boolean;
  theme: ThemeChoice;
};

type ThemeChoice = 'dark' | 'light' | 'system';
type ResolvedTheme = 'dark' | 'light';

const FEEDBACK_OPTIONS = [
  { id: 'handshake', label: 'Useful', Icon: Handshake },
  { id: 'heart', label: 'Loved', Icon: Heart },
  { id: 'crown', label: 'Best', Icon: Crown },
  { id: 'smile', label: 'Clear', Icon: Smile },
  { id: 'party', label: 'Great', Icon: PartyPopper },
] as const;

function App() {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [content, setContent] = useState('');
  const [draftContent, setDraftContent] = useState('');
  const [files, setFiles] = useState<File[]>([]);
  const [isSending, setIsSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isSidebarOpen, setIsSidebarOpen] = useState(() => !isNarrowViewport());
  const [isSystemPanelOpen, setIsSystemPanelOpen] = useState(false);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [openConversationMenu, setOpenConversationMenu] = useState<string | null>(null);
  const [renamingConversationId, setRenamingConversationId] = useState<string | null>(null);
  const [renameTitle, setRenameTitle] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Conversation | null>(null);
  const [pinnedIds, setPinnedIds] = useState<Set<string>>(() => loadPinnedConversationIds());
  const [searchResultsByConversation, setSearchResultsByConversation] = useState<Record<string, SearchResult[]>>(
    () => loadSearchResults(),
  );
  const [pendingSourceConversationId, setPendingSourceConversationId] = useState<string | null>(null);
  const [areSourcesVisible, setAreSourcesVisible] = useState(false);
  const [messageFeedback, setMessageFeedback] = useState<Record<string, string>>({});
  const [responseProviders, setResponseProviders] = useState<Record<string, ProviderInfo>>({});
  const [uiPrefs, setUIPrefs] = useState<UIPrefs>(() => loadUIPrefs());
  const [systemTheme, setSystemTheme] = useState<ResolvedTheme>(() => getSystemTheme());
  const [isThemePanelOpen, setIsThemePanelOpen] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const sidebarFooterRef = useRef<HTMLDivElement | null>(null);
  const renameCancelledRef = useRef(false);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const messageEndRef = useRef<HTMLDivElement | null>(null);
  const activeIdRef = useRef<string | null>(null);

  const activeConversation = useMemo(
    () => conversations.find((conversation) => conversation.id === activeId),
    [activeId, conversations],
  );
  const visibleConversations = useMemo(
    () =>
      [...conversations].sort((first, second) => {
        return new Date(second.updatedAt).getTime() - new Date(first.updatedAt).getTime();
      }),
    [conversations],
  );
  const pinnedConversations = useMemo(
    () => visibleConversations.filter((conversation) => pinnedIds.has(conversation.id)),
    [pinnedIds, visibleConversations],
  );
  const unpinnedConversations = useMemo(
    () => visibleConversations.filter((conversation) => !pinnedIds.has(conversation.id)),
    [pinnedIds, visibleConversations],
  );
  const chatTitle = activeConversation?.title ?? 'Untitled';
  const sourceConversationId = activeId ?? pendingSourceConversationId;
  const activeSearchResults = sourceConversationId ? (searchResultsByConversation[sourceConversationId] ?? []) : [];
  const showSources = activeSearchResults.length > 0 && areSourcesVisible;
  const shellClassName = [
    'shell',
    !isSidebarOpen ? 'sidebar-collapsed' : '',
    showSources ? 'sources-open' : '',
  ]
    .filter(Boolean)
    .join(' ');
  useEffect(() => {
    void loadConversations();
    void loadSystemStatus();
  }, []);

  useEffect(() => {
    const closeMenu = () => setOpenConversationMenu(null);
    window.addEventListener('click', closeMenu);
    return () => window.removeEventListener('click', closeMenu);
  }, []);

  useEffect(() => {
    if (!isSystemPanelOpen && !isThemePanelOpen) {
      return;
    }

    const closeFooterPanels = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Element && sidebarFooterRef.current?.contains(target)) {
        const interactiveFooterArea = target.closest('.system-panel, .theme-panel, .footer-actions');
        if (interactiveFooterArea && sidebarFooterRef.current.contains(interactiveFooterArea)) {
          return;
        }
      }
      setIsSystemPanelOpen(false);
      setIsThemePanelOpen(false);
    };
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key !== 'Escape') {
        return;
      }
      setIsSystemPanelOpen(false);
      setIsThemePanelOpen(false);
    };

    window.addEventListener('pointerdown', closeFooterPanels, true);
    window.addEventListener('keydown', closeOnEscape);
    return () => {
      window.removeEventListener('pointerdown', closeFooterPanels, true);
      window.removeEventListener('keydown', closeOnEscape);
    };
  }, [isSystemPanelOpen, isThemePanelOpen]);

  useEffect(() => {
    activeIdRef.current = activeId;
    if (!activeId) {
      setMessages([]);
      return;
    }
    void loadMessages(activeId);
  }, [activeId]);

  useEffect(() => {
    messageEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, isSending]);

  useEffect(() => {
    resizeComposer(textareaRef.current);
  }, [content]);

  useEffect(() => {
    const resolvedTheme = resolveTheme(uiPrefs.theme, systemTheme);
    document.documentElement.dataset.theme = resolvedTheme;
    document.documentElement.style.colorScheme = resolvedTheme;
    saveUIPrefs(uiPrefs);
  }, [systemTheme, uiPrefs]);

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: light)');
    const syncSystemTheme = () => setSystemTheme(media.matches ? 'light' : 'dark');
    syncSystemTheme();
    media.addEventListener('change', syncSystemTheme);
    return () => media.removeEventListener('change', syncSystemTheme);
  }, []);

  async function loadConversations() {
    const data = await request<Conversation[]>('/api/conversations');
    const nextConversations = Array.isArray(data) ? data : [];
    setConversations(nextConversations);
    setActiveId((current) => (current === null && draftContent.trim() ? null : current ?? nextConversations[0]?.id ?? null));
  }

  async function loadSystemStatus() {
    try {
      const data = await request<SystemStatus>('/api/status');
      setSystemStatus(data);
    } catch {
      setSystemStatus(null);
    }
  }

  async function loadMessages(conversationId: string) {
    setError(null);
    const data = await request<Message[]>(`/api/conversations/${conversationId}/messages`);
    if (activeIdRef.current === conversationId) {
      setMessages(Array.isArray(data) ? data : []);
    }
  }

  async function createConversation(initialTitle = 'Untitled', activate = true) {
    setError(null);
    const conversation = await request<Conversation>('/api/conversations', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: initialTitle }),
    });
    setConversations((items) => [conversation, ...items]);
    if (activate) {
      setActiveId(conversation.id);
    }
    return conversation;
  }

  function startNewChat() {
    setPendingSourceConversationId(null);
    setActiveId(null);
    setMessages([]);
    setContent(draftContent);
    setFiles([]);
    setError(null);
    window.requestAnimationFrame(() => textareaRef.current?.focus());
  }

  function selectConversation(conversationId: string) {
    if (!activeId) {
      setDraftContent(content);
    }
    setPendingSourceConversationId(null);
    setActiveId(conversationId);
    setContent('');
    setFiles([]);
    setError(null);
  }

  function updateContent(value: string) {
    setContent(value);
    if (!activeId) {
      setDraftContent(value);
    }
  }

  function requestDeleteConversation(conversation: Conversation) {
    setOpenConversationMenu(null);
    setDeleteTarget(conversation);
  }

  async function deleteConversation(conversation: Conversation) {
    setError(null);
    await request<void>(`/api/conversations/${conversation.id}`, { method: 'DELETE' });
    setDeleteTarget(null);
    setConversations((items) => {
      const nextItems = items.filter((item) => item.id !== conversation.id);
      if (activeId === conversation.id) {
        setActiveId(nextItems[0]?.id ?? null);
        setContent('');
        setFiles([]);
        if (nextItems.length === 0) {
          setMessages([]);
        }
      }
      return nextItems;
    });
  }

  function startRenameConversation(conversation: Conversation) {
    setOpenConversationMenu(null);
    renameCancelledRef.current = false;
    setRenamingConversationId(conversation.id);
    setRenameTitle(conversation.title);
  }

  async function renameConversation(conversation: Conversation, nextTitle = renameTitle) {
    if (renameCancelledRef.current) {
      renameCancelledRef.current = false;
      setRenamingConversationId(null);
      return;
    }
    const title = nextTitle.trim();
    setRenamingConversationId(null);
    if (!title || title === conversation.title) {
      return;
    }
    try {
      setError(null);
      const updated = await request<Conversation>(`/api/conversations/${conversation.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title }),
      });
      setConversations((items) => items.map((item) => (item.id === updated.id ? updated : item)));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not rename conversation.');
    }
  }

  function togglePinned(conversation: Conversation) {
    setOpenConversationMenu(null);
    setPinnedIds((current) => {
      const next = new Set(current);
      if (next.has(conversation.id)) {
        next.delete(conversation.id);
      } else {
        next.add(conversation.id);
      }
      savePinnedConversationIds(next);
      return next;
    });
  }

  async function shareConversation(conversation: Conversation) {
    try {
      setError(null);
      setOpenConversationMenu(null);
      const conversationMessages =
        conversation.id === activeId
          ? messages
          : await request<Message[]>(`/api/conversations/${conversation.id}/messages`);
      await navigator.clipboard.writeText(formatConversationShare(conversation, conversationMessages));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not share conversation.');
    }
  }

  function editMessage(message: Message) {
    setContent(message.content);
    setError(null);
    window.requestAnimationFrame(() => textareaRef.current?.focus());
  }

  async function sendMessage(event: FormEvent) {
    event.preventDefault();
    await submitMessage();
  }

  async function submitMessage() {
    const trimmed = content.trim();
    if (!trimmed || isSending) {
      return;
    }

    setIsSending(true);
    setError(null);
    const optimisticUser: Message = { role: 'user', content: trimmed };
    let assistantDraft: Message | null = null;
    let submittedConversationId: string | null = null;
    setMessages((items) => [...items, optimisticUser]);
    updateContent('');

    try {
      const conversation = activeId
        ? conversations.find((item) => item.id === activeId)
        : await createConversation(trimmed.slice(0, 60) || 'Untitled', false);
      if (!conversation) {
        throw new Error('No active conversation.');
      }
      submittedConversationId = conversation.id;
      setPendingSourceConversationId(conversation.id);

      const form = new FormData();
      form.append('content', trimmed);
      files.forEach((file) => form.append('files', file));

      setFiles([]);
      const assistantClientId = `response-${Date.now()}`;
      let assistantProvider: ProviderInfo | undefined;
      const draft: Message = { clientId: assistantClientId, role: 'assistant', content: '' };
      assistantDraft = draft;
      setMessages((items) => [...items.filter((item) => item !== optimisticUser), optimisticUser, draft]);
      setConversationSearchResults(conversation.id, []);

      await streamMessage(`/api/conversations/${conversation.id}/messages`, form, {
        onUser: (message) => {
          setMessages((items) => items.map((item) => (item === optimisticUser ? message : item)));
        },
        onSearch: (result) => {
          appendConversationSearchResult(conversation.id, result);
        },
        onProvider: (provider) => {
          assistantProvider = provider;
          setResponseProviders((current) => ({ ...current, [assistantClientId]: provider }));
          setMessages((items) =>
            items.map((item) => (item.clientId === assistantClientId ? { ...item, provider } : item)),
          );
        },
        onChunk: (chunk) => {
          if (chunk.provider) {
            assistantProvider = chunk.provider;
            setResponseProviders((current) => ({ ...current, [assistantClientId]: chunk.provider as ProviderInfo }));
          }
          setMessages((items) =>
            items.map((item) =>
              item.clientId === assistantClientId
                ? { ...item, provider: item.provider ?? assistantProvider, content: item.content + chunk.content }
                : item,
            ),
          );
        },
        onDone: (message) => {
          setMessages((items) =>
            items.map((item) =>
              item.clientId === assistantClientId
                ? { ...message, clientId: assistantClientId, provider: item.provider ?? assistantProvider ?? message.provider }
                : item,
            ),
          );
        },
      });
      await loadConversations();
      setActiveId(conversation.id);
      if (!activeId) {
        setDraftContent('');
      }
    } catch (err) {
      setMessages((items) => items.filter((item) => item !== optimisticUser && item !== assistantDraft));
      if (submittedConversationId) {
        await loadMessages(submittedConversationId);
      }
      setError(err instanceof Error ? err.message : 'Something went wrong.');
    } finally {
      setPendingSourceConversationId(null);
      setIsSending(false);
    }
  }

  function handleComposerKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== 'Enter' || (!event.metaKey && !event.ctrlKey)) {
      return;
    }
    event.preventDefault();
    void submitMessage();
  }

  function setConversationSearchResults(conversationId: string, results: SearchResult[]) {
    setSearchResultsByConversation((current) => {
      const next = { ...current, [conversationId]: results };
      saveSearchResults(next);
      return next;
    });
  }

  function appendConversationSearchResult(conversationId: string, result: SearchResult) {
    setSearchResultsByConversation((current) => {
      const results = current[conversationId] ?? [];
      if (results.some((item) => item.URL === result.URL && item.Title === result.Title)) {
        return current;
      }
      const next = { ...current, [conversationId]: [...results, result] };
      saveSearchResults(next);
      return next;
    });
  }

  return (
    <main className={shellClassName}>
      {isSidebarOpen && (
        <button
          aria-label="Hide conversations"
          className="sidebar-scrim"
          type="button"
          onClick={() => setIsSidebarOpen(false)}
        />
      )}
      {isSidebarOpen && (
        <aside className="sidebar">
          <div className="sidebar-top">
            <div className="brand">
              <div className="mark">
                <Route size={16} strokeWidth={ICON_STROKE} />
              </div>
              <h1>Linea</h1>
            </div>
            <button
              aria-label="Hide conversations"
              className="sidebar-close icon-button subtle"
              type="button"
              onClick={() => setIsSidebarOpen(false)}
            >
              <PanelRight className="panel-toggle-icon" size={18} strokeWidth={ICON_STROKE} />
            </button>
          </div>

          <button className="new-chat" type="button" onClick={startNewChat}>
            <Plus size={14} strokeWidth={ICON_STROKE} />
            New
          </button>

          <nav className="conversation-list" aria-label="Conversations">
            {pinnedConversations.length > 0 && (
              <div className="conversation-section" aria-label="Pinned conversations">
                <div className="conversation-section-title">
                  <Bookmark size={PIN_ICON_SIZE} strokeWidth={ICON_STROKE} />
                  Pinned
                </div>
                {pinnedConversations.map((conversation) => (
                  <ConversationRow
                    activeId={activeId}
                    conversation={conversation}
                    isPinned={pinnedIds.has(conversation.id)}
                    menuOpen={openConversationMenu === conversation.id}
                    renameTitle={renameTitle}
                    renaming={renamingConversationId === conversation.id}
                    onDelete={requestDeleteConversation}
                    onMenuChange={setOpenConversationMenu}
                    onRename={renameConversation}
                    onRenameCancel={() => {
                      renameCancelledRef.current = true;
                      setRenameTitle('');
                      setRenamingConversationId(null);
                    }}
                    onRenameStart={startRenameConversation}
                    onRenameTitleChange={setRenameTitle}
                    onSelect={selectConversation}
                    onShare={shareConversation}
                    onTogglePinned={togglePinned}
                    key={conversation.id}
                  />
                ))}
              </div>
            )}
            <div className="conversation-section" aria-label="Recent conversations">
              {pinnedConversations.length > 0 && <div className="conversation-section-title">Recent</div>}
              {(!activeId || draftContent.trim()) && (
                <div className={!activeId ? 'conversation active draft-conversation' : 'conversation draft-conversation'}>
                  <button className="conversation-select" type="button" onClick={startNewChat}>
                    <span>Untitled</span>
                    <time>Draft</time>
                  </button>
                </div>
              )}
              {unpinnedConversations.map((conversation) => (
                <ConversationRow
                  activeId={activeId}
                  conversation={conversation}
                  isPinned={pinnedIds.has(conversation.id)}
                  menuOpen={openConversationMenu === conversation.id}
                  renameTitle={renameTitle}
                  renaming={renamingConversationId === conversation.id}
                  onDelete={requestDeleteConversation}
                  onMenuChange={setOpenConversationMenu}
                  onRename={renameConversation}
                  onRenameCancel={() => {
                    renameCancelledRef.current = true;
                    setRenameTitle('');
                    setRenamingConversationId(null);
                  }}
                  onRenameStart={startRenameConversation}
                  onRenameTitleChange={setRenameTitle}
                  onSelect={selectConversation}
                  onShare={shareConversation}
                  onTogglePinned={togglePinned}
                  key={conversation.id}
                />
              ))}
            </div>
          </nav>

          <div className="sidebar-footer" ref={sidebarFooterRef}>
            {isSystemPanelOpen && <SystemPanel status={systemStatus} />}
            {isThemePanelOpen && <ThemePanel prefs={uiPrefs} systemTheme={systemTheme} onChange={setUIPrefs} />}
            <div className="footer-actions">
              <button
                aria-label={isSystemPanelOpen ? 'Hide system status' : 'Show system status'}
                className="system-button has-tooltip tooltip-above"
                data-tooltip={isSystemPanelOpen ? 'Hide status' : 'System status'}
                type="button"
                onClick={() => {
                  setIsSystemPanelOpen((open) => !open);
                  setIsThemePanelOpen(false);
                  if (!systemStatus) {
                    void loadSystemStatus();
                  }
                }}
              >
                <Info size={14} strokeWidth={ICON_STROKE} />
              </button>
              <button
                aria-label={isThemePanelOpen ? 'Hide theme' : 'Choose theme'}
                className="system-button has-tooltip tooltip-above"
                data-tooltip="Theme"
                type="button"
                onClick={() => {
                  setIsThemePanelOpen((open) => !open);
                  setIsSystemPanelOpen(false);
                }}
              >
                <Brush size={14} strokeWidth={ICON_STROKE} />
              </button>
            </div>
          </div>
        </aside>
      )}

      <section className="chat">
        <header className="chat-header">
          <div className="chat-title">
            <button
              aria-label={isSidebarOpen ? 'Hide conversations' : 'Show conversations'}
              className="icon-button subtle has-tooltip tooltip-align-left"
              data-tooltip={isSidebarOpen ? 'Hide conversations' : 'Show conversations'}
              type="button"
              onClick={() => setIsSidebarOpen((open) => !open)}
            >
              <PanelRight
                className={isSidebarOpen ? 'panel-toggle-icon' : 'panel-toggle-icon collapsed'}
                size={18}
                strokeWidth={ICON_STROKE}
              />
            </button>
            <div>
              <h2>{chatTitle}</h2>
            </div>
            {activeSearchResults.length > 0 && (
              <button
                aria-label={areSourcesVisible ? 'Hide sources' : 'Show sources'}
                className="sources-toggle has-tooltip tooltip-align-left"
                data-tooltip={areSourcesVisible ? 'Hide sources' : 'Show sources'}
                type="button"
                onClick={() => setAreSourcesVisible((visible) => !visible)}
              >
                <FileText size={14} strokeWidth={ICON_STROKE} />
                {activeSearchResults.length}
              </button>
            )}
          </div>
        </header>

        <div className="messages">
          {messages.length === 0 ? (
            <div className="empty-state" aria-label="No messages" />
          ) : (
            messages.map((message, index) => {
              const key = messageKey(message, index);
              return (
              <article
                aria-label={message.role === 'user' ? 'Your message' : 'Linea response'}
                className={`message ${message.role} ${messageFeedback[key] ? 'has-feedback' : ''}`}
                key={key}
              >
                {message.role === 'assistant' && message.content === '' ? (
                  <LoadingResponse />
                ) : (
                  <>
                    <MessageContent role={message.role} content={message.content} />
                    {message.role === 'user' && (
                      <button
                        aria-label="Edit"
                        className="message-edit has-tooltip tooltip-above"
                        data-tooltip="Edit"
                        type="button"
                        onClick={() => editMessage(message)}
                      >
                        <PenLine size={14} strokeWidth={ICON_STROKE} />
                      </button>
                    )}
                    {message.role === 'assistant' && (
                      <div className="response-tools">
                        <ResponseMeta
                          prefs={uiPrefs}
                          provider={
                            message.provider ??
                            responseProviders[key] ??
                            statusProviderInfo(systemStatus?.providers.find((provider) => provider.enabled && provider.role === 'primary'))
                          }
                          sleepingProviders={systemStatus?.providers.filter((provider) => !provider.enabled) ?? []}
                        />
                        <FeedbackRow
                          selected={messageFeedback[key]}
                          onSelect={(feedback) =>
                            setMessageFeedback((current) => ({
                              ...current,
                              [key]: current[key] === feedback ? '' : feedback,
                            }))
                          }
                        />
                      </div>
                    )}
                  </>
                )}
              </article>
              );
            })
          )}
          <div ref={messageEndRef} />
        </div>

        {error && <div className="error">{error}</div>}

        <form className="composer" onSubmit={sendMessage}>
          {files.length > 0 && (
            <div className="attachments">
              {files.map((file) => (
                <span key={`${file.name}-${file.size}`}>
                  <FileText size={14} strokeWidth={ICON_STROKE} />
                  {file.name}
                </span>
              ))}
            </div>
          )}
          <div className="composer-row">
            <button
              aria-label="Attach files"
              className="icon-button has-tooltip tooltip-above"
              data-tooltip="Attach"
              type="button"
              onClick={() => fileInputRef.current?.click()}
            >
              <Paperclip size={16} strokeWidth={ICON_STROKE} />
            </button>
            <input
              ref={fileInputRef}
              hidden
              multiple
              type="file"
              accept=".txt,.md,.csv,.json,.log,image/png,image/jpeg,image/webp"
              onChange={(event) => setFiles(Array.from(event.target.files ?? []))}
            />
            <textarea
              ref={textareaRef}
              aria-label="Message"
              placeholder="Message · ⌘↵"
              rows={1}
              value={content}
              onKeyDown={handleComposerKeyDown}
              onChange={(event) => updateContent(event.target.value)}
            />
            <button
              aria-label="Send"
              className="send-button has-tooltip tooltip-above"
              data-tooltip="Send"
              disabled={isSending || !content.trim()}
              type="submit"
            >
              <ArrowUpRight size={14} strokeWidth={ICON_STROKE} />
            </button>
          </div>
        </form>
      </section>

      {showSources && <SourcesPanel results={activeSearchResults} />}
      {deleteTarget && (
        <ConfirmDialog
          title="Delete conversation"
          detail={`Delete "${deleteTarget.title}" and its messages?`}
          action="Delete"
          onCancel={() => setDeleteTarget(null)}
          onConfirm={() => void deleteConversation(deleteTarget)}
        />
      )}
    </main>
  );
}

function ConfirmDialog({
  action,
  detail,
  onCancel,
  onConfirm,
  title,
}: {
  action: string;
  detail: string;
  onCancel: () => void;
  onConfirm: () => void;
  title: string;
}) {
  useEffect(() => {
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') {
        onCancel();
      }
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [onCancel]);

  return (
    <div className="dialog-backdrop" onPointerDown={onCancel}>
      <section
        aria-modal="true"
        aria-label={title}
        className="confirm-dialog"
        role="dialog"
        onPointerDown={(event) => event.stopPropagation()}
      >
        <div className="confirm-copy">
          <strong>{title}</strong>
          <p>{detail}</p>
        </div>
        <div className="confirm-actions">
          <button className="confirm-button" type="button" onClick={onCancel}>
            Cancel
          </button>
          <button className="confirm-button danger" type="button" onClick={onConfirm}>
            {action}
          </button>
        </div>
      </section>
    </div>
  );
}

function MessageContent({ role, content }: { role: Message['role']; content: string }) {
  if (role === 'user') {
    return <p>{content}</p>;
  }

  return <MarkdownContent content={content} />;
}

function MarkdownContent({ content }: { content: string }) {
  const blocks = useMemo(() => parseMarkdownBlocks(content), [content]);

  return (
    <div className="message-content">
      {blocks.map((block, index) => {
        if (block.type === 'heading') {
          return <h3 key={index}>{renderInlineMarkdown(block.text)}</h3>;
        }
        if (block.type === 'code') {
          return <CodeBlock code={block.text} language={block.language} key={index} />;
        }
        if (block.type === 'list') {
          return (
            <ul key={index}>
              {block.items.map((item, itemIndex) => (
                <li key={itemIndex}>{renderInlineMarkdown(item)}</li>
              ))}
            </ul>
          );
        }
        return <p key={index}>{renderInlineMarkdown(block.text)}</p>;
      })}
    </div>
  );
}

function CodeBlock({ code, language }: { code: string; language?: string }) {
  const [isPreviewOpen, setIsPreviewOpen] = useState(false);
  const canPreview = isPreviewableHTML(code, language);
  const lines = useMemo(() => code.split('\n'), [code]);
  const byteCount = useMemo(() => new TextEncoder().encode(code).length, [code]);
  const meta = codeMeta(language, lines.length, byteCount);

  return (
    <div className="code-shell">
      <div className="code-top">
        <span>{meta}</span>
        {canPreview && (
          <button
            aria-label={isPreviewOpen ? 'Hide preview' : 'Preview code'}
            className="code-action has-tooltip tooltip-above"
            data-tooltip={isPreviewOpen ? 'Hide preview' : 'Preview'}
            type="button"
            onClick={() => setIsPreviewOpen((open) => !open)}
          >
            <Eye size={13} strokeWidth={ICON_STROKE} />
          </button>
        )}
      </div>
      <pre className="code-block" data-language={language || undefined}>
        <code>
          {lines.map((line, index) => (
            <span className="code-line" key={index}>
              <span className="code-line-number">{index + 1}</span>
              <span className="code-line-text">{highlightCode(line || ' ', language)}</span>
            </span>
          ))}
        </code>
      </pre>
      {canPreview && isPreviewOpen && (
        <div className="code-preview-shell">
          <iframe
            className="code-preview"
            sandbox="allow-scripts"
            srcDoc={code}
            title="Code preview"
          />
        </div>
      )}
    </div>
  );
}

function codeMeta(language: string | undefined, lineCount: number, byteCount: number) {
  const parts = [];
  if (language) {
    parts.push(language);
  }
  parts.push(`${lineCount} ${lineCount === 1 ? 'line' : 'lines'}`);
  parts.push(`${byteCount} ${byteCount === 1 ? 'byte' : 'bytes'}`);
  return parts.join(' · ');
}

type MarkdownBlock =
  | { type: 'heading'; text: string }
  | { type: 'code'; text: string; language?: string }
  | { type: 'paragraph'; text: string }
  | { type: 'list'; items: string[] };

function parseMarkdownBlocks(content: string): MarkdownBlock[] {
  const blocks: MarkdownBlock[] = [];
  const paragraph: string[] = [];
  let list: string[] = [];
  let code: string[] = [];
  let codeLanguage = '';
  let isCodeBlock = false;

  const flushParagraph = () => {
    if (paragraph.length === 0) {
      return;
    }
    blocks.push({ type: 'paragraph', text: paragraph.join(' ') });
    paragraph.length = 0;
  };

  const flushList = () => {
    if (list.length === 0) {
      return;
    }
    blocks.push({ type: 'list', items: list });
    list = [];
  };

  for (const rawLine of content.split(/\r?\n/)) {
    if (rawLine.trim().startsWith('```')) {
      flushParagraph();
      flushList();
      if (isCodeBlock) {
        blocks.push({ type: 'code', language: codeLanguage, text: code.join('\n') });
        code = [];
        codeLanguage = '';
        isCodeBlock = false;
      } else {
        codeLanguage = rawLine.trim().slice(3).trim();
        isCodeBlock = true;
      }
      continue;
    }

    if (isCodeBlock) {
      code.push(rawLine);
      continue;
    }

    const line = rawLine.trim();
    if (!line) {
      flushParagraph();
      flushList();
      continue;
    }

    const heading = line.match(/^(#{1,3})\s+(.+)$/) ?? line.match(/^\*\*(.+)\*\*$/);
    if (heading) {
      flushParagraph();
      flushList();
      blocks.push({ type: 'heading', text: cleanMarkdownText(heading[2] ?? heading[1]) });
      continue;
    }

    const listItem = line.match(/^[-*]\s+(.+)$/);
    if (listItem) {
      flushParagraph();
      list.push(cleanMarkdownText(listItem[1]));
      continue;
    }

    flushList();
    paragraph.push(cleanMarkdownText(line));
  }

  flushParagraph();
  flushList();
  if (isCodeBlock && code.length > 0) {
    blocks.push({ type: 'code', language: codeLanguage, text: code.join('\n') });
  }

  return blocks.length > 0 ? blocks : [{ type: 'paragraph', text: content }];
}

function cleanMarkdownText(value: string): string {
  return value
    .replace(/\s+/g, ' ')
    .replace(/【([^】(]+)\s+\((https?:\/\/[^)]+)\)】/g, '[$1]($2)')
    .replace(/【([^】]+)】/g, '$1')
    .trim();
}

function renderInlineMarkdown(text: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  const pattern = /(\*\*[^*]+\*\*|\[([^\]]+)\]\((https?:\/\/[^)]+)\)|(https?:\/\/[^\s)]+))/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      nodes.push(text.slice(lastIndex, match.index));
    }

    const token = match[0];
    if (token.startsWith('**')) {
      nodes.push(<strong key={`${match.index}-strong`}>{token.slice(2, -2)}</strong>);
    } else {
      const label = match[2] ?? tidyUrlLabel(token);
      const href = match[3] ?? token;
      nodes.push(
        <a key={`${match.index}-link`} href={href} target="_blank" rel="noreferrer">
          {label}
        </a>,
      );
    }
    lastIndex = pattern.lastIndex;
  }

  if (lastIndex < text.length) {
    nodes.push(text.slice(lastIndex));
  }

  return nodes;
}

function highlightCode(code: string, language?: string): React.ReactNode[] {
  const normalized = (language ?? '').toLowerCase();
  if (normalized.includes('html') || code.trimStart().startsWith('<')) {
    return highlightHTML(code);
  }
  return highlightGenericCode(code);
}

function isPreviewableHTML(code: string, language?: string) {
  const normalized = (language ?? '').toLowerCase();
  const trimmed = code.trimStart().toLowerCase();
  return normalized.includes('html') || trimmed.startsWith('<!doctype html') || trimmed.startsWith('<html');
}

function highlightHTML(code: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  const pattern = /(<!--[\s\S]*?-->|<\/?[A-Za-z][^>\n]*?>)/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(code)) !== null) {
    if (match.index > lastIndex) {
      nodes.push(...highlightGenericCode(code.slice(lastIndex, match.index), `${match.index}-text`));
    }
    const token = match[0];
    nodes.push(
      <span className={token.startsWith('<!--') ? 'code-token comment' : 'code-token tag'} key={`${match.index}-tag`}>
        {token}
      </span>,
    );
    lastIndex = pattern.lastIndex;
  }

  if (lastIndex < code.length) {
    nodes.push(...highlightGenericCode(code.slice(lastIndex), `${lastIndex}-tail`));
  }

  return nodes;
}

function highlightGenericCode(code: string, prefix = 'code'): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  const pattern = /("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|\b(?:const|let|var|function|return|if|else|for|while|class|new|true|false|null|document|addEventListener)\b|#[0-9a-fA-F]{3,8}\b|\b\d+(?:\.\d+)?(?:px|rem|em|%)?\b)/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(code)) !== null) {
    if (match.index > lastIndex) {
      nodes.push(code.slice(lastIndex, match.index));
    }
    const token = match[0];
    nodes.push(
      <span className={`code-token ${codeTokenClass(token)}`} key={`${prefix}-${match.index}`}>
        {token}
      </span>,
    );
    lastIndex = pattern.lastIndex;
  }

  if (lastIndex < code.length) {
    nodes.push(code.slice(lastIndex));
  }

  return nodes;
}

function codeTokenClass(token: string) {
  if (/^["'`]/.test(token)) {
    return 'string';
  }
  if (/^#/.test(token)) {
    return 'color';
  }
  if (/^\d/.test(token)) {
    return 'number';
  }
  return 'keyword';
}

function tidyUrlLabel(url: string): string {
  try {
    const parsed = new URL(url);
    return parsed.hostname.replace(/^www\./, '');
  } catch {
    return url;
  }
}

function SystemPanel({ status }: { status: SystemStatus | null }) {
  const primary = status?.providers.find((provider) => provider.role === 'primary');
  const enabledProviders = status?.providers.filter((provider) => provider.enabled) ?? [];
  const disabledProviders = status?.providers.filter((provider) => !provider.enabled) ?? [];

  return (
    <div className="system-panel" role="status" aria-label="System status">
      <SystemRow Icon={Check} label="Tuned" value={primary?.model ?? 'Model ready'} />
      <SystemRow Icon={Database} label="Synced" value={status?.storage ?? 'Storage'} />
      <SystemRow Icon={SearchIcon} label="Search" value={status?.search ?? 'Ready'} />
      <SystemRow Icon={Eye} label="Vision" value={primary?.name === 'Gemini' ? 'Gemini' : 'Off'} />
      <SystemRow
        Icon={Cloud}
        label="Cloud"
        value={enabledProviders.filter((provider) => provider.role !== 'local').map((provider) => provider.name).join(', ') || 'Off'}
      />
      <SystemRow
        Icon={Cpu}
        label="Local"
        value={enabledProviders.find((provider) => provider.role === 'local')?.model ?? 'Off'}
      />
      {disabledProviders.length > 0 && (
        <div className="system-muted">
          {disabledProviders.map((provider) => provider.name).join(', ')} off
        </div>
      )}
    </div>
  );
}

function ThemePanel({
  prefs,
  systemTheme,
  onChange,
}: {
  prefs: UIPrefs;
  systemTheme: ResolvedTheme;
  onChange: React.Dispatch<React.SetStateAction<UIPrefs>>;
}) {
  return (
    <div className="theme-panel" aria-label="Theme settings">
      <button
        aria-label={`System theme, currently ${systemTheme}`}
        className={prefs.theme === 'system' ? 'active' : ''}
        type="button"
        onClick={() => onChange((current) => ({ ...current, theme: 'system' }))}
      >
        <Monitor size={13} strokeWidth={ICON_STROKE} />
        System
      </button>
      <button
        className={prefs.theme === 'dark' ? 'active' : ''}
        type="button"
        onClick={() => onChange((current) => ({ ...current, theme: 'dark' }))}
      >
        <Moon size={13} strokeWidth={ICON_STROKE} />
        Dark
      </button>
      <button
        className={prefs.theme === 'light' ? 'active' : ''}
        type="button"
        onClick={() => onChange((current) => ({ ...current, theme: 'light' }))}
      >
        <Sun size={13} strokeWidth={ICON_STROKE} />
        White
      </button>
      <label>
        <input
          checked={prefs.showModelBadge}
          type="checkbox"
          onChange={(event) => onChange((current) => ({ ...current, showModelBadge: event.target.checked }))}
        />
        Model badge
      </label>
      <label>
        <input
          checked={prefs.showSleepAlert}
          type="checkbox"
          onChange={(event) => onChange((current) => ({ ...current, showSleepAlert: event.target.checked }))}
        />
        Sleeping alert
      </label>
    </div>
  );
}

function SystemRow({
  Icon,
  label,
  value,
}: {
  Icon: React.ComponentType<{ size?: number; strokeWidth?: number }>;
  label: string;
  value: string;
}) {
  return (
    <div className="system-row">
      <Icon size={13} strokeWidth={ICON_STROKE} />
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function ResponseMeta({
  prefs,
  provider,
  sleepingProviders,
}: {
  prefs: UIPrefs;
  provider?: ProviderInfo;
  sleepingProviders: ProviderStatus[];
}) {
  const sleepingNames = sleepingProviders.map((item) => item.name).join(', ');
  if (!prefs.showModelBadge && (!prefs.showSleepAlert || sleepingProviders.length === 0)) {
    return null;
  }
  return (
    <div className="response-meta">
      {prefs.showModelBadge && provider && (
        <span className="model-badge has-tooltip tooltip-above" data-tooltip={`${provider.name} ${provider.model}`}>
          <BadgeHelp size={13} strokeWidth={ICON_STROKE} />
          {provider.model}
        </span>
      )}
      {prefs.showSleepAlert && sleepingProviders.length > 0 && (
        <span className="sleep-alert has-tooltip tooltip-above" data-tooltip={`${sleepingNames} sleeping`}>
          <BellOff size={13} strokeWidth={ICON_STROKE} />
        </span>
      )}
    </div>
  );
}

function resizeComposer(textarea: HTMLTextAreaElement | null) {
  if (!textarea) {
    return;
  }
  textarea.style.height = '0px';
  textarea.style.height = `${textarea.scrollHeight}px`;
}

type ConversationRowProps = {
  activeId: string | null;
  conversation: Conversation;
  isPinned: boolean;
  menuOpen: boolean;
  renaming: boolean;
  renameTitle: string;
  onDelete: (conversation: Conversation) => void;
  onMenuChange: React.Dispatch<React.SetStateAction<string | null>>;
  onRename: (conversation: Conversation) => Promise<void>;
  onRenameCancel: () => void;
  onRenameStart: (conversation: Conversation) => void;
  onRenameTitleChange: React.Dispatch<React.SetStateAction<string>>;
  onSelect: (conversationId: string) => void;
  onShare: (conversation: Conversation) => Promise<void>;
  onTogglePinned: (conversation: Conversation) => void;
};

function ConversationRow({
  activeId,
  conversation,
  isPinned,
  menuOpen,
  renaming,
  renameTitle,
  onDelete,
  onMenuChange,
  onRename,
  onRenameCancel,
  onRenameStart,
  onRenameTitleChange,
  onSelect,
  onShare,
  onTogglePinned,
}: ConversationRowProps) {
  return (
    <div className={conversation.id === activeId ? 'conversation active' : 'conversation'}>
      {renaming ? (
        <input
          aria-label="Conversation name"
          autoFocus
          className="conversation-rename"
          value={renameTitle}
          onBlur={() => void onRename(conversation)}
          onChange={(event) => onRenameTitleChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.currentTarget.blur();
            }
            if (event.key === 'Escape') {
              event.preventDefault();
              onRenameTitleChange('');
              onRenameCancel();
            }
          }}
        />
      ) : (
        <button className="conversation-select" type="button" onClick={() => onSelect(conversation.id)}>
          <span>
            {isPinned && <Bookmark size={PIN_ICON_SIZE} strokeWidth={ICON_STROKE} />}
            {conversation.title}
          </span>
          <time>{formatDate(conversation.updatedAt)}</time>
        </button>
      )}
      <button
        aria-label={`Conversation actions for ${conversation.title}`}
        className="conversation-menu-button"
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          onMenuChange((current) => (current === conversation.id ? null : conversation.id));
        }}
      >
        <MoreHorizontal size={14} strokeWidth={ICON_STROKE} />
      </button>
      {menuOpen && (
        <div className="conversation-menu" onClick={(event) => event.stopPropagation()}>
          <button type="button" onClick={() => onRenameStart(conversation)}>
            <Pencil size={14} strokeWidth={ICON_STROKE} />
            Rename
          </button>
          <button type="button" onClick={() => onTogglePinned(conversation)}>
            {isPinned ? (
              <PinOff size={PIN_ICON_SIZE} strokeWidth={ICON_STROKE} />
            ) : (
              <Pin size={PIN_ICON_SIZE} strokeWidth={ICON_STROKE} />
            )}
            {isPinned ? 'Unpin' : 'Pin'}
          </button>
          <button type="button" onClick={() => void onShare(conversation)}>
            <Share2 size={14} strokeWidth={ICON_STROKE} />
            Share
          </button>
          <button
            className="danger"
            type="button"
            onClick={() => {
              onMenuChange(null);
              void onDelete(conversation);
            }}
          >
            <Trash2 size={14} strokeWidth={ICON_STROKE} />
            Delete
          </button>
        </div>
      )}
    </div>
  );
}

function FeedbackRow({
  selected,
  onSelect,
}: {
  selected?: string;
  onSelect: (feedback: string) => void;
}) {
  return (
    <div className="feedback-row" aria-label="Response feedback">
      {FEEDBACK_OPTIONS.map(({ id, label, Icon }) => (
        <button
          aria-label={label}
          className={`feedback-button has-tooltip tooltip-above ${selected === id ? 'active' : ''}`}
          data-tooltip={label}
          key={id}
          type="button"
          onClick={() => onSelect(id)}
        >
          <Icon size={14} strokeWidth={ICON_STROKE} />
        </button>
      ))}
    </div>
  );
}

function LoadingResponse() {
  return (
    <div className="response-loading" aria-label="Generating response">
      <div className="loading-mark">
        <Route size={18} strokeWidth={ICON_STROKE} />
      </div>
      <div className="loading-lines">
        <span />
        <span />
        <span />
      </div>
    </div>
  );
}

function messageKey(message: Message, index: number) {
  return message.clientId ?? message.id ?? `${message.role}-${message.createdAt ?? 'draft'}-${index}`;
}

function statusProviderInfo(provider?: ProviderStatus): ProviderInfo | undefined {
  if (!provider?.model) {
    return undefined;
  }
  return { name: provider.name, model: provider.model };
}

function SourcesPanel({ results }: { results: SearchResult[] }) {
  return (
    <aside className="sources-panel" aria-label="Sources">
      <div className="sources-heading">Sources</div>
      <div className="sources-list">
        {results.map((result) => (
          <a className="source-card" href={result.URL} key={`${result.Title}-${result.URL}`} target="_blank" rel="noreferrer">
            <span>{result.Title || hostFromURL(result.URL) || 'Source'}</span>
            {result.URL && <small>{hostFromURL(result.URL)}</small>}
            {result.Snippet && <p>{result.Snippet}</p>}
          </a>
        ))}
      </div>
    </aside>
  );
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, init);
  const body = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(body?.error ?? `Request failed with ${response.status}`);
  }
  return body as T;
}

async function streamMessage(
  path: string,
  body: FormData,
  handlers: {
    onUser: (message: Message) => void;
    onSearch: (result: SearchResult) => void;
    onProvider: (provider: ProviderInfo) => void;
    onChunk: (chunk: StreamChunk) => void;
    onDone: (message: Message) => void;
  },
) {
  const response = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    body,
  });
  if (!response.ok || !response.body) {
    const payload = await response.json().catch(() => null);
    throw new Error(payload?.error ?? `Request failed with ${response.status}`);
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const events = buffer.split('\n\n');
    buffer = events.pop() ?? '';
    for (const event of events) {
      handleStreamEvent(event, handlers);
    }
  }
  if (buffer.trim()) {
    handleStreamEvent(buffer, handlers);
  }
}

function handleStreamEvent(
  raw: string,
  handlers: {
    onUser: (message: Message) => void;
    onSearch: (result: SearchResult) => void;
    onProvider: (provider: ProviderInfo) => void;
    onChunk: (chunk: StreamChunk) => void;
    onDone: (message: Message) => void;
  },
) {
  const event = raw
    .split('\n')
    .find((line) => line.startsWith('event:'))
    ?.slice('event:'.length)
    .trim();
  const data = raw
    .split('\n')
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice('data:'.length).trim())
    .join('\n');
  if (!event || !data) {
    return;
  }
  const payload = JSON.parse(data);
  if (event === 'user') {
    handlers.onUser(payload as Message);
  }
  if (event === 'search') {
    handlers.onSearch(payload as SearchResult);
  }
  if (event === 'provider') {
    handlers.onProvider(payload as ProviderInfo);
  }
  if (event === 'chunk') {
    handlers.onChunk({ content: payload.content ?? '', provider: payload.provider });
  }
  if (event === 'done') {
    handlers.onDone(payload as Message);
  }
  if (event === 'error') {
    throw new Error(payload.error ?? 'Streaming failed.');
  }
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
  }).format(new Date(value));
}

function loadPinnedConversationIds() {
  try {
    const value = window.localStorage.getItem(PINNED_STORAGE_KEY);
    return new Set<string>(value ? JSON.parse(value) : []);
  } catch {
    return new Set<string>();
  }
}

function savePinnedConversationIds(ids: Set<string>) {
  window.localStorage.setItem(PINNED_STORAGE_KEY, JSON.stringify([...ids]));
}

function loadUIPrefs(): UIPrefs {
  try {
    const value = window.localStorage.getItem(UI_PREFS_STORAGE_KEY);
    const parsed = value ? JSON.parse(value) : {};
    return {
      showModelBadge: true,
      showSleepAlert: true,
      ...parsed,
      theme: isThemeChoice(parsed.theme) ? parsed.theme : 'system',
    };
  } catch {
    return { showModelBadge: true, showSleepAlert: true, theme: 'system' };
  }
}

function saveUIPrefs(prefs: UIPrefs) {
  window.localStorage.setItem(UI_PREFS_STORAGE_KEY, JSON.stringify(prefs));
}

function isThemeChoice(value: unknown): value is ThemeChoice {
  return value === 'dark' || value === 'light' || value === 'system';
}

function getSystemTheme(): ResolvedTheme {
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

function isNarrowViewport() {
  return window.matchMedia('(max-width: 820px)').matches;
}

function resolveTheme(theme: ThemeChoice, systemTheme: ResolvedTheme): ResolvedTheme {
  return theme === 'system' ? systemTheme : theme;
}

function loadSearchResults() {
  try {
    const value = window.localStorage.getItem(SEARCH_RESULTS_STORAGE_KEY);
    return value ? (JSON.parse(value) as Record<string, SearchResult[]>) : {};
  } catch {
    return {};
  }
}

function saveSearchResults(results: Record<string, SearchResult[]>) {
  window.localStorage.setItem(SEARCH_RESULTS_STORAGE_KEY, JSON.stringify(results));
}

function hostFromURL(value: string) {
  try {
    return new URL(value).hostname.replace(/^www\./, '');
  } catch {
    return '';
  }
}

function formatConversationShare(conversation: Conversation, messages: Message[]) {
  const body = messages
    .map((message) => `${message.role === 'user' ? 'User' : 'Linea'}: ${message.content}`)
    .join('\n\n');
  return `${conversation.title}\n\n${body}`.trim();
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
