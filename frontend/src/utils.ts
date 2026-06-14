export type ThemeChoice = 'dark' | 'light' | 'system';
export type ResolvedTheme = 'dark' | 'light';
export type VisualStyle = 'modern' | 'classic';

export function resolveTheme(theme: ThemeChoice, systemTheme: ResolvedTheme): ResolvedTheme {
  return theme === 'system' ? systemTheme : theme;
}

export function isThemeChoice(value: unknown): value is ThemeChoice {
  return value === 'dark' || value === 'light' || value === 'system';
}

export function isVisualStyle(value: unknown): value is VisualStyle {
  return value === 'modern' || value === 'classic';
}

export function normalizeCommand(value: string) {
  return value.trim().replace(/\s+/g, ' ');
}

export function isAutonomousLoopMode(mode?: string) {
  return mode === 'auto' || mode === 'developer';
}

export function hostFromURL(value: string) {
  try {
    return new URL(value).hostname.replace(/^www\./, '');
  } catch {
    return '';
  }
}

export function storeTitleFromContent(value: string) {
  const title = value.trim().replace(/\s+/g, ' ');
  if (!title) {
    return 'Untitled';
  }
  return title.length > 60 ? `${title.slice(0, 57).trim()}...` : title;
}

export function formatBytes(value: number) {
  if (value < 1024) {
    return `${value} B`;
  }
  if (value < 1024 * 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

export function languageFromPath(path: string) {
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

export function loopActivityState(state: string): string {
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

export function summarizeJSON(value: unknown) {
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

export function summarizeAgentResult(prefix: string, value?: string, truncated = false) {
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

export function formatAgentActivityDetail(value?: string) {
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

export function tidyUrlLabel(url: string): string {
  return url.replace(/^https?:\/\//, '').replace(/\/$/, '').replace(/^www\./, '');
}

export function formatDateTime(value: string) {
  return new Date(value).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
}

export function formatDate(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
  }).format(new Date(value));
}

export function truncateText(value: string, maxLength: number) {
  if (value.length <= maxLength) {
    return value;
  }
  return `${value.slice(0, maxLength)}...`;
}

export function codeMeta(language: string | undefined, lineCount: number, byteCount: number) {
  return [language, `${lineCount} lines`, `${byteCount} bytes`].filter(Boolean).join(' · ');
}

export function cleanMarkdownText(value: string): string {
  return value.replace(/\s+/g, ' ').trim();
}

export function isSafeMarkdownHref(href: string) {
  try {
    const url = new URL(href, 'https://example.com');
    return url.protocol === 'https:' || url.protocol === 'http:' || url.protocol === 'mailto:';
  } catch {
    return false;
  }
}

export function diffLineNumber(line: { type: string; oldLine?: number; newLine?: number }) {
  if (line.type === 'add') {
    return line.newLine ? `+${line.newLine}` : '+';
  }
  if (line.type === 'remove') {
    return line.oldLine ? `-${line.oldLine}` : '-';
  }
  return String(line.newLine ?? line.oldLine ?? '');
}

export function statusProviderInfo(provider?: { name: string; model?: string }): { name: string; model: string } | undefined {
  if (!provider?.model) {
    return undefined;
  }
  return { name: provider.name, model: provider.model };
}

export function messageKey(message: { clientId?: string; id?: string; role: string; createdAt?: string }, index: number) {
  return message.clientId ?? message.id ?? `${message.role}-${message.createdAt ?? 'draft'}-${index}`;
}

export function getSystemTheme(): ResolvedTheme {
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

export function isNarrowViewport() {
  return window.matchMedia('(max-width: 820px)').matches;
}
