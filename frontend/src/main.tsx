import React, { FormEvent, KeyboardEvent as ReactKeyboardEvent, useEffect, useMemo, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  ArrowUpRight,
  ArrowDown,
  ArrowUp,
  BadgeHelp,
  BellOff,
  Bookmark,
  Brush,
  Check,
  Copy,
  Crown,
  Cpu,
  Database,
  Eye,
  FileText,
  Handshake,
  Heart,
  Info,
  Layers,
  ListChecks,
  Mic,
  MicOff,
  Moon,
  MoreHorizontal,
  Monitor,
  Palette,
  PanelRight,
  Paperclip,
  Plug,
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
  X,
} from 'lucide-react';
import './styles.css';

const API_BASE = import.meta.env.VITE_API_BASE ?? '';
const ICON_STROKE = 1.5;
const PIN_ICON_SIZE = 13;
const PINNED_STORAGE_KEY = 'linea:pinned-conversations';
const SEARCH_RESULTS_STORAGE_KEY = 'linea:search-results';
const UI_PREFS_STORAGE_KEY = 'linea:ui-prefs';
const ACCEPTED_ATTACHMENT_EXTENSIONS = new Set(['txt', 'md', 'csv', 'json', 'log', 'png', 'jpg', 'jpeg', 'webp']);
const ACCEPTED_ATTACHMENT_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp']);
const ATTACHMENT_ACCEPT = '.txt,.md,.csv,.json,.log,image/png,image/jpeg,image/webp';
const AGENT_ACTIVITY_LIMIT = 12;

function detectPlatform(): string {
  const ua = navigator.userAgent;
  const standalone = window.matchMedia('(display-mode: standalone)').matches;

  if (/iPad|iPhone|iPod/.test(ua)) {
    return standalone ? 'iOS App' : 'iOS Web';
  }
  if (/Android/.test(ua)) {
    return standalone ? 'Android App' : 'Android Web';
  }
  if (/Mac/.test(ua)) {
    return standalone ? 'macOS App' : 'macOS Web';
  }
  if (/Windows/.test(ua)) return 'Windows';
  if (/Linux/.test(ua)) return 'Linux';
  return 'Unknown';
}

function reportUrl(version: string): string {
  const platform = detectPlatform();
  const body = `**Platform:** ${platform}
**Version:** ${version || '...'}

**Do you want to fix it yourself?**

**What happened:**
`;
  return `https://github.com/bniladridas/linea/issues/new?template=bug-report.md&body=${encodeURIComponent(body)}`;
}

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

type User = {
  id: string;
  email: string;
  name: string;
  createdAt: string;
  updatedAt: string;
};

type SystemStatus = {
  storage: string;
  search: string;
  providers: ProviderStatus[];
  mcpStatus?: string;
  mcpConfigured: boolean;
  agentState?: string;
};

type AgentStatus = {
  mode: string;
  workspaceRoot?: string;
  rules: {
    source: string;
    loaded: boolean;
    summary: string[];
  };
  tools: Array<{
    id: string;
    name: string;
    access: string;
    approval: string;
  }>;
  hooks: Array<{
    id: string;
    event: string;
    state: string;
  }>;
  skills: Array<{
    id: string;
    name: string;
    state: string;
    command?: string;
  }>;
  subagents: Array<{
    id: string;
    name: string;
    purpose: string;
    state: string;
    tools: string[];
  }>;
  boundaries: string[];
  next: string[];
  traceEvents: Array<{
    id: string;
    event: string;
    state: string;
    detail?: string;
    createdAt: string;
  }>;
  mcpServers?: Array<{
    id: string;
    name: string;
    state: string;
    command?: string;
  }>;
  mcpTools?: Array<{
    id: string;
    name: string;
    serverId: string;
    serverName?: string;
    description?: string;
    inputSchema?: string;
    state?: string;
  }>;
  mcpResources?: Array<{
    id: string;
    uri: string;
    name: string;
    serverId: string;
    serverName?: string;
    description?: string;
    mimeType?: string;
    state?: string;
  }>;
  mcpPrompts?: Array<{
    id: string;
    name: string;
    serverId: string;
    serverName?: string;
    description?: string;
    state?: string;
  }>;
  mcpCalls?: Array<{
    id: string;
    toolId: string;
    serverId: string;
    name: string;
    state: string;
    output?: string;
    error?: string;
    truncated: boolean;
    createdAt: string;
  }>;
  mcpSubscriptions?: Array<{
    id: string;
    serverId: string;
    serverName: string;
    resourceId?: string;
    uri: string;
    state: string;
    error?: string;
    createdAt: string;
    updatedAt: string;
  }>;
  mcpEvents?: Array<{
    id: string;
    subscriptionId?: string;
    serverId: string;
    uri?: string;
    method: string;
    output?: string;
    error?: string;
    createdAt: string;
  }>;
  runSummary?: {
    state: string;
    traceEvents: number;
    hookRuns: number;
    skillRuns: number;
    subagentRuns?: number;
    agentLoops?: number;
    mcpCalls?: number;
    commandApprovals: number;
    commandChecks: number;
    commandRuns: number;
    editProposals: number;
  };
  commandChecks?: Array<{
    id: string;
    command: string;
    allowed: boolean;
    reason: string;
    createdAt?: string;
  }>;
  commandApprovals?: Array<{
    id: string;
    command: string;
    state: string;
    detail?: string;
    createdAt: string;
  }>;
  commandRuns?: Array<{
    id: string;
    command: string;
    exitCode: number;
    output: string;
    truncated: boolean;
    createdAt: string;
  }>;
  hookRuns?: Array<{
    id: string;
    hookId: string;
    state: string;
    detail?: string;
    createdAt: string;
  }>;
  skillRuns?: Array<{
    id: string;
    skillId: string;
    state: string;
    detail?: string;
    createdAt: string;
  }>;
  subagentRuns?: Array<{
    id: string;
    subagentId: string;
    state: string;
    summary: string;
    createdAt: string;
  }>;
  agentLoops?: Array<{
    id: string;
    goal: string;
    mode?: AgentLoopMode;
    maxIterations?: number;
    state: string;
    workspaceRoot?: string;
    previewUrl?: string;
    summary: string;
    createdAt: string;
    updatedAt: string;
    steps: Array<{
      id: string;
      kind: string;
      title: string;
      state: string;
      detail?: string;
      toolId?: string;
      command?: string;
      createdId?: string;
    }>;
  }>;
  unrestricted?: boolean;
  backgroundJobs?: BackgroundJob[];
};

type BackgroundJob = {
  id: string;
  loopId: string;
  goal: string;
  state: string;
  summary: string;
  createdAt: string;
  updatedAt: string;
};

type AgentRun = {
  id: string;
  state: string;
  createdAt: string;
};

type AgentDiagnostic = {
  path: string;
  line: number;
  column: number;
  severity: string;
  message: string;
};

type AgentWorkspaceSymbol = {
  name: string;
  kind: string;
  path: string;
  line: number;
};

type AgentWorkspaceReference = {
  name: string;
  path: string;
  line: number;
  text: string;
};

type AgentWorkspaceSearchResult = {
  path: string;
  line: number;
  text: string;
};

type AgentWorkspaceFile = {
  path: string;
  content: string;
  size: number;
  truncated: boolean;
};

type AgentEditProposal = {
  id: string;
  path: string;
  summary?: string;
  status: string;
  reviewDetail?: string;
  diff: AgentDiffLine[];
  createdAt: string;
  reviewedAt?: string;
  appliedAt?: string;
};

type AgentDiffLine = {
  type: 'equal' | 'add' | 'remove';
  oldLine?: number;
  newLine?: number;
  text: string;
};

type AgentActivityState = 'running' | 'completed' | 'failed' | 'approved' | 'rejected' | 'blocked' | 'waiting';

type AgentActivity = {
  id: string;
  kind: string;
  label: string;
  state: AgentActivityState;
  params?: string;
  result?: string;
  resultDetail?: string;
  createdAt: string;
};

type AgentLoopRequest = {
  goal: string;
  mode?: AgentLoopMode;
  maxIterations?: number;
  autoApply?: boolean;
  command?: string;
  query?: string;
  filePath?: string;
  proposalPath?: string;
  proposalContent?: string;
};

type AgentLoopMode = 'guided' | 'auto' | 'developer';

type ProviderStatus = {
  name: string;
  model?: string;
  enabled: boolean;
  role: string;
  state?: string;
  message?: string;
  detail?: string;
};

type ProviderInfo = {
  name: string;
  model: string;
};

type AppSettings = {
  providers: ProviderSetting[];
};

type ProviderSetting = {
  id: string;
  name: string;
  model?: string;
  role: string;
  enabled: boolean;
  configured: boolean;
};

type StreamChunk = {
  content: string;
  provider?: ProviderInfo;
};

type UIPrefs = {
  showResponseDetails: boolean;
  showComposerShimmer: boolean;
  showScrollCue: boolean;
  showHeaderShadow: boolean;
  theme: ThemeChoice;
  visualStyle: VisualStyle;
};

type ThemeChoice = 'dark' | 'light' | 'system';
type ResolvedTheme = 'dark' | 'light';
type VisualStyle = 'modern' | 'classic';

const FEEDBACK_OPTIONS = [
  { id: 'handshake', label: 'Useful', Icon: Handshake },
  { id: 'heart', label: 'Loved', Icon: Heart },
  { id: 'crown', label: 'Best', Icon: Crown },
  { id: 'smile', label: 'Clear', Icon: Smile },
  { id: 'party', label: 'Great', Icon: PartyPopper },
] as const;

function useVoiceInput(onTranscript: (text: string) => void) {
  const [listening, setListening] = useState(false);
  const recognitionRef = useRef<any>(null);

  function toggle() {
    const SR = (window as any).SpeechRecognition ?? (window as any).webkitSpeechRecognition;
    if (!SR) return;

    if (listening) {
      recognitionRef.current?.stop();
      return;
    }

    const r: any = new SR();
    r.lang = 'en-US';
    r.interimResults = false;
    r.onresult = (e: SpeechRecognitionEvent) => {
      const text = e.results[e.results.length - 1][0].transcript;
      onTranscript(text);
    };
    r.onend = () => setListening(false);
    r.onerror = () => setListening(false);
    recognitionRef.current = r;
    r.start();
    setListening(true);
  }

  return { listening, toggle };
}

function App() {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [content, setContent] = useState('');
  const { listening: voiceListening, toggle: toggleVoice } = useVoiceInput((text) =>
    setContent((prev) => (prev ? prev + ' ' + text : text)),
  );
  const [draftContent, setDraftContent] = useState('');
  const [chatMode, setChatMode] = useState<'saved' | 'temporary'>('saved');
  const [temporaryTitle, setTemporaryTitle] = useState('Untitled');
  const [files, setFiles] = useState<File[]>([]);
  const [isDraggingFiles, setIsDraggingFiles] = useState(false);
  const [isSending, setIsSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isSidebarOpen, setIsSidebarOpen] = useState(() => !isNarrowViewport());
  const [isSystemPanelOpen, setIsSystemPanelOpen] = useState(false);
  const [isConnectionsPanelOpen, setIsConnectionsPanelOpen] = useState(false);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [agentStatus, setAgentStatus] = useState<AgentStatus | null>(null);
  const [agentRuns, setAgentRuns] = useState<AgentRun[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [agentDiagnostics, setAgentDiagnostics] = useState<AgentDiagnostic[]>([]);
  const [agentEditProposals, setAgentEditProposals] = useState<AgentEditProposal[]>([]);
  const [agentActivities, setAgentActivities] = useState<AgentActivity[]>([]);
  const [appSettings, setAppSettings] = useState<AppSettings | null>(null);
  const [isNewChatMenuOpen, setIsNewChatMenuOpen] = useState(false);
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
  const [connections, setConnections] = useState<Connection[] | null>(null);
  const [isSystemDetailsOpen, setIsSystemDetailsOpen] = useState(false);
  const [areTooltipsSuppressed, setAreTooltipsSuppressed] = useState(false);
  const sidebarFooterRef = useRef<HTMLDivElement | null>(null);
  const renameCancelledRef = useRef(false);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const composerRef = useRef<HTMLFormElement | null>(null);
  const messagesRef = useRef<HTMLDivElement | null>(null);
  const messageEndRef = useRef<HTMLDivElement | null>(null);
  const activeIdRef = useRef<string | null>(null);
  const chatModeRef = useRef<'saved' | 'temporary'>(chatMode);
  const autoOpenedSourcesRef = useRef<Record<string, boolean>>({});
  const [hasScrollableMessages, setHasScrollableMessages] = useState(false);
  const [isAtMessageEnd, setIsAtMessageEnd] = useState(true);
  const [isHeaderScrolled, setIsHeaderScrolled] = useState(false);
  const [composerHeight, setComposerHeight] = useState(108);
  const [autoApproveCategories, setAutoApproveCategoriesState] = useState<string[]>([]);

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
  const isTemporaryChat = chatMode === 'temporary' && !activeId;
  const chatTitle = activeConversation?.title ?? (isTemporaryChat ? temporaryTitle : 'Untitled');
  const sourceConversationId = isTemporaryChat ? 'temporary' : activeId ?? pendingSourceConversationId;
  const activeSearchResults = sourceConversationId ? (searchResultsByConversation[sourceConversationId] ?? []) : [];
  const showSources = activeSearchResults.length > 0 && areSourcesVisible;
  const shellClassName = [
    'shell',
    !isSidebarOpen ? 'sidebar-collapsed' : '',
    showSources ? 'sources-open' : '',
    (areTooltipsSuppressed || isNewChatMenuOpen || isSystemPanelOpen || isThemePanelOpen || isConnectionsPanelOpen)
      ? 'tooltips-suppressed'
      : '',
  ]
    .filter(Boolean)
    .join(' ');
  const messagesStyle: React.CSSProperties = { paddingBottom: `${composerHeight + 44}px` };
  const scrollNoteStyle: React.CSSProperties = { bottom: `${composerHeight + 14}px` };

  useEffect(() => {
    void loadConversations();
    void loadSystemStatus();
    void loadAgentStatus();
    void loadAgentRuns();
    void loadAgentEditProposals();
    void loadAppSettings();
    void loadUsers();
  }, []);

  useEffect(() => {
    const closeMenu = () => {
      setIsNewChatMenuOpen(false);
      setOpenConversationMenu(null);
    };
    window.addEventListener('click', closeMenu);
    return () => window.removeEventListener('click', closeMenu);
  }, []);

  useEffect(() => {
    if (!isSystemPanelOpen && !isThemePanelOpen && !isConnectionsPanelOpen) {
      return;
    }

    const closeFooterPanels = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Element && sidebarFooterRef.current?.contains(target)) {
        const interactiveFooterArea = target.closest('.system-panel, .theme-panel, .connections-panel, .settings-panel, .footer-actions');
        if (interactiveFooterArea && sidebarFooterRef.current.contains(interactiveFooterArea)) {
          return;
        }
      }
      setIsSystemPanelOpen(false);
      setIsThemePanelOpen(false);
      setIsConnectionsPanelOpen(false);
    };
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key !== 'Escape') {
        return;
      }
      setIsSystemPanelOpen(false);
      setIsThemePanelOpen(false);
      setIsConnectionsPanelOpen(false);
    };

    window.addEventListener('pointerdown', closeFooterPanels, true);
    window.addEventListener('keydown', closeOnEscape);
    return () => {
      window.removeEventListener('pointerdown', closeFooterPanels, true);
      window.removeEventListener('keydown', closeOnEscape);
    };
  }, [isSystemPanelOpen, isThemePanelOpen, isConnectionsPanelOpen]);

  useEffect(() => {
    if (!areTooltipsSuppressed) {
      return;
    }

    const restoreTooltips = () => setAreTooltipsSuppressed(false);
    window.addEventListener('pointermove', restoreTooltips, { once: true });
    window.addEventListener('keydown', restoreTooltips, { once: true });
    return () => {
      window.removeEventListener('pointermove', restoreTooltips);
      window.removeEventListener('keydown', restoreTooltips);
    };
  }, [areTooltipsSuppressed]);

  useEffect(() => {
    chatModeRef.current = chatMode;
    activeIdRef.current = activeId;
    if (!activeId) {
      if (chatMode !== 'temporary') {
        setMessages([]);
      }
      return;
    }
    void loadMessages(activeId);
  }, [activeId, chatMode]);

  useEffect(() => {
    messageEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    window.setTimeout(updateMessageScrollState, 80);
  }, [messages, isSending, agentActivities.length]);

  useEffect(() => {
    updateMessageScrollState();
    const node = messagesRef.current;
    if (!node) {
      return;
    }
    const observer = new ResizeObserver(updateMessageScrollState);
    observer.observe(node);
    return () => observer.disconnect();
  }, [messages.length, agentActivities.length, showSources, isSidebarOpen]);

  useEffect(() => {
    const node = composerRef.current;
    if (!node) {
      return;
    }
    const updateComposerHeight = () => {
      setComposerHeight(Math.ceil(node.getBoundingClientRect().height));
      window.setTimeout(updateMessageScrollState, 0);
    };
    updateComposerHeight();
    const observer = new ResizeObserver(updateComposerHeight);
    observer.observe(node);
    return () => observer.disconnect();
  }, [files.length, error]);

  useEffect(() => {
    resizeComposer(textareaRef.current);
  }, [content]);

  useEffect(() => {
    const resolvedTheme = resolveTheme(uiPrefs.theme, systemTheme);
    document.documentElement.dataset.theme = resolvedTheme;
    document.documentElement.style.colorScheme = resolvedTheme;
    document.documentElement.dataset.visualStyle = uiPrefs.visualStyle;
    saveUIPrefs(uiPrefs);
  }, [systemTheme, uiPrefs]);

  useEffect(() => {
    const linkId = 'linea-classic-stylesheet';
    let link = document.getElementById(linkId) as HTMLLinkElement | null;
    if (uiPrefs.visualStyle === 'classic') {
      if (!link) {
        link = document.createElement('link');
        link.id = linkId;
        link.rel = 'stylesheet';
        link.href = '/styles-classic.css';
        document.head.appendChild(link);
      }
    } else {
      if (link) {
        link.remove();
      }
    }
  }, [uiPrefs.visualStyle]);

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
    setActiveId((current) => {
      if (chatModeRef.current === 'temporary') {
        return current;
      }
      return current === null && draftContent.trim() ? null : current ?? nextConversations[0]?.id ?? null;
    });
  }

  async function loadSystemStatus() {
    try {
      const data = await request<SystemStatus>('/api/status');
      setSystemStatus(data);
    } catch {
      setSystemStatus(null);
    }
  }

  async function loadConnections() {
    try {
      const data = await request<Connection[]>('/api/oauth/providers');
      setConnections(Array.isArray(data) ? data : []);
    } catch {
      setConnections([]);
    }
  }

  async function loadUsers() {
    try {
      const data = await request<User[]>('/api/users');
      setUsers(Array.isArray(data) ? data : []);
    } catch {
      setUsers([]);
    }
  }

  async function createUser(email: string, name: string) {
    try {
      const user = await request<User>('/api/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, name }),
      });
      setUsers((items) => [...items, user]);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not create user.';
      setError(message);
    }
  }

  async function loadAgentStatus() {
    try {
      const data = await request<AgentStatus>('/api/agent');
      setAgentStatus(data);
    } catch {
      setAgentStatus(null);
    }
  }

  async function loadAgentRuns() {
    try {
      const data = await request<AgentRun[]>('/api/agent/runs');
      setAgentRuns(Array.isArray(data) ? data : []);
    } catch {
      setAgentRuns([]);
    }
  }

  async function loadAgentDiagnostics() {
    try {
      const data = await request<AgentDiagnostic[]>('/api/agent/workspace/diagnostics');
      setAgentDiagnostics(Array.isArray(data) ? data : []);
    } catch {
      setAgentDiagnostics([]);
    }
  }

  async function loadAgentEditProposals() {
    try {
      const data = await request<AgentEditProposal[]>('/api/agent/edit-proposals');
      setAgentEditProposals(Array.isArray(data) ? data : []);
    } catch {
      setAgentEditProposals([]);
    }
  }

  function recordAgentActivity(activity: Omit<AgentActivity, 'id' | 'createdAt'>) {
    const id = `agent-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    const next: AgentActivity = {
      ...activity,
      id,
      createdAt: new Date().toISOString(),
    };
    setAgentActivities((items) => [...items, next].slice(-AGENT_ACTIVITY_LIMIT));
    return id;
  }

  function updateAgentActivity(id: string, patch: Partial<Omit<AgentActivity, 'id' | 'createdAt'>>) {
    setAgentActivities((items) => items.map((item) => (item.id === id ? { ...item, ...patch } : item)));
  }

  function reconcileAgentLoopActivities(loopId: string, state: string, summary?: string) {
    if (state !== 'completed') {
      return;
    }
    setAgentActivities((items) =>
      items.map((item) =>
        item.kind === 'loop' && item.params === loopId && item.state === 'waiting'
          ? { ...item, state: 'completed', result: summary ?? 'Completed.' }
          : item,
      ),
    );
  }

  async function reviewAgentEditProposal(proposalId: string, status: 'approved' | 'rejected') {
    const currentProposal = agentEditProposals.find((proposal) => proposal.id === proposalId);
    const activityId = recordAgentActivity({
      kind: 'proposal',
      label: status === 'approved' ? 'Approve proposal' : 'Reject proposal',
      state: 'running',
      params: currentProposal?.path ?? proposalId,
    });
    try {
      const proposal = await request<AgentEditProposal>(`/api/agent/edit-proposals/${proposalId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status, detail: 'Reviewed in Linea.' }),
      });
      setAgentEditProposals((items) => items.map((item) => (item.id === proposal.id ? proposal : item)));
      await loadAgentStatus();
      updateAgentActivity(activityId, {
        state: status,
        params: proposal.path,
        result: proposal.reviewDetail ?? 'Review saved.',
      });
    } catch (reviewError) {
      const message = reviewError instanceof Error ? reviewError.message : 'Could not review proposal.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function applyAgentEditProposal(proposalId: string) {
    const currentProposal = agentEditProposals.find((proposal) => proposal.id === proposalId);
    const activityId = recordAgentActivity({
      kind: 'proposal',
      label: 'Apply proposal',
      state: 'running',
      params: currentProposal?.path ?? proposalId,
    });
    try {
      const proposal = await request<AgentEditProposal>(`/api/agent/edit-proposals/${proposalId}/apply`, {
        method: 'POST',
      });
      setAgentEditProposals((items) => items.map((item) => (item.id === proposal.id ? proposal : item)));
      const nextStatus = await refreshAgentDetails();
      updateAgentActivity(activityId, { state: 'completed', params: proposal.path, result: 'Applied to disk.' });
      if (nextStatus) {
        continueAppliedAutoLoops(nextStatus, proposal.id);
      }
    } catch (applyError) {
      const message = applyError instanceof Error ? applyError.message : 'Could not apply proposal.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function createAgentEditProposal(input: { path: string; content: string; summary?: string }) {
    const activityId = recordAgentActivity({
      kind: 'proposal',
      label: 'Create proposal',
      state: 'running',
      params: input.path,
    });
    try {
      const proposal = await request<AgentEditProposal>('/api/agent/edit-proposals', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      });
      setAgentEditProposals((items) => [proposal, ...items.filter((item) => item.id !== proposal.id)]);
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: 'completed',
        result: proposal.summary || proposal.status,
      });
    } catch (proposalError) {
      const message = proposalError instanceof Error ? proposalError.message : 'Could not create proposal.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function refreshAgentDetails() {
    const [status, mcpTools, mcpResources, mcpPrompts, categories] = await Promise.all([
      request<AgentStatus>('/api/agent')
        .then((data) => data)
        .catch(() => null),
      request<NonNullable<AgentStatus['mcpTools']>>('/api/agent/mcp-tools')
        .then((data) => (Array.isArray(data) ? data : []))
        .catch(() => []),
      request<NonNullable<AgentStatus['mcpResources']>>('/api/agent/mcp-resources')
        .then((data) => (Array.isArray(data) ? data : []))
        .catch(() => []),
      request<NonNullable<AgentStatus['mcpPrompts']>>('/api/agent/mcp-prompts')
        .then((data) => (Array.isArray(data) ? data : []))
        .catch(() => []),
      request<string[]>('/api/agent/auto-approve-categories')
        .then((data) => (Array.isArray(data) ? data : []))
        .catch(() => []),
      loadAgentRuns(),
      loadAgentEditProposals(),
    ]);
    setAgentStatus(status ? { ...status, mcpTools, mcpResources, mcpPrompts } : null);
    setAutoApproveCategoriesState(categories);
    return status;
  }

  async function checkAgentCommand(command: string) {
    const activityId = recordAgentActivity({
      kind: 'command',
      label: 'Check command',
      state: 'running',
      params: command,
    });
    try {
      const check = await request<{ allowed: boolean; reason: string }>('/api/agent/command-checks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: check.allowed ? 'completed' : 'blocked',
        result: check.reason,
      });
    } catch (commandError) {
      const message = commandError instanceof Error ? commandError.message : 'Could not check command.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function approveAgentCommand(command: string) {
    const activityId = recordAgentActivity({
      kind: 'command',
      label: 'Approve command',
      state: 'running',
      params: command,
    });
    try {
      const approval = await request<{ id: string; command: string; state: string; detail?: string }>('/api/agent/command-approvals', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command, state: 'approved', detail: 'Approved in Linea.' }),
      });
      const nextStatus = await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: approval.state === 'approved' ? 'approved' : 'completed',
        result: approval.detail ?? 'Approval saved.',
      });
      if (approval.state === 'approved' && nextStatus) {
        continueApprovedAutoLoops(nextStatus, approval.command);
      }
    } catch (approvalError) {
      const message = approvalError instanceof Error ? approvalError.message : 'Could not approve command.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function runAgentCommand(command: string, approvalId: string) {
    const activityId = recordAgentActivity({
      kind: 'command',
      label: 'Run command',
      state: 'running',
      params: command,
    });
    try {
      const run = await request<{ exitCode: number; output?: string; truncated?: boolean }>('/api/agent/command-runs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command, approvalId }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: run.exitCode === 0 ? 'completed' : 'failed',
        result: summarizeAgentResult(`exit ${run.exitCode}`, run.output, run.truncated),
      });
    } catch (runError) {
      const message = runError instanceof Error ? runError.message : 'Could not run command.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function runAgentHook(hookId: string, command: string, approvalId?: string) {
    const activityId = recordAgentActivity({
      kind: 'hook',
      label: 'Run hook',
      state: 'running',
      params: command || hookId,
    });
    try {
      const run = await request<{ state: string; detail?: string }>(`/api/agent/hooks/${hookId}/run`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command, approvalId, detail: 'Run from Linea.' }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: run.state === 'failed' ? 'failed' : 'completed',
        result: run.detail ?? run.state,
      });
    } catch (hookError) {
      const message = hookError instanceof Error ? hookError.message : 'Could not run hook.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function createAgentHookRun(input: { hookId: string; state: string; detail?: string }) {
    const activityId = recordAgentActivity({
      kind: 'hook',
      label: 'Record hook run',
      state: 'running',
      params: input.hookId,
    });
    try {
      const run = await request<{ hookId: string; state: string; detail?: string }>('/api/agent/hook-runs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: run.state === 'failed' ? 'failed' : 'completed',
        result: run.detail || run.state,
      });
    } catch (hookError) {
      const message = hookError instanceof Error ? hookError.message : 'Could not record hook run.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function runAgentSkill(skillId: string, command: string, approvalId?: string) {
    const activityId = recordAgentActivity({
      kind: 'skill',
      label: 'Run skill',
      state: 'running',
      params: command || skillId,
    });
    try {
      const run = await request<{ state: string; detail?: string }>(`/api/agent/skills/${skillId}/run`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command, approvalId, detail: 'Run from Linea.' }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: run.state === 'failed' ? 'failed' : 'completed',
        result: run.detail ?? run.state,
      });
    } catch (skillError) {
      const message = skillError instanceof Error ? skillError.message : 'Could not run skill.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function runAgentSubagent(subagentId: string, query: string) {
    const activityId = recordAgentActivity({
      kind: 'subagent',
      label: 'Run subagent',
      state: 'running',
      params: query || subagentId,
    });
    try {
      const run = await request<{ state: string; summary?: string }>(`/api/agent/subagents/${subagentId}/run`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ goal: query, query }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: run.state === 'failed' ? 'failed' : 'completed',
        result: run.summary ?? run.state,
      });
    } catch (subagentError) {
      const message = subagentError instanceof Error ? subagentError.message : 'Could not run subagent.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function callAgentMCPTool(toolId: string, args: Record<string, unknown>) {
    const activityId = recordAgentActivity({
      kind: 'mcp',
      label: 'Call tool',
      state: 'running',
      params: `${toolId} ${summarizeJSON(args)}`.trim(),
    });
    try {
      const call = await request<{ state: string; output?: string; error?: string; truncated?: boolean }>('/api/agent/mcp-calls', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ toolId, arguments: args }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: call.state === 'failed' ? 'failed' : 'completed',
        result: summarizeAgentResult(call.state, call.error || call.output, call.truncated),
        resultDetail: formatAgentActivityDetail(call.error || call.output),
      });
    } catch (mcpError) {
      const message = mcpError instanceof Error ? mcpError.message : 'Could not call MCP tool.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function readAgentMCPResource(resourceId: string) {
    const activityId = recordAgentActivity({
      kind: 'mcp',
      label: 'Read resource',
      state: 'running',
      params: resourceId,
    });
    try {
      const call = await request<{ state: string; output?: string; error?: string; truncated?: boolean }>('/api/agent/mcp-resources/read', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resourceId }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: call.state === 'failed' ? 'failed' : 'completed',
        result: summarizeAgentResult(call.state, call.error || call.output, call.truncated),
        resultDetail: formatAgentActivityDetail(call.error || call.output),
      });
    } catch (mcpError) {
      const message = mcpError instanceof Error ? mcpError.message : 'Could not read MCP resource.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function subscribeAgentMCPResource(resourceId: string) {
    const activityId = recordAgentActivity({
      kind: 'mcp',
      label: 'Subscribe resource',
      state: 'running',
      params: resourceId,
    });
    try {
      const subscription = await request<{ state: string; uri?: string; error?: string }>('/api/agent/mcp-resources/subscribe', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resourceId }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: subscription.state === 'failed' ? 'failed' : 'completed',
        result: subscription.error || subscription.uri || subscription.state,
      });
    } catch (mcpError) {
      const message = mcpError instanceof Error ? mcpError.message : 'Could not subscribe MCP resource.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function unsubscribeAgentMCPResource(subscriptionId: string) {
    const activityId = recordAgentActivity({
      kind: 'mcp',
      label: 'Unsubscribe resource',
      state: 'running',
      params: subscriptionId,
    });
    try {
      const subscription = await request<{ state: string; uri?: string; error?: string }>(
        `/api/agent/mcp-subscriptions/${encodeURIComponent(subscriptionId)}/unsubscribe`,
        { method: 'POST' },
      );
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: subscription.state === 'failed' ? 'failed' : 'completed',
        result: subscription.error || subscription.uri || subscription.state,
      });
    } catch (mcpError) {
      const message = mcpError instanceof Error ? mcpError.message : 'Could not unsubscribe MCP resource.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function getAgentMCPPrompt(promptId: string, args: Record<string, unknown>) {
    const activityId = recordAgentActivity({
      kind: 'mcp',
      label: 'Get prompt',
      state: 'running',
      params: `${promptId} ${summarizeJSON(args)}`.trim(),
    });
    try {
      const call = await request<{ state: string; output?: string; error?: string; truncated?: boolean }>('/api/agent/mcp-prompts/get', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ promptId, arguments: args }),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: call.state === 'failed' ? 'failed' : 'completed',
        result: summarizeAgentResult(call.state, call.error || call.output, call.truncated),
        resultDetail: formatAgentActivityDetail(call.error || call.output),
      });
    } catch (mcpError) {
      const message = mcpError instanceof Error ? mcpError.message : 'Could not get MCP prompt.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function saveAgentRunSnapshot() {
    try {
      await request('/api/agent/runs', { method: 'POST' });
      await refreshAgentDetails();
    } catch (runError) {
      setError(runError instanceof Error ? runError.message : 'Could not save agent run.');
    }
  }

  async function createAgentTrace(input: { event: string; state: string; detail?: string }) {
    const activityId = recordAgentActivity({
      kind: 'trace',
      label: 'Create trace',
      state: 'running',
      params: input.event,
    });
    try {
      const trace = await request<{ event: string; state: string; detail?: string }>('/api/agent/traces', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: trace.state === 'failed' ? 'failed' : 'completed',
        result: trace.detail || trace.state,
      });
    } catch (traceError) {
      const message = traceError instanceof Error ? traceError.message : 'Could not create trace.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function startAgentLoop(input: AgentLoopRequest) {
    const activityId = recordAgentActivity({
      kind: 'loop',
      label: 'Start loop',
      state: 'running',
      params: input.goal,
    });
    try {
      const loop = await request<{ state: string; summary?: string }>('/api/agent/loops', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: loopActivityState(loop.state),
        result: loop.summary ?? loop.state,
      });
    } catch (loopError) {
      const message = loopError instanceof Error ? loopError.message : 'Could not start agent loop.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function continueAgentLoop(loopId: string, input: Omit<AgentLoopRequest, 'goal'> = {}) {
    const activityId = recordAgentActivity({
      kind: 'loop',
      label: 'Continue loop',
      state: 'running',
      params: input.command || input.query || input.filePath || loopId,
    });
    try {
      const loop = await request<{ id?: string; state: string; summary?: string }>(`/api/agent/loops/${loopId}/continue`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      });
      await refreshAgentDetails();
      updateAgentActivity(activityId, {
        state: loopActivityState(loop.state),
        result: loop.summary ?? loop.state,
      });
      reconcileAgentLoopActivities(loop.id ?? loopId, loop.state, loop.summary);
    } catch (loopError) {
      const message = loopError instanceof Error ? loopError.message : 'Could not continue agent loop.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  function continueApprovedAutoLoops(status: AgentStatus, command: string) {
    const normalized = normalizeCommand(command);
    const loop = (status.agentLoops ?? []).find(
      (item) =>
        isAutonomousLoopMode(item.mode) &&
        item.state === 'waiting_approval' &&
        item.steps.some(
          (step) =>
            step.kind === 'command_approval' &&
            step.state === 'waiting_approval' &&
            normalizeCommand(step.command ?? '') === normalized,
        ),
    );
    if (loop) {
      void continueAgentLoop(loop.id);
    }
  }

  function continueAppliedAutoLoops(status: AgentStatus, proposalId: string) {
    const loop = (status.agentLoops ?? []).find(
      (item) =>
        isAutonomousLoopMode(item.mode) &&
        item.state === 'waiting_approval' &&
        item.steps.some(
          (step) =>
            step.kind === 'edit_review' &&
            step.state === 'waiting_approval' &&
            step.createdId === proposalId,
        ),
    );
    if (loop) {
      void continueAgentLoop(loop.id);
    }
  }

  async function cancelAgentLoop(loopId: string) {
    const activityId = recordAgentActivity({
      kind: 'loop',
      label: 'Cancel loop',
      state: 'running',
      params: loopId,
    });
    try {
      const loop = await request<{ state: string; summary?: string }>(`/api/agent/loops/${loopId}/cancel`, { method: 'POST' });
      await refreshAgentDetails();
      updateAgentActivity(activityId, { state: 'completed', result: loop.summary ?? loop.state });
    } catch (loopError) {
      const message = loopError instanceof Error ? loopError.message : 'Could not cancel agent loop.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function setAgentUnrestricted(unrestricted: boolean) {
    try {
      await request('/api/agent/unrestricted', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ unrestricted }),
      });
      await refreshAgentDetails();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not update unrestricted mode.');
    }
  }

  async function setAutoApproveCategories(categories: string[]) {
    try {
      await request('/api/agent/auto-approve-categories', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ categories }),
      });
      setAutoApproveCategoriesState(categories);
      await refreshAgentDetails();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not update auto-approve categories.');
    }
  }

  async function startBackgroundJob(input: { goal: string; mode?: string; maxIterations?: number; autoApply?: boolean }) {
    try {
      await request('/api/agent/background-jobs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      });
      await refreshAgentDetails();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not start background job.');
    }
  }

  async function cancelBackgroundJob(id: string) {
    try {
      await request(`/api/agent/background-jobs/${id}/cancel`, { method: 'POST' });
      await refreshAgentDetails();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not cancel background job.');
    }
  }

  async function saveAgentWorkspaceRoot(root: string) {
    const activityId = recordAgentActivity({
      kind: 'workspace',
      label: 'Set workspace',
      state: 'running',
      params: root,
    });
    try {
      await request<{ root: string }>('/api/agent/workspace', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ root }),
      });
      setAgentEditProposals([]);
      setAgentDiagnostics([]);
      await loadAgentStatus();
      updateAgentActivity(activityId, { state: 'completed', result: 'Workspace updated.' });
    } catch (workspaceError) {
      const message = workspaceError instanceof Error ? workspaceError.message : 'Could not update workspace.';
      updateAgentActivity(activityId, { state: 'failed', result: message });
      setError(message);
    }
  }

  async function loadAppSettings() {
    try {
      const data = await request<AppSettings>('/api/settings');
      setAppSettings(data);
    } catch {
      setAppSettings(null);
    }
  }

  async function saveAppSettings(next: AppSettings) {
    try {
      const data = await request<AppSettings>('/api/settings', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(next),
      });
      setAppSettings(data);
      await loadSystemStatus();
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : 'Could not save settings.');
    }
  }

  async function loadMessages(conversationId: string) {
    setError(null);
    const data = await request<Message[]>(`/api/conversations/${conversationId}/messages`);
    if (activeIdRef.current === conversationId) {
      setMessages(Array.isArray(data) ? data : []);
    }
  }

  async function createConversation(initialTitle = 'Untitled', activate = true, initialMessages: Message[] = []) {
    setError(null);
    const conversation = await request<Conversation>('/api/conversations', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: initialTitle,
        messages: initialMessages
          .filter((message) => message.role === 'user' || message.role === 'assistant')
          .map((message) => ({ role: message.role, content: message.content })),
      }),
    });
    setConversations((items) => [conversation, ...items]);
    if (activate) {
      setActiveId(conversation.id);
    }
    return conversation;
  }

  function startNewChat() {
    setChatMode('saved');
    setPendingSourceConversationId(null);
    setActiveId(null);
    setMessages([]);
    setAgentActivities([]);
    setContent(draftContent);
    setFiles([]);
    setError(null);
    window.requestAnimationFrame(() => textareaRef.current?.focus());
  }

  function startTemporaryChat() {
    if (!activeId && chatMode === 'saved') {
      setDraftContent(content);
    }
    setChatMode('temporary');
    setTemporaryTitle('Untitled');
    setPendingSourceConversationId(null);
    setActiveId(null);
    setMessages([]);
    setAgentActivities([]);
    setContent('');
    setFiles([]);
    setError(null);
    setConversationSearchResults('temporary', []);
    window.requestAnimationFrame(() => textareaRef.current?.focus());
  }

  function selectConversation(conversationId: string) {
    if (!activeId && chatMode === 'saved') {
      setDraftContent(content);
    }
    setChatMode('saved');
    setPendingSourceConversationId(null);
    setActiveId(conversationId);
    setAgentActivities([]);
    setContent('');
    setFiles([]);
    setError(null);
  }

  function updateContent(value: string) {
    setContent(value);
    if (!activeId && chatMode === 'saved') {
      setDraftContent(value);
    }
  }

  function attachFiles(nextFiles: FileList | File[]) {
    const accepted = Array.from(nextFiles).filter(isAcceptedAttachment);
    setFiles(accepted);
  }

  function handleComposerDragOver(event: React.DragEvent<HTMLFormElement>) {
    if (!hasDraggedFiles(event.dataTransfer)) {
      return;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = 'copy';
    setIsDraggingFiles(true);
  }

  function handleComposerDragLeave(event: React.DragEvent<HTMLFormElement>) {
    if (event.currentTarget.contains(event.relatedTarget as Node | null)) {
      return;
    }
    setIsDraggingFiles(false);
  }

  function handleComposerDrop(event: React.DragEvent<HTMLFormElement>) {
    if (!hasDraggedFiles(event.dataTransfer)) {
      return;
    }
    event.preventDefault();
    setIsDraggingFiles(false);
    attachFiles(event.dataTransfer.files);
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

  async function saveTemporaryChat() {
    if (!isTemporaryChat || messages.length === 0) {
      return;
    }
    try {
      setError(null);
      const title = storeTitleFromMessages(messages);
      const conversation = await createConversation(title, false, messages);
      setChatMode('saved');
      setTemporaryTitle('Untitled');
      setDraftContent('');
      setConversationSearchResults(conversation.id, searchResultsByConversation.temporary ?? []);
      setConversations((items) => {
        const seen = new Set<string>();
        return [conversation, ...items].filter((item) => {
          if (seen.has(item.id)) {
            return false;
          }
          seen.add(item.id);
          return true;
        });
      });
      setActiveId(conversation.id);
      setConversationSearchResults('temporary', []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save temporary chat.');
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
      const conversation = activeId ? conversations.find((item) => item.id === activeId) : null;
      if (!isTemporaryChat && !conversation) {
        const created = await createConversation(trimmed.slice(0, 60) || 'Untitled', false);
        submittedConversationId = created.id;
        setPendingSourceConversationId(created.id);
      } else if (conversation) {
        submittedConversationId = conversation.id;
        setPendingSourceConversationId(conversation.id);
      }

      const form = new FormData();
      form.append('content', content);
      files.forEach((file) => form.append('files', file));
      if (isTemporaryChat) {
        form.append(
          'history',
          JSON.stringify(
            messages
              .filter((message) => message.role === 'user' || message.role === 'assistant')
              .map((message) => ({ role: message.role, content: message.content })),
          ),
        );
      }

      setFiles([]);
      const assistantClientId = `response-${Date.now()}`;
      let assistantProvider: ProviderInfo | undefined;
      const draft: Message = { clientId: assistantClientId, role: 'assistant', content: '' };
      assistantDraft = draft;
      setMessages((items) => [...items.filter((item) => item !== optimisticUser), optimisticUser, draft]);
      const streamConversationId = isTemporaryChat ? 'temporary' : submittedConversationId;
      if (!streamConversationId) {
        throw new Error('No active conversation.');
      }
      autoOpenedSourcesRef.current[streamConversationId] = false;
      setConversationSearchResults(streamConversationId, []);

      await streamMessage(isTemporaryChat ? '/api/chat/temp' : `/api/conversations/${streamConversationId}/messages`, form, {
        onUser: (message) => {
          setMessages((items) => items.map((item) => (item === optimisticUser ? message : item)));
        },
        onSearch: (result) => {
          appendConversationSearchResult(streamConversationId, result);
          if (!autoOpenedSourcesRef.current[streamConversationId]) {
            autoOpenedSourcesRef.current[streamConversationId] = true;
            setAreSourcesVisible(true);
          }
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
      void loadAgentStatus();
      void loadAgentEditProposals();
      if (isTemporaryChat) {
        setTemporaryTitle((current) => (current === 'Untitled' ? storeTitleFromContent(trimmed) : current));
      } else {
        await loadConversations();
        setActiveId(streamConversationId);
        if (!activeId) {
          setDraftContent('');
        }
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

  function updateMessageScrollState() {
    const node = messagesRef.current;
    if (!node) {
      setHasScrollableMessages(false);
      setIsAtMessageEnd(true);
      setIsHeaderScrolled(false);
      return;
    }
    setHasScrollableMessages(node.scrollHeight - node.clientHeight > 8);
    setIsAtMessageEnd(node.scrollHeight - node.scrollTop - node.clientHeight < 28);
    setIsHeaderScrolled(node.scrollTop > 8);
  }

  function scrollToMessageEnd() {
    messageEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    window.setTimeout(updateMessageScrollState, 160);
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

          <div className="new-chat-group" aria-label="Create chat">
            <button
              className="new-chat"
              type="button"
              onPointerDown={() => setAreTooltipsSuppressed(true)}
              onClick={(event) => {
                event.stopPropagation();
                setOpenConversationMenu(null);
                setIsNewChatMenuOpen((open) => !open);
              }}
            >
              <Plus size={14} strokeWidth={ICON_STROKE} />
              New
            </button>
            {isNewChatMenuOpen && (
              <div className="new-chat-menu" onClick={(event) => event.stopPropagation()}>
                <button
                  type="button"
                  onClick={() => {
                    setIsNewChatMenuOpen(false);
                    startNewChat();
                  }}
                >
                  <Plus size={14} strokeWidth={ICON_STROKE} />
                  Saved
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setIsNewChatMenuOpen(false);
                    startTemporaryChat();
                  }}
                >
                  <BellOff size={14} strokeWidth={ICON_STROKE} />
                  Temporary
                </button>
              </div>
            )}
          </div>

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
              {isTemporaryChat ? (
                <div className="conversation active draft-conversation">
                  <button className="conversation-select" type="button" onClick={startTemporaryChat}>
                    <span>{temporaryTitle}</span>
                    <time>Temporary</time>
                  </button>
                </div>
              ) : (
                (!activeId || draftContent.trim()) && (
                  <div className={!activeId ? 'conversation active draft-conversation' : 'conversation draft-conversation'}>
                    <button className="conversation-select" type="button" onClick={startNewChat}>
                      <span>Untitled</span>
                      <time>Draft</time>
                    </button>
                  </div>
                )
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
            {isSystemPanelOpen && (
              <SystemPanel
                status={systemStatus}
                agentStatus={agentStatus}
                agentRuns={agentRuns}
                onOpenDetails={() => {
                  setIsSystemDetailsOpen(true);
                  setIsSystemPanelOpen(false);
                  if (!appSettings) {
                    void loadAppSettings();
                  }
                  void refreshAgentDetails();
                  void loadAgentDiagnostics();
                }}
              />
            )}
            {isThemePanelOpen && <ThemePanel prefs={uiPrefs} systemTheme={systemTheme} onChange={setUIPrefs} />}
            {isConnectionsPanelOpen && (
              <ConnectionsPanel
                connections={connections}
                onRefresh={() => {
                  void loadConnections();
                }}
                onClose={() => setIsConnectionsPanelOpen(false)}
              />
            )}
            <div className="footer-actions">
              <button
                aria-label={isSystemPanelOpen ? 'Hide system status' : 'Show system status'}
                className="system-button has-tooltip tooltip-above"
                data-tooltip={isSystemPanelOpen ? 'Hide status' : 'System status'}
                type="button"
                onPointerDown={() => setAreTooltipsSuppressed(true)}
                onClick={() => {
                  setIsSystemPanelOpen((open) => !open);
                  setIsThemePanelOpen(false);
                  if (!systemStatus) {
                    void loadSystemStatus();
                  }
                  if (!agentStatus) {
                    void loadAgentStatus();
                  }
                  void loadAgentRuns();
                  void loadAgentEditProposals();
                }}
              >
                <Info size={14} strokeWidth={ICON_STROKE} />
              </button>
              <button
                aria-label={isThemePanelOpen ? 'Hide theme' : 'Choose theme'}
                className="system-button has-tooltip tooltip-above"
                data-tooltip="Theme"
                type="button"
                onPointerDown={() => setAreTooltipsSuppressed(true)}
                onClick={() => {
                  setIsThemePanelOpen((open) => !open);
                  setIsSystemPanelOpen(false);
                  setIsConnectionsPanelOpen(false);
                }}
              >
                <Brush size={14} strokeWidth={ICON_STROKE} />
              </button>
              <button
                aria-label={isConnectionsPanelOpen ? 'Hide connections' : 'Connections'}
                className="system-button has-tooltip tooltip-above"
                data-tooltip="Connections"
                type="button"
                onPointerDown={() => setAreTooltipsSuppressed(true)}
                onClick={() => {
                  setIsConnectionsPanelOpen((open) => !open);
                  setIsSystemPanelOpen(false);
                  setIsThemePanelOpen(false);
                  if (connections === null) {
                    void loadConnections();
                  }
                }}
              >
                <Plug size={14} strokeWidth={ICON_STROKE} />
              </button>
            </div>
          </div>
        </aside>
      )}

      <section className="chat">
        <header className={"chat-header" + (isHeaderScrolled && uiPrefs.showHeaderShadow ? " scrolled" : "")}>
          <div className="chat-title">
            <button
              aria-label={isSidebarOpen ? 'Hide conversations' : 'Show conversations'}
              className="icon-button subtle has-tooltip tooltip-align-left"
              data-tooltip={isSidebarOpen ? 'Hide conversations' : 'Show conversations'}
              type="button"
              onPointerDown={() => setAreTooltipsSuppressed(true)}
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
            {isTemporaryChat && (
              <span className="temporary-badge" title="This chat is not saved">
                Temporary
              </span>
            )}
            {activeSearchResults.length > 0 && (
              <button
                aria-label={areSourcesVisible ? 'Hide sources' : 'Show sources'}
                className="sources-toggle has-tooltip tooltip-align-left"
                data-tooltip={areSourcesVisible ? 'Hide sources' : 'Show sources'}
                type="button"
                onPointerDown={() => setAreTooltipsSuppressed(true)}
                onClick={() => setAreSourcesVisible((visible) => !visible)}
              >
                <FileText size={14} strokeWidth={ICON_STROKE} />
                {activeSearchResults.length}
              </button>
            )}
            {isTemporaryChat && messages.length > 0 && (
              <button
                aria-label="Save temporary chat"
                className="save-chat-button has-tooltip tooltip-align-left"
                data-tooltip="Save chat"
                disabled={isSending}
                type="button"
                onPointerDown={() => setAreTooltipsSuppressed(true)}
                onClick={() => void saveTemporaryChat()}
              >
                <Check size={14} strokeWidth={ICON_STROKE} />
                Save
              </button>
            )}
          </div>
        </header>

        <div className="messages" ref={messagesRef} style={messagesStyle} onScroll={updateMessageScrollState}>
          {messages.length === 0 && agentActivities.length === 0 ? (
            <div className="empty-state" aria-label="No messages" />
          ) : (
            <>
              {messages.map((message, index) => {
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
                            className="message-edit has-tooltip"
                            data-tooltip="Edit"
                            type="button"
                            onPointerDown={() => setAreTooltipsSuppressed(true)}
                            onClick={() => editMessage(message)}
                          >
                            <PenLine size={14} strokeWidth={ICON_STROKE} />
                          </button>
                        )}
                        {message.role === 'assistant' && uiPrefs.showResponseDetails && (
                          <div className="response-tools">
                            <ResponseMeta
                              prefs={uiPrefs}
                              provider={
                                message.provider ??
                                responseProviders[key] ??
                                statusProviderInfo(defaultResponseProvider(systemStatus))
                              }
                              sleepingProviders={systemStatus?.providers.filter((provider) => provider.state === 'sleeping') ?? []}
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
              })}
              {agentActivities.map((activity) => (
                <AgentActivityRow activity={activity} key={activity.id} />
              ))}
            </>
          )}
          <div ref={messageEndRef} />
        </div>
        {uiPrefs.showScrollCue && hasScrollableMessages && messages.length > 0 && (
          <div className={`scroll-note ${isAtMessageEnd ? 'at-end' : 'can-scroll'}`} style={scrollNoteStyle}>
            {isAtMessageEnd ? (
              <span>End</span>
            ) : (
              <button type="button" onClick={scrollToMessageEnd}>
                <ArrowDown size={12} strokeWidth={ICON_STROKE} />
                Scroll
              </button>
            )}
          </div>
        )}

        <form
          className={[
            'composer',
            uiPrefs.showComposerShimmer ? 'has-shimmer' : '',
            isDraggingFiles ? 'is-dragging' : '',
          ]
            .filter(Boolean)
            .join(' ')}
          ref={composerRef}
          onDragLeave={handleComposerDragLeave}
          onDragOver={handleComposerDragOver}
          onDrop={handleComposerDrop}
          onSubmit={sendMessage}
        >
          {error && <div className="error">{error}</div>}
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
            <label
              className="icon-button file-picker has-tooltip tooltip-bottom-safe"
              data-tooltip="Attach"
              onPointerDown={() => setAreTooltipsSuppressed(true)}
            >
              <Paperclip size={16} strokeWidth={ICON_STROKE} />
              <input
                aria-label="Attach files"
                multiple
                type="file"
                accept={ATTACHMENT_ACCEPT}
                onChange={(event) => {
                  attachFiles(event.target.files ?? []);
                  event.currentTarget.value = '';
                }}
              />
            </label>
            <div className={`textarea-wrap${voiceListening ? ' is-listening' : ''}`}>
              <textarea
                ref={textareaRef}
                aria-label="Message"
                placeholder={voiceListening ? 'Listening…' : 'Message · ⌘↵'}
                rows={1}
                value={content}
                onKeyDown={handleComposerKeyDown}
                onChange={(event) => updateContent(event.target.value)}
              />
              {voiceListening && (
                <div className="voice-pulse" aria-hidden="true">
                  {[0,1,2,3,4].map((i) => <span key={i} style={{ animationDelay: `${i * 0.1}s` }} />)}
                </div>
              )}
            </div>
            <div className="send-wrap">
              <div className="mic-drawer">
                <button
                  aria-label={voiceListening ? 'Stop recording' : 'Start voice input'}
                  className={`icon-button mic-button has-tooltip tooltip-bottom-safe${voiceListening ? ' is-listening' : ''}`}
                  data-tooltip={voiceListening ? 'Stop' : 'Voice'}
                  type="button"
                  onClick={toggleVoice}
                  onPointerDown={() => setAreTooltipsSuppressed(true)}
                >
                  {voiceListening ? <MicOff size={16} strokeWidth={ICON_STROKE} /> : <Mic size={16} strokeWidth={ICON_STROKE} />}
                </button>
              </div>
              <button
                aria-label="Send"
                className="send-button has-tooltip tooltip-bottom-safe"
                data-tooltip="Send"
                disabled={isSending || !content.trim()}
                type="submit"
                onPointerDown={() => setAreTooltipsSuppressed(true)}
              >
                <ArrowUpRight size={14} strokeWidth={ICON_STROKE} />
              </button>
            </div>
          </div>
        </form>
      </section>

      {showSources && <SourcesPanel results={activeSearchResults} />}

      {isSystemDetailsOpen && (
        <SystemDetailsDialog
          status={systemStatus}
          agentStatus={agentStatus}
          agentRuns={agentRuns}
          users={users}
          diagnostics={agentDiagnostics}
          editProposals={agentEditProposals}
          settings={appSettings}
          onReviewProposal={(proposalId, status) => void reviewAgentEditProposal(proposalId, status)}
          onApplyProposal={(proposalId) => void applyAgentEditProposal(proposalId)}
          onCreateProposal={(input) => void createAgentEditProposal(input)}
          onApproveCommand={(command) => void approveAgentCommand(command)}
          onCheckCommand={(command) => void checkAgentCommand(command)}
          onRunCommand={(command, approvalId) => void runAgentCommand(command, approvalId)}
          onRunHook={(hookId, command, approvalId) => void runAgentHook(hookId, command, approvalId)}
          onCreateHookRun={(input) => void createAgentHookRun(input)}
          onRunSkill={(skillId, command, approvalId) => void runAgentSkill(skillId, command, approvalId)}
          onRunSubagent={(subagentId, query) => void runAgentSubagent(subagentId, query)}
          onCallMCPTool={(toolId, args) => void callAgentMCPTool(toolId, args)}
          onReadMCPResource={(resourceId) => void readAgentMCPResource(resourceId)}
          onSubscribeMCPResource={(resourceId) => void subscribeAgentMCPResource(resourceId)}
          onUnsubscribeMCPResource={(subscriptionId) => void unsubscribeAgentMCPResource(subscriptionId)}
          onGetMCPPrompt={(promptId, args) => void getAgentMCPPrompt(promptId, args)}
          onSaveRun={() => void saveAgentRunSnapshot()}
          onCreateTrace={(input) => void createAgentTrace(input)}
          onCreateUser={(email, name) => void createUser(email, name)}
          onStartLoop={(input) => void startAgentLoop(input)}
          onContinueLoop={(loopId, input) => void continueAgentLoop(loopId, input)}
          onCancelLoop={(loopId) => void cancelAgentLoop(loopId)}
          onSetUnrestricted={(unrestricted) => void setAgentUnrestricted(unrestricted)}
          autoApproveCategories={autoApproveCategories}
          onAutoApproveCategories={(categories) => void setAutoApproveCategories(categories)}
          onStartBackgroundJob={(input) => void startBackgroundJob(input)}
          onCancelBackgroundJob={(id) => void cancelBackgroundJob(id)}
          onWorkspaceChange={(root) => void saveAgentWorkspaceRoot(root)}
          onSettingsChange={(next) => void saveAppSettings(next)}
          onClose={() => setIsSystemDetailsOpen(false)}
        />
      )}
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

function AgentActivityRow({ activity }: { activity: AgentActivity }) {
  return (
    <article className={`agent-activity ${activity.state}`} aria-label={`Agent ${activity.kind} ${activity.state}`}>
      <div className="agent-activity-main">
        <span>{activity.kind}</span>
        <strong>{activity.label}</strong>
        <em>{activity.state}</em>
      </div>
      {(activity.params || activity.result) && (
        <p>
          {activity.params && <span>{activity.params}</span>}
          {activity.params && activity.result && <span aria-hidden="true">{' -> '}</span>}
          {activity.result && <span>{activity.result}</span>}
        </p>
      )}
      {activity.resultDetail && <pre className="agent-activity-output">{activity.resultDetail}</pre>}
    </article>
  );
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
        if (block.type === 'rule') {
          return <hr key={index} />;
        }
        if (block.type === 'blockquote') {
          return (
            <blockquote key={index}>
              {block.items.map((item, itemIndex) => (
                <p key={itemIndex}>{renderInlineMarkdown(item)}</p>
              ))}
            </blockquote>
          );
        }
        return <p key={index}>{renderInlineMarkdown(block.text)}</p>;
      })}
    </div>
  );
}

function CodeBlock({ code, focusedLine, language }: { code: string; focusedLine?: number; language?: string }) {
  const [copied, setCopied] = useState(false);
  const [isPreviewOpen, setIsPreviewOpen] = useState(false);
  const blockRef = useRef<HTMLPreElement | null>(null);
  const previewRef = useRef<HTMLDivElement | null>(null);
  const canPreview = isPreviewableHTML(code, language);
  const lines = useMemo(() => code.split('\n'), [code]);
  const byteCount = useMemo(() => new TextEncoder().encode(code).length, [code]);
  const meta = codeMeta(language, lines.length, byteCount);

  useEffect(() => {
    if (!isPreviewOpen) {
      return;
    }
    window.requestAnimationFrame(() => {
      previewRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'nearest' });
    });
  }, [isPreviewOpen]);

  useEffect(() => {
    if (!focusedLine) {
      return;
    }
    window.requestAnimationFrame(() => {
      const target = blockRef.current?.querySelector<HTMLElement>(`[data-line="${focusedLine}"]`);
      target?.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'nearest' });
    });
  }, [code, focusedLine]);

  return (
    <div className="code-shell">
      <div className="code-top">
        <span>{meta}</span>
        <div style={{ display: 'flex', gap: 4 }}>
          <button
            aria-label="Copy code"
            className="code-action has-tooltip tooltip-above"
            data-tooltip={copied ? 'Copied' : 'Copy'}
            type="button"
            onClick={() => {
              navigator.clipboard.writeText(code).then(() => {
                setCopied(true);
                setTimeout(() => setCopied(false), 2000);
              }).catch(() => {});
            }}
          >
            {copied ? <Check size={13} strokeWidth={ICON_STROKE} /> : <Copy size={13} strokeWidth={ICON_STROKE} />}
          </button>
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
      </div>
      <pre className="code-block" data-language={language || undefined} ref={blockRef}>
        <code>
          {lines.map((line, index) => (
            <span
              className={focusedLine === index + 1 ? 'code-line focused' : 'code-line'}
              data-line={index + 1}
              key={index}
            >
              <span className="code-line-number">{index + 1}</span>
              <span className="code-line-text">{highlightCode(line || ' ', language)}</span>
            </span>
          ))}
        </code>
      </pre>
      {canPreview && isPreviewOpen && (
        <div className="code-preview-shell" ref={previewRef}>
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
  | { type: 'list'; items: string[] }
  | { type: 'rule' }
  | { type: 'blockquote'; items: string[] };

function parseMarkdownBlocks(content: string): MarkdownBlock[] {
  const blocks: MarkdownBlock[] = [];
  const paragraph: string[] = [];
  let list: string[] = [];
  let quote: string[] = [];
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

  const flushQuote = () => {
    if (quote.length === 0) {
      return;
    }
    blocks.push({ type: 'blockquote', items: quote });
    quote = [];
  };

  for (const rawLine of content.split(/\r?\n/)) {
    if (rawLine.trim().startsWith('```')) {
      flushParagraph();
      flushList();
      flushQuote();
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
      flushQuote();
      continue;
    }

    if (/^([-*_])(?:\s*\1){2,}$/.test(line)) {
      flushParagraph();
      flushList();
      flushQuote();
      blocks.push({ type: 'rule' });
      continue;
    }

    const heading = line.match(/^(#{1,3})\s+(.+)$/) ?? line.match(/^\*\*(.+)\*\*$/);
    if (heading) {
      flushParagraph();
      flushList();
      flushQuote();
      blocks.push({ type: 'heading', text: cleanMarkdownText(heading[2] ?? heading[1]) });
      continue;
    }

    const listItem = line.match(/^[-*•]\s+(.+)$/) ?? line.match(/^\d+\.\s+(.+)$/);
    if (listItem) {
      flushParagraph();
      flushQuote();
      list.push(cleanMarkdownText(listItem[1]));
      continue;
    }

    if (line.startsWith('>')) {
      flushParagraph();
      flushList();
      quote.push(cleanMarkdownText(line.replace(/^>\s*/, '')));
      continue;
    }

    flushList();
    flushQuote();
    paragraph.push(cleanMarkdownText(line));
  }

  flushParagraph();
  flushList();
  flushQuote();
  if (isCodeBlock && code.length > 0) {
    blocks.push({ type: 'code', language: codeLanguage, text: code.join('\n') });
  }

  return blocks.length > 0 ? blocks : [{ type: 'paragraph', text: content }];
}

function cleanMarkdownText(value: string): string {
  return value
    .replace(/\s+/g, ' ')
    .replace(/\\`/g, '`')
    .replace(/【([^】(]+)\s+\((https?:\/\/[^)]+)\)】/g, '[$1]($2)')
    .replace(/【([^】]+)】/g, '$1')
    .trim();
}

function renderInlineMarkdown(text: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  const pattern = /(`[^`\n]+`|\*\*[^*]+\*\*|\[([^\]]+)\]\(([^)\s]+)\)|(https?:\/\/[^\s)]+))/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      nodes.push(text.slice(lastIndex, match.index));
    }

    const token = match[0];
    if (token.startsWith('`')) {
      nodes.push(<code key={`${match.index}-code`}>{token.slice(1, -1)}</code>);
    } else if (token.startsWith('**')) {
      nodes.push(<strong key={`${match.index}-strong`}>{renderInlineMarkdown(token.slice(2, -2))}</strong>);
    } else {
      const label = match[2] ?? tidyUrlLabel(token);
      const href = match[3] ?? token;
      if (isSafeMarkdownHref(href)) {
        nodes.push(
          <a key={`${match.index}-link`} href={href} target="_blank" rel="noreferrer">
            {label}
          </a>,
        );
      } else {
        nodes.push(token);
      }
    }
    lastIndex = pattern.lastIndex;
  }

  if (lastIndex < text.length) {
    nodes.push(text.slice(lastIndex));
  }

  return nodes;
}

function isSafeMarkdownHref(href: string) {
  const value = href.trim();
  if (/^https?:\/\//i.test(value)) {
    return true;
  }
  if (value.startsWith('/') && !value.startsWith('//')) {
    return true;
  }
  if (value.startsWith('./') || value.startsWith('../')) {
    return true;
  }
  return false;
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

function SystemPanel({
  status,
  agentStatus,
  agentRuns,
  onOpenDetails,
}: {
  status: SystemStatus | null;
  agentStatus: AgentStatus | null;
  agentRuns: AgentRun[];
  onOpenDetails: () => void;
}) {
  const primary = status?.providers.find((provider) => provider.role === 'primary');
  const disabledProviders = status?.providers.filter((provider) => !provider.enabled) ?? [];
  const localProvider = status?.providers.find((provider) => provider.role === 'local');
  const agentValue = agentStatus?.runSummary?.state ?? (agentStatus ? 'ready' : 'Ready');

  return (
    <div className="system-panel" role="status" aria-label="System status">
      <SystemRow Icon={Check} label="Model" value={primary?.model ?? 'Ready'} />
      <SystemRow Icon={Database} label="Storage" value={status?.storage ?? 'Ready'} />
      <SystemRow Icon={SearchIcon} label="Search" value={status?.search ?? 'Ready'} />
      <SystemRow
        Icon={Cpu}
        label="Local"
        value={localProvider ? providerStatusText(localProvider) : 'Off'}
      />
      <SystemRow Icon={Route} label="Agent" value={agentValue} />
      {localProvider?.detail && localProvider.state !== 'ready' && (
        <div className="system-detail">{localProvider.detail}</div>
      )}
      {disabledProviders.length > 0 && (
        <div className="system-muted">
          {disabledProviders.map((provider) => provider.name).join(', ')} off
        </div>
      )}
      <button
        aria-label="System details"
        className="system-details-button has-tooltip tooltip-above"
        data-tooltip="Details"
        type="button"
        onClick={onOpenDetails}
      >
        <ListChecks size={12} strokeWidth={ICON_STROKE} />
      </button>
    </div>
  );
}

function SystemDetailsDialog({
  agentRuns,
  agentStatus,
  users,
  diagnostics,
  editProposals,
  onApplyProposal,
  onApproveCommand,
  onCheckCommand,
  onClose,
  onCreateHookRun,
  onCreateProposal,
  onCreateTrace,
  onCreateUser,
  onReviewProposal,
  onRunCommand,
  onRunHook,
  onRunSkill,
  onRunSubagent,
  onCallMCPTool,
  onReadMCPResource,
  onSubscribeMCPResource,
  onUnsubscribeMCPResource,
  onGetMCPPrompt,
  onSaveRun,
  onStartLoop,
  onContinueLoop,
  onCancelLoop,
  onSetUnrestricted,
  onAutoApproveCategories,
  autoApproveCategories,
  onStartBackgroundJob,
  onCancelBackgroundJob,
  onSettingsChange,
  onWorkspaceChange,
  settings,
  status,
}: {
  agentRuns: AgentRun[];
  agentStatus: AgentStatus | null;
  users: User[];
  diagnostics: AgentDiagnostic[];
  editProposals: AgentEditProposal[];
  onApplyProposal: (proposalId: string) => void;
  onApproveCommand: (command: string) => void;
  onCheckCommand: (command: string) => void;
  onClose: () => void;
  onCreateHookRun: (input: { hookId: string; state: string; detail?: string }) => void;
  onCreateProposal: (input: { path: string; content: string; summary?: string }) => void;
  onCreateTrace: (input: { event: string; state: string; detail?: string }) => void;
  onCreateUser: (email: string, name: string) => void;
  onReviewProposal: (proposalId: string, status: 'approved' | 'rejected') => void;
  onRunCommand: (command: string, approvalId: string) => void;
  onRunHook: (hookId: string, command: string, approvalId?: string) => void;
  onRunSkill: (skillId: string, command: string, approvalId?: string) => void;
  onRunSubagent: (subagentId: string, query: string) => void;
  onCallMCPTool: (toolId: string, args: Record<string, unknown>) => void;
  onReadMCPResource: (resourceId: string) => void;
  onSubscribeMCPResource: (resourceId: string) => void;
  onUnsubscribeMCPResource: (subscriptionId: string) => void;
  onGetMCPPrompt: (promptId: string, args: Record<string, unknown>) => void;
  onSaveRun: () => void;
  onStartLoop: (input: AgentLoopRequest) => void;
  onContinueLoop: (loopId: string, input: Omit<AgentLoopRequest, 'goal'>) => void;
  onCancelLoop: (loopId: string) => void;
  onSetUnrestricted: (unrestricted: boolean) => void;
  onAutoApproveCategories: (categories: string[]) => void;
  autoApproveCategories: string[];
  onStartBackgroundJob: (input: { goal: string; mode?: string; maxIterations?: number; autoApply?: boolean }) => void;
  onCancelBackgroundJob: (id: string) => void;
  onSettingsChange: (settings: AppSettings) => void;
  onWorkspaceChange: (root: string) => void;
  settings: AppSettings | null;
  status: SystemStatus | null;
}) {
  const [selectedProposalId, setSelectedProposalId] = useState<string | null>(editProposals[0]?.id ?? null);
  const [commandInput, setCommandInput] = useState('');
  const [loopGoalInput, setLoopGoalInput] = useState('');
  const [loopQueryInput, setLoopQueryInput] = useState('');
  const [loopFileInput, setLoopFileInput] = useState('');
  const [loopCommandInput, setLoopCommandInput] = useState('');
  const [loopProposalPathInput, setLoopProposalPathInput] = useState('');
  const [loopProposalContentInput, setLoopProposalContentInput] = useState('');
  const [loopModeInput, setLoopModeInput] = useState<AgentLoopMode>('guided');
  const [unrestrictedConfirm, setUnrestrictedConfirm] = useState<boolean | null>(null);
  const [hookCommandInput, setHookCommandInput] = useState('');
  const [hookRunHookId, setHookRunHookId] = useState(agentStatus?.hooks?.[0]?.id ?? '');
  const [hookRunState, setHookRunState] = useState('completed');
  const [hookRunDetail, setHookRunDetail] = useState('');
  const [skillCommandInput, setSkillCommandInput] = useState('');
  const [traceEventInput, setTraceEventInput] = useState('manual trace');
  const [traceStateInput, setTraceStateInput] = useState('completed');
  const [traceDetailInput, setTraceDetailInput] = useState('');
  const [proposalPathInput, setProposalPathInput] = useState('');
  const [userEmailInput, setUserEmailInput] = useState('');
  const [userNameInput, setUserNameInput] = useState('');
  const [proposalSummaryInput, setProposalSummaryInput] = useState('');
  const [proposalContentInput, setProposalContentInput] = useState('');
  const [mcpToolId, setMCPToolId] = useState(agentStatus?.mcpTools?.[0]?.id ?? '');
  const [mcpResourceId, setMCPResourceId] = useState(agentStatus?.mcpResources?.[0]?.id ?? '');
  const [mcpPromptId, setMCPPromptId] = useState(agentStatus?.mcpPrompts?.[0]?.id ?? '');
  const [mcpArguments, setMCPArguments] = useState('{}');
  const [mcpPromptArguments, setMCPPromptArguments] = useState('{}');
  const [mcpError, setMCPError] = useState<string | null>(null);
  const [workspaceRootInput, setWorkspaceRootInput] = useState(agentStatus?.workspaceRoot ?? '');
  const [workspaceQuery, setWorkspaceQuery] = useState('');
  const [workspaceMode, setWorkspaceMode] = useState<'search' | 'symbols' | 'references'>('search');
  const [workspaceResults, setWorkspaceResults] = useState<AgentWorkspaceSearchResult[]>([]);
  const [workspaceSymbols, setWorkspaceSymbols] = useState<AgentWorkspaceSymbol[]>([]);
  const [workspaceReferences, setWorkspaceReferences] = useState<AgentWorkspaceReference[]>([]);
  const [workspaceFile, setWorkspaceFile] = useState<AgentWorkspaceFile | null>(null);
  const [workspaceFocusedLine, setWorkspaceFocusedLine] = useState<number | null>(null);
  const [workspaceError, setWorkspaceError] = useState<string | null>(null);
  const [isWorkspaceLoading, setIsWorkspaceLoading] = useState(false);
  function submitUser(event: FormEvent) {
    event.preventDefault();
    const email = userEmailInput.trim();
    const name = userNameInput.trim();
    if (!email) {
      return;
    }
    onCreateUser(email, name);
    setUserEmailInput('');
    setUserNameInput('');
  }
  const enabledAgentTools = agentStatus?.tools.filter((tool) => tool.access !== 'off').length ?? 0;
  const workspaceOn = agentStatus?.tools
    .filter((tool) => ['read_file', 'search_files', 'diagnostics', 'symbols', 'references'].includes(tool.id))
    .some((tool) => tool.access !== 'off') ?? false;
  const blockedChecks = agentStatus?.commandChecks?.filter((check) => !check.allowed) ?? [];
  const normalizedCommandInput = normalizeCommand(commandInput);
  const commandApprovals = agentStatus?.commandApprovals ?? [];
  const commandApproval = findApprovedCommandApproval(commandApprovals, normalizedCommandInput);
  const normalizedHookCommandInput = normalizeCommand(hookCommandInput);
  const hookCommandApproval = findApprovedCommandApproval(commandApprovals, normalizedHookCommandInput);
  const selectedProposal =
    editProposals.find((proposal) => proposal.id === selectedProposalId) ?? editProposals[0] ?? null;
  const mcpTools = agentStatus?.mcpTools ?? [];
  const mcpResources = agentStatus?.mcpResources ?? [];
  const mcpPrompts = agentStatus?.mcpPrompts ?? [];
  const agentTimeline = useMemo(() => buildAgentTimeline(agentStatus, editProposals), [agentStatus, editProposals]);
  const workspaceEmptyText = !workspaceOn
    ? 'Workspace tools are off'
    : workspaceMode === 'symbols'
      ? 'No symbols'
      : workspaceMode === 'references'
        ? 'No references'
        : 'No search results';

  useEffect(() => {
    if (!selectedProposalId && editProposals.length > 0) {
      setSelectedProposalId(editProposals[0].id);
      return;
    }
    if (selectedProposalId && editProposals.length > 0 && !editProposals.some((proposal) => proposal.id === selectedProposalId)) {
      setSelectedProposalId(editProposals[0].id);
    }
  }, [editProposals, selectedProposalId]);

  useEffect(() => {
    setWorkspaceRootInput(agentStatus?.workspaceRoot ?? '');
  }, [agentStatus?.workspaceRoot]);

  useEffect(() => {
    if (!hookRunHookId && (agentStatus?.hooks ?? []).length > 0) {
      setHookRunHookId((agentStatus?.hooks ?? [])[0].id);
      return;
    }
    if (hookRunHookId && (agentStatus?.hooks ?? []).length > 0 && !(agentStatus?.hooks ?? []).some((hook) => hook.id === hookRunHookId)) {
      setHookRunHookId((agentStatus?.hooks ?? [])[0].id);
    }
  }, [agentStatus?.hooks, hookRunHookId]);

  useEffect(() => {
    if (!mcpToolId && mcpTools.length > 0) {
      setMCPToolId(mcpTools[0].id);
      return;
    }
    if (mcpToolId && mcpTools.length > 0 && !mcpTools.some((tool) => tool.id === mcpToolId)) {
      setMCPToolId(mcpTools[0].id);
    }
  }, [mcpToolId, mcpTools]);

  useEffect(() => {
    if (!mcpResourceId && mcpResources.length > 0) {
      setMCPResourceId(mcpResources[0].id);
      return;
    }
    if (mcpResourceId && mcpResources.length > 0 && !mcpResources.some((resource) => resource.id === mcpResourceId)) {
      setMCPResourceId(mcpResources[0].id);
    }
  }, [mcpResourceId, mcpResources]);

  useEffect(() => {
    if (!mcpPromptId && mcpPrompts.length > 0) {
      setMCPPromptId(mcpPrompts[0].id);
      return;
    }
    if (mcpPromptId && mcpPrompts.length > 0 && !mcpPrompts.some((prompt) => prompt.id === mcpPromptId)) {
      setMCPPromptId(mcpPrompts[0].id);
    }
  }, [mcpPromptId, mcpPrompts]);

  useEffect(() => {
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') {
        onClose();
      }
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [onClose]);

  async function searchWorkspace(event: FormEvent) {
    event.preventDefault();
    const query = workspaceQuery.trim();
    setWorkspaceFile(null);
    setWorkspaceFocusedLine(null);
    if (query.length < 2) {
      setWorkspaceError('Use at least 2 characters.');
      setWorkspaceMode('search');
      setWorkspaceResults([]);
      setWorkspaceSymbols([]);
      setWorkspaceReferences([]);
      return;
    }
    try {
      setIsWorkspaceLoading(true);
      setWorkspaceError(null);
      setWorkspaceMode('search');
      setWorkspaceResults([]);
      setWorkspaceSymbols([]);
      setWorkspaceReferences([]);
      const results = await request<AgentWorkspaceSearchResult[]>(
        `/api/agent/workspace/search?q=${encodeURIComponent(query)}`,
      );
      setWorkspaceResults(Array.isArray(results) ? results : []);
    } catch (err) {
      setWorkspaceResults([]);
      setWorkspaceSymbols([]);
      setWorkspaceReferences([]);
      setWorkspaceError(err instanceof Error ? err.message : 'Could not search workspace.');
    } finally {
      setIsWorkspaceLoading(false);
    }
  }

  async function readWorkspaceFile(path: string) {
    try {
      setIsWorkspaceLoading(true);
      setWorkspaceError(null);
      const file = await request<AgentWorkspaceFile>(`/api/agent/workspace/file?path=${encodeURIComponent(path)}`);
      setWorkspaceFile(file);
    } catch (err) {
      setWorkspaceError(err instanceof Error ? err.message : 'Could not read file.');
    } finally {
      setIsWorkspaceLoading(false);
    }
  }

  async function searchWorkspaceSymbols() {
    try {
      setIsWorkspaceLoading(true);
      setWorkspaceError(null);
      setWorkspaceMode('symbols');
      setWorkspaceResults([]);
      setWorkspaceSymbols([]);
      setWorkspaceReferences([]);
      setWorkspaceFile(null);
      setWorkspaceFocusedLine(null);
      const symbols = await request<AgentWorkspaceSymbol[]>(
        `/api/agent/workspace/symbols?q=${encodeURIComponent(workspaceQuery.trim())}`,
      );
      setWorkspaceSymbols(Array.isArray(symbols) ? symbols : []);
    } catch (err) {
      setWorkspaceResults([]);
      setWorkspaceSymbols([]);
      setWorkspaceReferences([]);
      setWorkspaceError(err instanceof Error ? err.message : 'Could not read symbols.');
    } finally {
      setIsWorkspaceLoading(false);
    }
  }

  async function searchWorkspaceReferences() {
    try {
      setIsWorkspaceLoading(true);
      setWorkspaceError(null);
      setWorkspaceMode('references');
      setWorkspaceResults([]);
      setWorkspaceSymbols([]);
      setWorkspaceReferences([]);
      setWorkspaceFile(null);
      setWorkspaceFocusedLine(null);
      const references = await request<AgentWorkspaceReference[]>(
        `/api/agent/workspace/references?q=${encodeURIComponent(workspaceQuery.trim())}`,
      );
      setWorkspaceReferences(Array.isArray(references) ? references : []);
    } catch (err) {
      setWorkspaceResults([]);
      setWorkspaceSymbols([]);
      setWorkspaceReferences([]);
      setWorkspaceError(err instanceof Error ? err.message : 'Could not read references.');
    } finally {
      setIsWorkspaceLoading(false);
    }
  }

  function submitCommand(action: 'check' | 'approve' | 'run') {
    const command = normalizeCommand(commandInput);
    if (!command) {
      return;
    }
    if (action === 'check') {
      onCheckCommand(command);
    } else if (action === 'approve') {
      onApproveCommand(command);
    } else if (commandApproval) {
      onRunCommand(command, commandApproval.id);
    }
  }

  function submitAgentLoop(event: FormEvent) {
    event.preventDefault();
    const goal = loopGoalInput.trim();
    if (!goal) {
      return;
    }
    onStartLoop({
      goal,
      mode: loopModeInput,
      maxIterations: isAutonomousLoopMode(loopModeInput) ? 5 : undefined,
      autoApply: isAutonomousLoopMode(loopModeInput) ? true : undefined,
      command: loopCommandInput.trim() || undefined,
      query: loopQueryInput.trim() || undefined,
      filePath: loopFileInput.trim() || undefined,
    });
  }

  function runHook(hookId: string) {
    if (normalizedHookCommandInput && !hookCommandApproval) {
      return;
    }
    onRunHook(hookId, normalizedHookCommandInput, hookCommandApproval?.id);
  }

  function submitHookRun(event: FormEvent) {
    event.preventDefault();
    if (!hookRunHookId || !hookRunState.trim()) {
      return;
    }
    onCreateHookRun({
      hookId: hookRunHookId,
      state: hookRunState.trim(),
      detail: hookRunDetail.trim() || undefined,
    });
    setHookRunDetail('');
  }

  function submitTrace(event: FormEvent) {
    event.preventDefault();
    const eventName = traceEventInput.trim();
    const state = traceStateInput.trim();
    if (!eventName || !state) {
      return;
    }
    onCreateTrace({
      event: eventName,
      state,
      detail: traceDetailInput.trim() || undefined,
    });
    setTraceDetailInput('');
  }

  function submitProposal(event: FormEvent) {
    event.preventDefault();
    const path = proposalPathInput.trim();
    if (!path) {
      return;
    }
    onCreateProposal({
      path,
      summary: proposalSummaryInput.trim() || undefined,
      content: proposalContentInput,
    });
    setProposalPathInput('');
    setProposalSummaryInput('');
    setProposalContentInput('');
  }

  function runSkill(skillId: string, command: string, approvalId?: string) {
    if (command && !approvalId) {
      return;
    }
    onRunSkill(skillId, command, approvalId);
  }

  function submitMCPCall(event: FormEvent) {
    event.preventDefault();
    if (!mcpToolId) {
      return;
    }
    let args: unknown = {};
    const rawArgs = mcpArguments.trim();
    if (rawArgs) {
      try {
        args = JSON.parse(rawArgs);
      } catch {
        setMCPError('Arguments must be JSON.');
        return;
      }
    }
    if (!args || typeof args !== 'object' || Array.isArray(args)) {
      setMCPError('Arguments must be a JSON object.');
      return;
    }
    setMCPError(null);
    onCallMCPTool(mcpToolId, args as Record<string, unknown>);
  }

  function submitMCPResourceRead(event: FormEvent) {
    event.preventDefault();
    if (!mcpResourceId) {
      return;
    }
    setMCPError(null);
    onReadMCPResource(mcpResourceId);
  }

  function submitMCPPromptGet(event: FormEvent) {
    event.preventDefault();
    if (!mcpPromptId) {
      return;
    }
    let args: unknown = {};
    const rawArgs = mcpPromptArguments.trim();
    if (rawArgs) {
      try {
        args = JSON.parse(rawArgs);
      } catch {
        setMCPError('Prompt arguments must be JSON.');
        return;
      }
    }
    if (!args || typeof args !== 'object' || Array.isArray(args)) {
      setMCPError('Prompt arguments must be a JSON object.');
      return;
    }
    setMCPError(null);
    onGetMCPPrompt(mcpPromptId, args as Record<string, unknown>);
  }

  return (
    <div className="dialog-backdrop" onPointerDown={onClose}>
      <section
        aria-modal="true"
        aria-label="System details"
        className="details-dialog"
        role="dialog"
        onPointerDown={(event) => event.stopPropagation()}
      >
        <div className="details-header">
          <div>
            <strong>System</strong>
            <p>Local status and agent checks.</p>
          </div>
          <button aria-label="Close system details" className="details-close" type="button" onClick={onClose}>
            <X size={14} strokeWidth={ICON_STROKE} />
          </button>
        </div>

        <div className="details-grid">
          <DetailsSection title="Runtime">
            <DetailLine label="Storage" value={status?.storage ?? 'Ready'} />
            <DetailLine label="Search" value={status?.search ?? 'Ready'} />
            <DetailLine label="Providers" value={String(status?.providers.filter((provider) => provider.enabled).length ?? 0)} />
          </DetailsSection>

          <DetailsSection title="Agent">
            <DetailLine label="Mode" value={agentStatus?.mode ?? 'local'} />
            <DetailLine label="State" value={agentStatus?.runSummary?.state ?? 'ready'} />
            <DetailLine label="Tools" value={agentStatus ? `${enabledAgentTools}/${agentStatus.tools.length}` : '0'} />
            <DetailLine label="Workspace" value={workspaceOn ? 'On' : 'Off'} />
          </DetailsSection>

          <DetailsSection title="Counts">
            <DetailLine label="Hooks" value={String(agentStatus?.runSummary?.hookRuns ?? 0)} />
            <DetailLine label="Skills" value={String(agentStatus?.runSummary?.skillRuns ?? 0)} />
            <DetailLine label="Subagents" value={String(agentStatus?.runSummary?.subagentRuns ?? 0)} />
            <DetailLine label="MCP calls" value={String(agentStatus?.runSummary?.mcpCalls ?? 0)} />
            <DetailLine label="Commands" value={String(agentStatus?.runSummary?.commandRuns ?? 0)} />
            <DetailLine label="Proposals" value={String(agentStatus?.runSummary?.editProposals ?? editProposals.length)} />
            <DetailLine label="Runs" value={String(agentRuns.length)} />
            <DetailLine label="Users" value={String(users.length)} />
          </DetailsSection>

          <DetailsSection title="MCP">
            <DetailLine label="Servers" value={String(agentStatus?.mcpServers?.length ?? 0)} />
            <DetailLine label="Tools" value={String(agentStatus?.mcpTools?.length ?? 0)} />
            <DetailLine label="Resources" value={String(agentStatus?.mcpResources?.length ?? 0)} />
            <DetailLine label="Prompts" value={String(agentStatus?.mcpPrompts?.length ?? 0)} />
            <DetailLine label="Subscriptions" value={String(agentStatus?.mcpSubscriptions?.length ?? 0)} />
            <DetailLine label="Events" value={String(agentStatus?.mcpEvents?.length ?? 0)} />
          </DetailsSection>
        </div>

        {settings && (
          <section className="details-list providers-review">
            <SettingsPanel settings={settings} onChange={onSettingsChange} variant="details" />
          </section>
        )}

        <section className="details-list agent-session">
          <div className="details-list-header">
            <h3>Agent session</h3>
            <span>{agentTimeline.length}</span>
          </div>
          <div className="agent-timeline">
            {agentTimeline.length > 0 ? (
              agentTimeline.map((item) => (
                <div className={`agent-timeline-item ${item.state}`} key={item.id}>
                  <div className="agent-timeline-main">
                    <span>{item.kind}</span>
                    <strong>{item.title}</strong>
                    <em>{item.state}</em>
                  </div>
                  {item.detail && <p>{truncateText(item.detail.replace(/\s+/g, ' '), 140)}</p>}
                  {item.children && item.children.length > 0 && (
                    <div className="agent-timeline-children">
                      {item.children.slice(0, 6).map((child) => (
                        <span key={child.id}>
                          {child.title}: {child.state}
                          {child.detail ? ` · ${truncateText(child.detail, 90)}` : ''}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              ))
            ) : (
              <p>No agent activity</p>
            )}
          </div>
        </section>

        <section className="details-list agent-control">
          <div className="details-list-header">
            <h3>Agent loop</h3>
            <span>{agentStatus?.runSummary?.agentLoops ?? 0}</span>
          </div>
          <form className="agent-loop-form" onSubmit={submitAgentLoop}>
            <div className="agent-loop-mode" aria-label="Loop mode" role="group">
              <button
                className={loopModeInput === 'guided' ? 'active' : ''}
                type="button"
                onClick={() => setLoopModeInput('guided')}
              >
                Guided
              </button>
              <button
                className={loopModeInput === 'auto' ? 'active' : ''}
                type="button"
                onClick={() => setLoopModeInput('auto')}
              >
                Auto
              </button>
              <button
                className={loopModeInput === 'developer' ? 'active' : ''}
                type="button"
                onClick={() => setLoopModeInput('developer')}
              >
                Developer
              </button>
            </div>
            {loopModeInput === 'developer' && (
              <div className="agent-unrestricted">
                {agentStatus?.unrestricted ? (
                  <button type="button" className="unrestricted-toggle active" onClick={() => onSetUnrestricted(false)}>
                    Unrestricted – ON
                  </button>
                ) : (
                  <button type="button" className="unrestricted-toggle" onClick={() => setUnrestrictedConfirm(true)}>
                    Enable unrestricted
                  </button>
                )}
                <div className="auto-approve-categories">
                  {['read', 'write', 'inspect', 'destructive'].map((cat) => (
                    <button
                      key={cat}
                      type="button"
                      className={autoApproveCategories.includes(cat) ? 'active' : ''}
                      onClick={() => {
                        if (autoApproveCategories.includes(cat)) {
                          onAutoApproveCategories(autoApproveCategories.filter((c) => c !== cat));
                        } else {
                          onAutoApproveCategories([...autoApproveCategories, cat]);
                        }
                      }}
                    >
                      Auto-approve {cat}
                    </button>
                  ))}
                </div>
              </div>
            )}
            <input
              aria-label="Agent goal"
              placeholder="Goal"
              value={loopGoalInput}
              onChange={(event) => setLoopGoalInput(event.target.value)}
            />
            <div className="agent-loop-options">
              <input
                aria-label="Search query"
                placeholder="Search query"
                value={loopQueryInput}
                onChange={(event) => setLoopQueryInput(event.target.value)}
              />
              <input
                aria-label="File path"
                placeholder="File path"
                value={loopFileInput}
                onChange={(event) => setLoopFileInput(event.target.value)}
              />
              <input
                aria-label="Command"
                placeholder="Command"
                value={loopCommandInput}
                onChange={(event) => setLoopCommandInput(event.target.value)}
              />
              <button disabled={!loopGoalInput.trim()} type="submit">
                Start
              </button>
              {isAutonomousLoopMode(loopModeInput) && (
                <button
                  disabled={!loopGoalInput.trim()}
                  type="button"
                  onClick={() => onStartBackgroundJob({ goal: loopGoalInput, mode: loopModeInput, autoApply: true, maxIterations: 10 })}
                >
                  Start bg
                </button>
              )}
            </div>
            <div className="agent-loop-options proposal-options">
              <input
                aria-label="Proposal path"
                placeholder="Proposal path"
                value={loopProposalPathInput}
                onChange={(event) => setLoopProposalPathInput(event.target.value)}
              />
              <textarea
                aria-label="Proposal content"
                placeholder="Proposal content"
                rows={3}
                value={loopProposalContentInput}
                onChange={(event) => setLoopProposalContentInput(event.target.value)}
              />
            </div>
          </form>
          <div className="agent-card-list">
            {(agentStatus?.agentLoops ?? []).slice(0, 3).map((loop) => (
              <AgentLoopCard
                editProposals={editProposals}
                key={loop.id}
                loop={loop}
                onApplyProposal={onApplyProposal}
                onApproveCommand={onApproveCommand}
                onCancelLoop={onCancelLoop}
                onContinueLoop={onContinueLoop}
                onReviewProposal={onReviewProposal}
                onSelectProposal={setSelectedProposalId}
                continueInput={{
                  command: loopCommandInput.trim() || undefined,
                  query: loopQueryInput.trim() || undefined,
                  filePath: loopFileInput.trim() || undefined,
                  proposalPath: loopProposalPathInput.trim() || undefined,
                  proposalContent: loopProposalPathInput.trim() ? loopProposalContentInput : undefined,
                }}
              />
            ))}
            {(agentStatus?.agentLoops ?? []).length === 0 && <p>No loops</p>}
          </div>
        </section>

        <section className="details-list agent-control">
          <div className="details-list-header">
            <h3>Commands</h3>
            <span>{agentStatus?.runSummary?.commandRuns ?? 0}</span>
          </div>
          <form
            className="agent-command-form"
            onSubmit={(event) => {
              event.preventDefault();
              submitCommand('check');
            }}
          >
            <input
              aria-label="Command"
              placeholder="Allowed command"
              value={commandInput}
              onChange={(event) => setCommandInput(event.target.value)}
            />
            <button disabled={!commandInput.trim()} type="submit">
              Check
            </button>
            <button disabled={!commandInput.trim()} type="button" onClick={() => submitCommand('approve')}>
              Approve
            </button>
            <button disabled={!normalizedCommandInput || !commandApproval} type="button" onClick={() => submitCommand('run')}>
              Run
            </button>
          </form>
          <div className="agent-list compact">
            {(agentStatus?.commandApprovals ?? []).slice(0, 3).map((approval) => (
              <p key={approval.id}>
                {approval.command} · {approval.state}
              </p>
            ))}
            {(agentStatus?.commandApprovals ?? []).length === 0 && <p>No approvals</p>}
          </div>
        </section>

        <section className="details-list agent-control">
          <div className="details-list-header">
            <h3>Hooks</h3>
            <span>{agentStatus?.runSummary?.hookRuns ?? 0}</span>
          </div>
          <input
            aria-label="Hook command"
            className="agent-inline-input"
            placeholder="Optional command"
            value={hookCommandInput}
            onChange={(event) => setHookCommandInput(event.target.value)}
          />
          <form className="agent-manual-form" onSubmit={submitHookRun}>
            <select
              aria-label="Hook run hook"
              disabled={(agentStatus?.hooks ?? []).length === 0}
              value={hookRunHookId}
              onChange={(event) => setHookRunHookId(event.target.value)}
            >
              {(agentStatus?.hooks ?? []).length === 0 ? (
                <option value="">No hooks</option>
              ) : (
                (agentStatus?.hooks ?? []).map((hook) => (
                  <option key={hook.id} value={hook.id}>
                    {hook.event}
                  </option>
                ))
              )}
            </select>
            <select aria-label="Hook run state" value={hookRunState} onChange={(event) => setHookRunState(event.target.value)}>
              <option value="completed">completed</option>
              <option value="failed">failed</option>
              <option value="skipped">skipped</option>
            </select>
            <input
              aria-label="Hook run detail"
              placeholder="Detail"
              value={hookRunDetail}
              onChange={(event) => setHookRunDetail(event.target.value)}
            />
            <button disabled={!hookRunHookId || !hookRunState.trim()} type="submit">
              Record
            </button>
          </form>
          <div className="agent-card-list">
            {(agentStatus?.hooks ?? []).map((hook) => (
              <div className="agent-card" key={hook.id}>
                <div>
                  <strong>{hook.event}</strong>
                  <span>{hook.state}</span>
                </div>
                <button disabled={Boolean(normalizedHookCommandInput && !hookCommandApproval)} type="button" onClick={() => runHook(hook.id)}>
                  Run
                </button>
              </div>
            ))}
            {(agentStatus?.hooks ?? []).length === 0 && <p>No hooks</p>}
          </div>
        </section>

        <section className="details-list agent-control">
          <div className="details-list-header">
            <h3>Skills</h3>
            <span>{agentStatus?.runSummary?.skillRuns ?? 0}</span>
          </div>
          <input
            aria-label="Skill command"
            className="agent-inline-input"
            placeholder="Optional command"
            value={skillCommandInput}
            onChange={(event) => setSkillCommandInput(event.target.value)}
          />
          <div className="agent-card-list">
            {(agentStatus?.skills ?? []).map((skill) => {
              const skillCommand = normalizeCommand(skillCommandInput || skill.command || '');
              const skillApproval = findApprovedCommandApproval(commandApprovals, skillCommand);
              const requiresApproval = Boolean(skillCommand);
              return (
                <div className="agent-card" key={skill.id}>
                  <div>
                    <strong>{skill.name}</strong>
                    <span>{skill.command || skill.state}</span>
                  </div>
                  <button
                    disabled={(skill.state === 'planned' && !skillCommandInput.trim()) || (requiresApproval && !skillApproval)}
                    type="button"
                    onClick={() => runSkill(skill.id, skillCommand, skillApproval?.id)}
                  >
                    Run
                  </button>
                </div>
              );
            })}
            {(agentStatus?.skills ?? []).length === 0 && <p>No skills</p>}
          </div>
        </section>

        <section className="details-list agent-control">
          <div className="details-list-header">
            <h3>Agent map</h3>
            <button type="button" onClick={onSaveRun}>
              Save run
            </button>
          </div>
          <div className="agent-card-list two-column">
            {(agentStatus?.tools ?? []).map((tool) => (
              <div className="agent-card read-only" key={tool.id}>
                <div>
                  <strong>{tool.name}</strong>
                  <span>{tool.access} · {tool.approval}</span>
                </div>
              </div>
            ))}
            {(agentStatus?.subagents ?? []).map((subagent) => (
              <div className="agent-card" key={subagent.id}>
                <div>
                  <strong>{subagent.name}</strong>
                  <span>{subagent.state} · {subagent.purpose}</span>
                </div>
                <button type="button" onClick={() => onRunSubagent(subagent.id, workspaceQuery.trim())}>
                  Run
                </button>
              </div>
            ))}
          </div>
        </section>

        <section className="details-list agent-control">
          <div className="details-list-header">
            <h3>MCP</h3>
            <span>{agentStatus?.runSummary?.mcpCalls ?? 0}</span>
          </div>
          <form className="agent-mcp-form" onSubmit={submitMCPResourceRead}>
            <select
              aria-label="MCP resource"
              disabled={mcpResources.length === 0}
              value={mcpResourceId}
              onChange={(event) => setMCPResourceId(event.target.value)}
            >
              {mcpResources.length === 0 ? (
                <option value="">No resources</option>
              ) : (
                mcpResources.map((resource) => (
                  <option key={resource.id} value={resource.id}>
                    {resource.serverName || resource.serverId}/{resource.name}
                  </option>
                ))
              )}
            </select>
            <button disabled={!mcpResourceId} type="submit">
              Read
            </button>
            <button disabled={!mcpResourceId} type="button" onClick={() => onSubscribeMCPResource(mcpResourceId)}>
              Subscribe
            </button>
          </form>
          <form className="agent-mcp-form" onSubmit={submitMCPPromptGet}>
            <select
              aria-label="MCP prompt"
              disabled={mcpPrompts.length === 0}
              value={mcpPromptId}
              onChange={(event) => setMCPPromptId(event.target.value)}
            >
              {mcpPrompts.length === 0 ? (
                <option value="">No prompts</option>
              ) : (
                mcpPrompts.map((prompt) => (
                  <option key={prompt.id} value={prompt.id}>
                    {prompt.serverName || prompt.serverId}/{prompt.name}
                  </option>
                ))
              )}
            </select>
            <textarea
              aria-label="MCP prompt arguments"
              disabled={mcpPrompts.length === 0}
              rows={2}
              value={mcpPromptArguments}
              onChange={(event) => setMCPPromptArguments(event.target.value)}
            />
            <button disabled={!mcpPromptId} type="submit">
              Get
            </button>
          </form>
          <form className="agent-mcp-form" onSubmit={submitMCPCall}>
            <select
              aria-label="MCP tool"
              disabled={mcpTools.length === 0}
              value={mcpToolId}
              onChange={(event) => setMCPToolId(event.target.value)}
            >
              {mcpTools.length === 0 ? (
                <option value="">No tools</option>
              ) : (
                mcpTools.map((tool) => (
                  <option key={tool.id} value={tool.id}>
                    {tool.serverName || tool.serverId}/{tool.name}
                  </option>
                ))
              )}
            </select>
            <textarea
              aria-label="MCP arguments"
              disabled={mcpTools.length === 0}
              rows={3}
              value={mcpArguments}
              onChange={(event) => setMCPArguments(event.target.value)}
            />
            <button disabled={!mcpToolId} type="submit">
              Run
            </button>
          </form>
          {mcpError && <p className="workspace-error">{mcpError}</p>}
          <div className="agent-card-list two-column">
            {(agentStatus?.mcpServers ?? []).map((server) => (
              <div className="agent-card read-only" key={server.id}>
                <div>
                  <strong>{server.name}</strong>
                  <span>{server.command || server.state}</span>
                </div>
              </div>
            ))}
            {(agentStatus?.mcpTools ?? []).map((tool) => (
              <div className="agent-card read-only" key={tool.id}>
                <div>
                  <strong>{tool.name}</strong>
                  <span>{tool.description || tool.serverName || tool.serverId}</span>
                  {tool.inputSchema && <span>{tool.inputSchema}</span>}
                </div>
              </div>
            ))}
            {(agentStatus?.mcpResources ?? []).map((resource) => (
              <div className="agent-card read-only" key={resource.id}>
                <div>
                  <strong>{resource.name}</strong>
                  <span>{resource.description || resource.uri}</span>
                </div>
              </div>
            ))}
            {(agentStatus?.mcpPrompts ?? []).map((prompt) => (
              <div className="agent-card read-only" key={prompt.id}>
                <div>
                  <strong>{prompt.name}</strong>
                  <span>{prompt.description || prompt.serverName || prompt.serverId}</span>
                </div>
              </div>
            ))}
            {(agentStatus?.mcpCalls ?? []).slice(0, 3).map((call) => (
              <div className="agent-card read-only" key={call.id}>
                <div>
                  <strong>{call.name || call.toolId}</strong>
                  <span>{call.state}{call.error ? ` · ${call.error}` : ''}</span>
                </div>
              </div>
            ))}
            {(agentStatus?.mcpSubscriptions ?? []).slice(0, 3).map((subscription) => (
              <div className="agent-card" key={subscription.id}>
                <div>
                  <strong>{subscription.uri}</strong>
                  <span>{subscription.state}{subscription.error ? ` · ${subscription.error}` : ''}</span>
                </div>
                <button
                  disabled={subscription.state !== 'active'}
                  type="button"
                  onClick={() => onUnsubscribeMCPResource(subscription.id)}
                >
                  Unsubscribe
                </button>
              </div>
            ))}
            {(agentStatus?.mcpEvents ?? []).slice(0, 3).map((event) => (
              <div className="agent-card read-only" key={event.id}>
                <div>
                  <strong>{event.method}</strong>
                  <span>{event.uri || event.serverId}{event.error ? ` · ${event.error}` : ''}</span>
                </div>
              </div>
            ))}
            {(agentStatus?.mcpServers ?? []).length === 0 &&
              (agentStatus?.mcpTools ?? []).length === 0 &&
              (agentStatus?.mcpResources ?? []).length === 0 &&
              (agentStatus?.mcpPrompts ?? []).length === 0 && <p>No MCP entries</p>}
          </div>
        </section>

        <section className="details-list workspace-review">
          <div className="details-list-header">
            <h3>Workspace</h3>
            <span>{workspaceOn ? 'On' : 'Off'}</span>
          </div>
          <form
            className="workspace-root"
            onSubmit={(event) => {
              event.preventDefault();
              onWorkspaceChange(workspaceRootInput);
            }}
          >
            <input
              aria-label="Workspace path"
              placeholder="Workspace path"
              value={workspaceRootInput}
              onChange={(event) => setWorkspaceRootInput(event.target.value)}
            />
            <button type="submit">Use</button>
          </form>
          <form className="workspace-search" onSubmit={searchWorkspace}>
            <input
              aria-label="Search workspace"
              disabled={!workspaceOn}
              placeholder="Search files"
              value={workspaceQuery}
              onChange={(event) => setWorkspaceQuery(event.target.value)}
            />
            <button disabled={!workspaceOn || isWorkspaceLoading || workspaceQuery.trim().length < 2} type="submit">
              Search
            </button>
            <button
              disabled={!workspaceOn || isWorkspaceLoading}
              type="button"
              onClick={() => void searchWorkspaceSymbols()}
            >
              Symbols
            </button>
            <button
              disabled={!workspaceOn || isWorkspaceLoading || !workspaceQuery.trim()}
              type="button"
              onClick={() => void searchWorkspaceReferences()}
            >
              References
            </button>
          </form>
          {workspaceError && <p className="workspace-error">{workspaceError}</p>}
          <div className="workspace-layout">
            <div className="workspace-results" role="list">
              {workspaceMode === 'symbols' && workspaceSymbols.length > 0 ? (
                workspaceSymbols.map((symbol) => (
                  <button
                    className={workspaceFile?.path === symbol.path ? 'workspace-result active' : 'workspace-result'}
                    key={`${symbol.path}-${symbol.line}-${symbol.kind}-${symbol.name}`}
                    type="button"
                    onClick={() => {
                      setWorkspaceFocusedLine(symbol.line);
                      void readWorkspaceFile(symbol.path);
                    }}
                  >
                    <span>{symbol.path}</span>
                    <small>
                      {symbol.kind} {symbol.name} · {symbol.line}
                    </small>
                  </button>
                ))
              ) : workspaceMode === 'references' && workspaceReferences.length > 0 ? (
                workspaceReferences.map((reference, index) => (
                  <button
                    className={workspaceFile?.path === reference.path ? 'workspace-result active' : 'workspace-result'}
                    key={`${reference.path}-${reference.line}-${reference.name}-${index}`}
                    type="button"
                    onClick={() => {
                      setWorkspaceFocusedLine(reference.line);
                      void readWorkspaceFile(reference.path);
                    }}
                  >
                    <span>{reference.path}</span>
                    <small>
                      {reference.line}: {reference.text}
                    </small>
                  </button>
                ))
              ) : workspaceMode === 'search' && workspaceResults.length > 0 ? (
                workspaceResults.map((result, index) => (
                  <button
                    className={workspaceFile?.path === result.path ? 'workspace-result active' : 'workspace-result'}
                    key={`${result.path}-${result.line}-${index}`}
                    type="button"
                    onClick={() => {
                      setWorkspaceFocusedLine(result.line);
                      void readWorkspaceFile(result.path);
                    }}
                  >
                    <span>{result.path}</span>
                    <small>
                      {result.line}: {result.text}
                    </small>
                  </button>
                ))
              ) : (
                <p>{workspaceEmptyText}</p>
              )}
            </div>
            <div className="workspace-file">
              {workspaceFile ? (
                <>
                  <div className="workspace-file-meta">
                    <strong>{workspaceFile.path}</strong>
                    <span>
                      {formatBytes(workspaceFile.size)}
                      {workspaceFile.truncated ? ' · truncated' : ''}
                    </span>
                  </div>
                  <CodeBlock
                    code={workspaceFile.content}
                    focusedLine={workspaceFocusedLine ?? undefined}
                    language={languageFromPath(workspaceFile.path)}
                  />
                </>
              ) : (
                <p>Select a result to read a file.</p>
              )}
            </div>
          </div>
        </section>

        <section className="details-list proposal-review">
          <div className="details-list-header">
            <h3>Edit proposals</h3>
            <span>{editProposals.length}</span>
          </div>
          <form className="agent-manual-form stacked" onSubmit={submitProposal}>
            <input
              aria-label="Proposal path"
              placeholder="Path"
              value={proposalPathInput}
              onChange={(event) => setProposalPathInput(event.target.value)}
            />
            <input
              aria-label="Proposal summary"
              placeholder="Summary"
              value={proposalSummaryInput}
              onChange={(event) => setProposalSummaryInput(event.target.value)}
            />
            <textarea
              aria-label="Proposal content"
              placeholder="Proposed file content"
              rows={4}
              value={proposalContentInput}
              onChange={(event) => setProposalContentInput(event.target.value)}
            />
            <button disabled={!proposalPathInput.trim()} type="submit">
              Create
            </button>
          </form>
          {editProposals.length > 0 ? (
            <div className="proposal-layout">
              <div className="proposal-list" role="list">
                {editProposals.map((proposal) => (
                  <button
                    aria-pressed={selectedProposal?.id === proposal.id}
                    className={selectedProposal?.id === proposal.id ? 'proposal-item active' : 'proposal-item'}
                    key={proposal.id}
                    type="button"
                    onClick={() => setSelectedProposalId(proposal.id)}
                  >
                    <span>{proposal.path}</span>
                    <small>{proposal.summary || proposal.status}</small>
                    <strong>{proposal.status}</strong>
                  </button>
                ))}
              </div>
              {selectedProposal && (
                <div className="proposal-detail">
                  <div className="proposal-detail-top">
                    <div>
                      <strong>{selectedProposal.path}</strong>
                      <p>{selectedProposal.summary || formatDateTime(selectedProposal.createdAt)}</p>
                    </div>
                    <div className="proposal-actions">
                      <button
                        disabled={selectedProposal.status !== 'pending'}
                        type="button"
                        onClick={() => onReviewProposal(selectedProposal.id, 'approved')}
                      >
                        Approve
                      </button>
                      <button
                        disabled={selectedProposal.status !== 'pending'}
                        type="button"
                        onClick={() => onReviewProposal(selectedProposal.id, 'rejected')}
                      >
                        Reject
                      </button>
                      {selectedProposal.status === 'approved' && (
                        <button type="button" onClick={() => onApplyProposal(selectedProposal.id)}>
                          Apply
                        </button>
                      )}
                    </div>
                  </div>
                  {selectedProposal.reviewDetail && <p className="proposal-review-detail">{selectedProposal.reviewDetail}</p>}
                  <div className="proposal-diff" aria-label="Proposal diff">
                    {selectedProposal.diff.map((line, index) => (
                      <div className={`proposal-diff-line ${line.type}`} key={`${index}-${line.type}`}>
                        <span>{diffLineNumber(line)}</span>
                        <code>{line.text || ' '}</code>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          ) : (
            <p>No edit proposals</p>
          )}
        </section>

        <DetailsList
          empty="No diagnostics"
          items={diagnostics}
          title="Diagnostics"
          render={(diagnostic) =>
            `${diagnostic.path}:${diagnostic.line}:${diagnostic.column} ${diagnostic.severity}: ${diagnostic.message}`
          }
        />
        <DetailsList
          empty="No subagent runs"
          items={agentStatus?.subagentRuns ?? []}
          title="Subagent runs"
          render={(run) => `${run.subagentId}: ${run.state} · ${run.summary}`}
        />
        <section className="details-list">
          <div className="details-list-header">
            <h3>Recent traces</h3>
            <span>{agentStatus?.traceEvents?.length ?? 0}</span>
          </div>
          <form className="agent-manual-form" onSubmit={submitTrace}>
            <input
              aria-label="Trace event"
              placeholder="Event"
              value={traceEventInput}
              onChange={(event) => setTraceEventInput(event.target.value)}
            />
            <select aria-label="Trace state" value={traceStateInput} onChange={(event) => setTraceStateInput(event.target.value)}>
              <option value="completed">completed</option>
              <option value="ready">ready</option>
              <option value="failed">failed</option>
              <option value="attention">attention</option>
            </select>
            <input
              aria-label="Trace detail"
              placeholder="Detail"
              value={traceDetailInput}
              onChange={(event) => setTraceDetailInput(event.target.value)}
            />
            <button disabled={!traceEventInput.trim() || !traceStateInput.trim()} type="submit">
              Add
            </button>
          </form>
          {(agentStatus?.traceEvents ?? []).length > 0 ? (
            (agentStatus?.traceEvents ?? []).map((trace, index) => (
              <p key={`${trace.id}-${index}`}>
                {trace.event}: {trace.state}
                {trace.detail ? ` (${trace.detail})` : ''}
              </p>
            ))
          ) : (
            <p>No traces</p>
          )}
        </section>

        <section className="details-list">
          <div className="details-list-header">
            <h3>Users</h3>
            <span>{users.length}</span>
          </div>
          <form className="agent-manual-form" onSubmit={submitUser}>
            <input
              aria-label="User email"
              placeholder="Email"
              type="email"
              value={userEmailInput}
              onChange={(event) => setUserEmailInput(event.target.value)}
            />
            <input
              aria-label="User name"
              placeholder="Name"
              value={userNameInput}
              onChange={(event) => setUserNameInput(event.target.value)}
            />
            <button disabled={!userEmailInput.trim()} type="submit">
              Add
            </button>
          </form>
          {users.length > 0 ? (
            users.map((user) => (
              <p key={user.id}>
                {user.email}
                {user.name ? ` (${user.name})` : ''}
              </p>
            ))
          ) : (
            <p>No users</p>
          )}
        </section>

        {(agentStatus?.backgroundJobs ?? []).length > 0 && (
          <section className="details-list">
            <div className="details-list-header">
              <h3>Background jobs</h3>
              <span>{agentStatus?.backgroundJobs?.length ?? 0}</span>
            </div>
            {(agentStatus?.backgroundJobs ?? []).map((job) => (
              <div key={job.id} className="agent-card">
                <div className="agent-card-header">
                  <strong>{job.goal}</strong>
                  <span className={`agent-loop-badge ${job.state}`}>{job.state}</span>
                </div>
                {job.summary && <p className="agent-card-summary">{job.summary}</p>}
                <div className="agent-card-actions">
                  {job.state === 'running' && (
                    <button type="button" onClick={() => onCancelBackgroundJob(job.id)}>Cancel</button>
                  )}
                </div>
              </div>
            ))}
          </section>
        )}
        <DetailsList
          empty="No blocked checks"
          items={blockedChecks}
          title="Blocked checks"
          render={(check) => `${check.command}: ${check.reason}`}
        />
        <DetailsList
          empty="No command runs"
          items={agentStatus?.commandRuns ?? []}
          title="Command runs"
          render={(run) => formatCommandRun(run)}
        />
        <DetailsList
          empty="No runs"
          items={agentRuns.slice(0, 5)}
          title="Latest runs"
          render={(run) => `${run.state} · ${new Date(run.createdAt).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })}`}
        />
      </section>
      {unrestrictedConfirm && (
        <ConfirmDialog
          title="Enable unrestricted mode"
          detail="The agent will have full terminal autonomy and can run any command without blocklist checks. Only destructive, secret, and billing actions will still require approval. Continue?"
          action="Enable"
          onCancel={() => setUnrestrictedConfirm(null)}
          onConfirm={() => { setUnrestrictedConfirm(null); onSetUnrestricted(true); }}
        />
      )}
    </div>
  );
}

function diffLineNumber(line: AgentDiffLine) {
  if (line.type === 'add') {
    return line.newLine ? `+${line.newLine}` : '+';
  }
  if (line.type === 'remove') {
    return line.oldLine ? `-${line.oldLine}` : '-';
  }
  return String(line.newLine ?? line.oldLine ?? '');
}

function formatDateTime(value: string) {
  return new Date(value).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
}

function formatBytes(value: number) {
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function languageFromPath(path: string) {
  const extension = path.split('.').pop()?.toLowerCase();
  switch (extension) {
    case 'html':
    case 'htm':
      return 'html';
    case 'css':
      return 'css';
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
      return 'javascript';
    case 'ts':
    case 'tsx':
      return 'typescript';
    case 'json':
      return 'json';
    case 'go':
      return 'go';
    case 'md':
      return 'markdown';
    default:
      return extension;
  }
}

function formatCommandRun(run: NonNullable<AgentStatus['commandRuns']>[number]) {
  const output = run.output.trim().replace(/\s+/g, ' ');
  const preview = output ? ` · ${truncateText(output, 90)}` : '';
  return `${run.command} · exit ${run.exitCode}${preview}`;
}

type AgentTimelineItem = {
  id: string;
  kind: string;
  title: string;
  state: string;
  detail?: string;
  createdAt: string;
  children?: Array<{
    id: string;
    title: string;
    state: string;
    detail?: string;
  }>;
};

function buildAgentTimeline(agentStatus: AgentStatus | null, editProposals: AgentEditProposal[]): AgentTimelineItem[] {
  if (!agentStatus) {
    return [];
  }
  const items: AgentTimelineItem[] = [];
  for (const loop of agentStatus.agentLoops ?? []) {
    items.push({
      id: `loop-${loop.id}`,
      kind: 'loop',
      title: loop.goal,
      state: loop.state,
      detail: loop.summary,
      createdAt: loop.updatedAt || loop.createdAt,
      children: loop.steps.map((step) => ({
        id: step.id,
        title: step.toolId || step.kind || step.title,
        state: step.state,
        detail: [step.title, step.detail, step.command].filter(Boolean).join(' · '),
      })),
    });
  }
  for (const run of agentStatus.commandRuns ?? []) {
    items.push({
      id: `command-run-${run.id}`,
      kind: 'command',
      title: run.command,
      state: run.exitCode === 0 ? 'completed' : 'failed',
      detail: formatCommandRun(run),
      createdAt: run.createdAt,
    });
  }
  for (const check of agentStatus.commandChecks ?? []) {
    items.push({
      id: `command-check-${check.id}`,
      kind: 'check',
      title: check.command,
      state: check.allowed ? 'allowed' : 'blocked',
      detail: check.reason,
      createdAt: check.createdAt || new Date(0).toISOString(),
    });
  }
  for (const approval of agentStatus.commandApprovals ?? []) {
    items.push({
      id: `approval-${approval.id}`,
      kind: 'approval',
      title: approval.command,
      state: approval.state,
      detail: approval.detail,
      createdAt: approval.createdAt,
    });
  }
  for (const run of agentStatus.hookRuns ?? []) {
    items.push({
      id: `hook-${run.id}`,
      kind: 'hook',
      title: run.hookId,
      state: run.state,
      detail: run.detail,
      createdAt: run.createdAt,
    });
  }
  for (const run of agentStatus.skillRuns ?? []) {
    items.push({
      id: `skill-${run.id}`,
      kind: 'skill',
      title: run.skillId,
      state: run.state,
      detail: run.detail,
      createdAt: run.createdAt,
    });
  }
  for (const run of agentStatus.subagentRuns ?? []) {
    items.push({
      id: `subagent-${run.id}`,
      kind: 'subagent',
      title: run.subagentId,
      state: run.state,
      detail: run.summary,
      createdAt: run.createdAt,
    });
  }
  for (const call of agentStatus.mcpCalls ?? []) {
    items.push({
      id: `mcp-${call.id}`,
      kind: 'mcp',
      title: call.name || call.toolId,
      state: call.state,
      detail: call.error || call.output,
      createdAt: call.createdAt,
    });
  }
  for (const proposal of editProposals) {
    items.push({
      id: `proposal-${proposal.id}`,
      kind: 'proposal',
      title: proposal.path,
      state: proposal.status,
      detail: proposal.summary || proposal.reviewDetail,
      createdAt: proposal.reviewedAt || proposal.createdAt,
    });
  }
  for (const trace of agentStatus.traceEvents ?? []) {
    items.push({
      id: `trace-${trace.id}`,
      kind: 'trace',
      title: trace.event,
      state: trace.state,
      detail: trace.detail,
      createdAt: trace.createdAt,
    });
  }
  return items
    .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
    .slice(0, 12);
}

function truncateText(value: string, maxLength: number) {
  if (value.length <= maxLength) {
    return value;
  }
  return `${value.slice(0, Math.max(0, maxLength - 3))}...`;
}

type AgentLoopItem = NonNullable<AgentStatus['agentLoops']>[number];

function AgentLoopCard({
  continueInput,
  editProposals,
  loop,
  onApplyProposal,
  onApproveCommand,
  onCancelLoop,
  onContinueLoop,
  onReviewProposal,
  onSelectProposal,
}: {
  continueInput: Omit<AgentLoopRequest, 'goal'>;
  editProposals: AgentEditProposal[];
  loop: AgentLoopItem;
  onApplyProposal: (proposalId: string) => void;
  onApproveCommand: (command: string) => void;
  onCancelLoop: (loopId: string) => void;
  onContinueLoop: (loopId: string, input: Omit<AgentLoopRequest, 'goal'>) => void;
  onReviewProposal: (proposalId: string, status: 'approved' | 'rejected') => void;
  onSelectProposal: (proposalId: string) => void;
}) {
  const proposal = findLoopProposal(loop, editProposals);
  const commandStep = [...loop.steps]
    .reverse()
    .find((step) => step.kind === 'command_approval' && step.state === 'waiting_approval' && step.command);
  const editBoundaryStep = [...loop.steps]
    .reverse()
    .find((step) => step.kind === 'edit_boundary' && step.state === 'waiting_approval');
  const canContinue = loop.state !== 'completed' && loop.state !== 'canceled';
  const needsProposalInput = Boolean(editBoundaryStep && !proposal && !continueInput.proposalPath);
  const nextAction = loopNextAction(proposal, commandStep, editBoundaryStep);

  return (
    <div className="agent-loop-card">
      <div className="agent-loop-top">
        <strong>{loop.goal}</strong>
        <span>
          {loop.mode ?? 'guided'} · {loop.state}
        </span>
      </div>
      <p>{loop.summary}</p>
      <div className="agent-loop-steps">
        {loop.steps.slice(0, 5).map((step) => (
          <span key={step.id}>
            {step.title}: {step.state}
            {step.detail ? ` · ${step.detail}` : ''}
          </span>
        ))}
      </div>
      {loop.previewUrl && (
        <div className="agent-loop-actions">
          <a className="agent-loop-link" href={loop.previewUrl} rel="noreferrer" target="_blank">
            Preview
          </a>
        </div>
      )}
      {(proposal || commandStep || editBoundaryStep) && (
        <div className="agent-loop-next">
          {nextAction && <strong>{nextAction}</strong>}
          {editBoundaryStep && !proposal && <span>{editBoundaryStep.detail}</span>}
          {proposal && (
            <>
              <span>
                Proposal · {proposal.path} · {proposal.status}
              </span>
              <div className="agent-loop-actions">
                <button type="button" onClick={() => onSelectProposal(proposal.id)}>
                  Review
                </button>
                <button
                  disabled={proposal.status !== 'pending'}
                  type="button"
                  onClick={() => onReviewProposal(proposal.id, 'approved')}
                >
                  Approve
                </button>
                {proposal.status === 'approved' && (
                  <button type="button" onClick={() => onApplyProposal(proposal.id)}>
                    Apply
                  </button>
                )}
              </div>
            </>
          )}
          {commandStep?.command && (
            <>
              <span>Command · {commandStep.command}</span>
              <div className="agent-loop-actions">
                <button type="button" onClick={() => onApproveCommand(commandStep.command ?? '')}>
                  Approve
                </button>
              </div>
            </>
          )}
        </div>
      )}
      {canContinue && (
        <div className="agent-loop-actions">
          <button disabled={needsProposalInput} type="button" onClick={() => onContinueLoop(loop.id, continueInput)}>
            {needsProposalInput ? 'Needs proposal' : 'Continue'}
          </button>
          <button type="button" onClick={() => onCancelLoop(loop.id)}>
            Cancel
          </button>
        </div>
      )}
    </div>
  );
}

function loopNextAction(
  proposal: AgentEditProposal | undefined,
  commandStep: AgentLoopItem['steps'][number] | undefined,
  editBoundaryStep: AgentLoopItem['steps'][number] | undefined,
) {
  if (editBoundaryStep && !proposal) {
    return 'Next · provide proposal';
  }
  if (proposal?.status === 'pending') {
    return 'Next · review proposal';
  }
  if (proposal?.status === 'approved') {
    return 'Next · apply proposal';
  }
  if (commandStep?.command) {
    return 'Next · approve command';
  }
  return '';
}

function findLoopProposal(loop: AgentLoopItem, proposals: AgentEditProposal[]) {
  const proposalIDs = new Set(
    loop.steps
      .filter((step) => step.kind === 'edit_proposal' || step.kind === 'edit_review')
      .flatMap((step) => (step.createdId ? [step.createdId] : [])),
  );
  return proposals.find((proposal) => proposalIDs.has(proposal.id));
}

function DetailsSection({ children, title }: { children: React.ReactNode; title: string }) {
  return (
    <section className="details-section">
      <h3>{title}</h3>
      {children}
    </section>
  );
}

function DetailLine({ label, value }: { label: string; value: string }) {
  return (
    <div className="detail-line">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function DetailsList<T>({ empty, items, render, title }: { empty: string; items: T[]; render: (item: T) => string; title: string }) {
  return (
    <section className="details-list">
      <h3>{title}</h3>
      {items.length > 0 ? (
        items.slice(0, 5).map((item, index) => <p key={index}>{render(item)}</p>)
      ) : (
        <p>{empty}</p>
      )}
    </section>
  );
}

function providerStatusText(provider: ProviderStatus) {
  if (provider.enabled) {
    return provider.model ?? 'Ready';
  }
  if (provider.state === 'sleeping') {
    return 'Sleeping';
  }
  return 'Off';
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
  const [appVersion, setAppVersion] = useState('...');

  useEffect(() => {
    let cancelled = false;
    fetch('/api/version')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (!cancelled && data?.version) {
          setAppVersion(data.version);
        }
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, []);

  return (
    <div className="theme-panel" aria-label="Theme settings">
      <span className="theme-panel-title">Theme</span>
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
      <span className="theme-panel-title">Style</span>
      <button
        aria-pressed={prefs.visualStyle === 'modern'}
        className={prefs.visualStyle === 'modern' ? 'active' : ''}
        type="button"
        onClick={() => onChange((current) => ({ ...current, visualStyle: 'modern' }))}
      >
        <Palette size={13} strokeWidth={ICON_STROKE} />
        Modern
      </button>
      <button
        aria-pressed={prefs.visualStyle === 'classic'}
        className={prefs.visualStyle === 'classic' ? 'active' : ''}
        type="button"
        onClick={() => onChange((current) => ({ ...current, visualStyle: 'classic' }))}
      >
        <Layers size={13} strokeWidth={ICON_STROKE} />
        Classic
      </button>
      <span className="theme-panel-title">Output</span>
      <label>
        <input
          checked={prefs.showResponseDetails}
          type="checkbox"
          onChange={(event) => onChange((current) => ({ ...current, showResponseDetails: event.target.checked }))}
        />
        <CheckBoxMark checked={prefs.showResponseDetails} />
        Model info
      </label>
      <span className="theme-panel-title">Comfort</span>
      <label>
        <input
          checked={prefs.showComposerShimmer}
          type="checkbox"
          onChange={(event) => onChange((current) => ({ ...current, showComposerShimmer: event.target.checked }))}
        />
        <CheckBoxMark checked={prefs.showComposerShimmer} />
        Input glow
      </label>
      <label>
        <input
          checked={prefs.showScrollCue}
          type="checkbox"
          onChange={(event) => onChange((current) => ({ ...current, showScrollCue: event.target.checked }))}
        />
        <CheckBoxMark checked={prefs.showScrollCue} />
        Jump to bottom
      </label>
      <label>
        <input
          checked={prefs.showHeaderShadow}
          type="checkbox"
          onChange={(event) => onChange((current) => ({ ...current, showHeaderShadow: event.target.checked }))}
        />
        <CheckBoxMark checked={prefs.showHeaderShadow} />
        Header shadow
      </label>
      <span className="theme-panel-title">About {appVersion}</span>
      <a
        className="about-report-link"
        href={reportUrl(appVersion)}
        target="_blank"
        rel="noopener noreferrer"
      >
        Report a problem
        <ArrowUpRight size={10} strokeWidth={2} />
      </a>
    </div>
  );
}

interface Connection {
  id: string;
  name: string;
  connected: boolean;
  scopes?: string[];
}

function ConnectionsPanel({
  connections,
  onRefresh,
  onClose,
}: {
  connections: Connection[] | null;
  onRefresh: () => void;
  onClose: () => void;
}) {
  const [connecting, setConnecting] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState<string | null>(null);

  return (
    <div className="connections-panel" aria-label="Connections">
      <span className="connections-panel-title">Connections</span>
      {connections === null ? (
        <div className="connections-loading">Loading...</div>
      ) : connections.length === 0 ? (
        <div className="connections-empty">No integrations configured.</div>
      ) : (
        connections.map((conn) => (
          <div className={`connection-card ${conn.connected ? '' : 'disconnected'}`} key={conn.id}>
            <div className="connection-info">
              <span className="connection-name">{conn.name}</span>
              <span className="connection-status">{conn.connected ? 'Connected' : 'Not connected'}</span>
            </div>
            {conn.connected ? (
              <button
                className="connection-action"
                type="button"
                disabled={disconnecting === conn.id}
                onClick={async () => {
                  setDisconnecting(conn.id);
                  try {
                    const tokens = await request<{ id: string; provider: string }[]>('/api/oauth/tokens');
                    const token = tokens.find((t) => t.provider === conn.id);
                    if (token) {
                      await request(`/api/oauth/tokens/${token.id}`, { method: 'DELETE' });
                    }
                    onRefresh();
                  } catch {
                    // ignore
                  }
                  setDisconnecting(null);
                }}
              >
                {disconnecting === conn.id ? '...' : 'Disconnect'}
              </button>
            ) : (
              <button
                className="connection-action"
                type="button"
                disabled={connecting === conn.id}
                onClick={async () => {
                  setConnecting(conn.id);
                  try {
                    const { url } = await request<{ url: string }>(`/api/oauth/${conn.id}/auth`);
                    const popup = window.open(url, 'oauth-popup', 'width=600,height=700');
                    if (!popup) {
                      setConnecting(null);
                      return;
                    }
                    const handleMessage = (event: MessageEvent) => {
                      if (event.data?.type === 'oauth-callback') {
                        window.removeEventListener('message', handleMessage);
                        onRefresh();
                        setConnecting(null);
                      }
                    };
                    window.addEventListener('message', handleMessage);
                    const timer = setInterval(() => {
                      if (popup.closed) {
                        clearInterval(timer);
                        window.removeEventListener('message', handleMessage);
                        setConnecting((prev) => prev === conn.id ? null : prev);
                      }
                    }, 500);
                  } catch {
                    setConnecting(null);
                  }
                }}
              >
                {connecting === conn.id ? '...' : 'Connect'}
              </button>
            )}
          </div>
        ))
      )}
      <button className="connections-close" type="button" onClick={onClose}>
        Done
      </button>
    </div>
  );
}

function SettingsPanel({
  settings,
  onChange,
  variant = 'panel',
}: {
  settings: AppSettings;
  onChange: (settings: AppSettings) => void;
  variant?: 'panel' | 'details';
}) {
  const updateProvider = (providerId: string, enabled: boolean) => {
    onChange({
      providers: settings.providers.map((provider) =>
        provider.id === providerId ? { ...provider, enabled } : provider,
      ),
    });
  };

  const moveProvider = (providerId: string, direction: -1 | 1) => {
    const index = settings.providers.findIndex((provider) => provider.id === providerId);
    const nextIndex = index + direction;
    if (index < 0 || nextIndex < 0 || nextIndex >= settings.providers.length) {
      return;
    }
    const providers = [...settings.providers];
    const [provider] = providers.splice(index, 1);
    providers.splice(nextIndex, 0, provider);
    onChange({ providers });
  };

  return (
    <div className={variant === 'details' ? 'settings-panel settings-panel-inline' : 'settings-panel'} aria-label="Provider settings">
      <div className="settings-panel-title">Providers</div>
      {settings.providers.map((provider, index) => (
        <div className={!provider.configured ? 'settings-provider muted' : 'settings-provider'} key={provider.id}>
          <label>
            <input
              checked={provider.enabled}
              disabled={!provider.configured}
              type="checkbox"
              onChange={(event) => updateProvider(provider.id, event.target.checked)}
            />
            <CheckBoxMark checked={provider.enabled} disabled={!provider.configured} />
            <span>{provider.name}</span>
          </label>
          <small>{provider.configured ? provider.model || provider.role : 'Not configured'}</small>
          <div className="settings-provider-actions">
            <button
              aria-label={`Move ${provider.name} up`}
              disabled={index === 0}
              type="button"
              onClick={() => moveProvider(provider.id, -1)}
            >
              <ArrowUp size={12} strokeWidth={ICON_STROKE} />
            </button>
            <button
              aria-label={`Move ${provider.name} down`}
              disabled={index === settings.providers.length - 1}
              type="button"
              onClick={() => moveProvider(provider.id, 1)}
            >
              <ArrowDown size={12} strokeWidth={ICON_STROKE} />
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}

function CheckBoxMark({ checked, disabled = false }: { checked: boolean; disabled?: boolean }) {
  return (
    <span className={disabled ? 'checkbox-mark disabled' : 'checkbox-mark'} aria-hidden="true">
      {checked && <Check size={10} strokeWidth={2.2} />}
    </span>
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
  const sleepingDetail = sleepingProviders.map((item) => item.detail || item.message || `${item.name} sleeping`).join(', ');
  if (!prefs.showResponseDetails) {
    return null;
  }
  return (
    <div className="response-meta">
      {provider && (
        <span className="model-badge has-tooltip tooltip-above" data-tooltip={`${provider.name} ${provider.model}`}>
          <BadgeHelp size={13} strokeWidth={ICON_STROKE} />
          {provider.model}
        </span>
      )}
      {sleepingProviders.length > 0 && (
        <span className="sleep-alert has-tooltip tooltip-above" data-tooltip={sleepingDetail || `${sleepingNames} sleeping`}>
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

function summarizeAgentResult(prefix: string, value?: string, truncated = false) {
  const parts = [prefix, value]
    .filter(Boolean)
    .map((part) => String(part).replace(/\s+/g, ' ').trim())
    .filter(Boolean);
  const summary = parts.join(': ');
  if (!summary) {
    return truncated ? 'Output truncated.' : undefined;
  }
  const capped = summary.length > 180 ? `${summary.slice(0, 177)}...` : summary;
  return truncated && !capped.endsWith('...') ? `${capped}...` : capped;
}

function formatAgentActivityDetail(value?: string) {
  const text = String(value ?? '').trim();
  if (!text) {
    return undefined;
  }
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text.includes('\n') || text.length > 120 ? text : undefined;
  }
}

function loopActivityState(state: string): AgentActivityState {
  if (state === 'failed') {
    return 'failed';
  }
  if (state === 'waiting' || state === 'waiting_approval' || state === 'waiting_input') {
    return 'waiting';
  }
  if (state === 'attention') {
    return 'blocked';
  }
  return 'completed';
}

function summarizeJSON(value: unknown) {
  try {
    const text = JSON.stringify(value);
    if (!text || text === '{}') {
      return '';
    }
    return text.length > 120 ? `${text.slice(0, 117)}...` : text;
  } catch {
    return '';
  }
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
          className={`feedback-button has-tooltip ${selected === id ? 'active' : ''}`}
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

function defaultResponseProvider(status: SystemStatus | null): ProviderStatus | undefined {
  return (
    status?.providers.find((provider) => provider.enabled && provider.role === 'primary') ??
    status?.providers.find((provider) => provider.enabled)
  );
}

function storeTitleFromMessages(messages: Message[]) {
  const firstUserMessage = messages.find((message) => message.role === 'user' && message.content.trim());
  return storeTitleFromContent(firstUserMessage?.content ?? 'Untitled');
}

function storeTitleFromContent(value: string) {
  const title = value.trim().replace(/\s+/g, ' ');
  if (!title) {
    return 'Untitled';
  }
  return title.length > 60 ? `${title.slice(0, 57).trim()}...` : title;
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

function hasDraggedFiles(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types).includes('Files');
}

function isAcceptedAttachment(file: File) {
  if (ACCEPTED_ATTACHMENT_TYPES.has(file.type)) {
    return true;
  }
  const extension = file.name.split('.').pop()?.toLowerCase();
  return extension ? ACCEPTED_ATTACHMENT_EXTENSIONS.has(extension) : false;
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
    const hasLegacyResponsePrefs =
      typeof parsed.showModelBadge === 'boolean' ||
      typeof parsed.showSleepAlert === 'boolean' ||
      typeof parsed.showReactions === 'boolean';
    const showResponseDetails =
      typeof parsed.showResponseDetails === 'boolean'
        ? parsed.showResponseDetails
        : hasLegacyResponsePrefs
          ? parsed.showModelBadge !== false || parsed.showSleepAlert !== false || parsed.showReactions !== false
          : false;
    return {
      showComposerShimmer: true,
      showScrollCue: true,
      showHeaderShadow: true,
      ...parsed,
      showResponseDetails,
      visualStyle: isVisualStyle(parsed.visualStyle) ? parsed.visualStyle : 'modern',
      theme: isThemeChoice(parsed.theme) ? parsed.theme : 'system',
    };
  } catch {
    return {
      showResponseDetails: false,
      showComposerShimmer: true,
      showScrollCue: true,
      showHeaderShadow: true,
      visualStyle: 'modern',
      theme: 'system',
    };
  }
}

function saveUIPrefs(prefs: UIPrefs) {
  window.localStorage.setItem(
    UI_PREFS_STORAGE_KEY,
    JSON.stringify({
      showResponseDetails: prefs.showResponseDetails,
      showComposerShimmer: prefs.showComposerShimmer,
      showScrollCue: prefs.showScrollCue,
      showHeaderShadow: prefs.showHeaderShadow,
      visualStyle: prefs.visualStyle,
      theme: prefs.theme,
    }),
  );
}

function isThemeChoice(value: unknown): value is ThemeChoice {
  return value === 'dark' || value === 'light' || value === 'system';
}

function isVisualStyle(value: unknown): value is VisualStyle {
  return value === 'modern' || value === 'classic';
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

function normalizeCommand(value: string) {
  return value.trim().replace(/\s+/g, ' ');
}

function isAutonomousLoopMode(mode?: string) {
  return mode === 'auto' || mode === 'developer';
}

function findApprovedCommandApproval(
  approvals: NonNullable<AgentStatus['commandApprovals']>,
  command: string,
) {
  if (!command) {
    return undefined;
  }
  return approvals.find((approval) => approval.state === 'approved' && normalizeCommand(approval.command) === command);
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);

if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch((err) => {
      console.error('Service worker registration failed:', err);
    });
  });
}
