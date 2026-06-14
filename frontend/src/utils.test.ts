import { describe, expect, it } from 'vitest';
import {
  resolveTheme,
  isThemeChoice,
  isVisualStyle,
  normalizeCommand,
  isAutonomousLoopMode,
  hostFromURL,
  storeTitleFromContent,
  formatBytes,
  languageFromPath,
  loopActivityState,
  summarizeJSON,
  summarizeAgentResult,
  formatAgentActivityDetail,
  tidyUrlLabel,
  formatDateTime,
  formatDate,
  truncateText,
  codeMeta,
  cleanMarkdownText,
  isSafeMarkdownHref,
  diffLineNumber,
  statusProviderInfo,
  messageKey,
} from './utils';

describe('resolveTheme', () => {
  it('returns system theme when theme is system', () => {
    expect(resolveTheme('system', 'dark')).toBe('dark');
    expect(resolveTheme('system', 'light')).toBe('light');
  });

  it('returns the theme when not system', () => {
    expect(resolveTheme('dark', 'light')).toBe('dark');
    expect(resolveTheme('light', 'dark')).toBe('light');
  });
});

describe('isThemeChoice', () => {
  it('accepts valid themes', () => {
    expect(isThemeChoice('dark')).toBe(true);
    expect(isThemeChoice('light')).toBe(true);
    expect(isThemeChoice('system')).toBe(true);
  });

  it('rejects invalid values', () => {
    expect(isThemeChoice('blue')).toBe(false);
    expect(isThemeChoice('')).toBe(false);
    expect(isThemeChoice(undefined)).toBe(false);
    expect(isThemeChoice(null)).toBe(false);
  });
});

describe('isVisualStyle', () => {
  it('accepts valid styles', () => {
    expect(isVisualStyle('modern')).toBe(true);
    expect(isVisualStyle('classic')).toBe(true);
  });

  it('rejects invalid values', () => {
    expect(isVisualStyle('retro')).toBe(false);
    expect(isVisualStyle('')).toBe(false);
  });
});

describe('normalizeCommand', () => {
  it('trims whitespace', () => {
    expect(normalizeCommand('  echo hello  ')).toBe('echo hello');
  });

  it('collapses multiple spaces', () => {
    expect(normalizeCommand('echo   hello')).toBe('echo hello');
  });

  it('handles empty string', () => {
    expect(normalizeCommand('')).toBe('');
  });
});

describe('isAutonomousLoopMode', () => {
  it('returns true for auto and developer', () => {
    expect(isAutonomousLoopMode('auto')).toBe(true);
    expect(isAutonomousLoopMode('developer')).toBe(true);
  });

  it('returns false for guided and other modes', () => {
    expect(isAutonomousLoopMode('guided')).toBe(false);
    expect(isAutonomousLoopMode('')).toBe(false);
    expect(isAutonomousLoopMode(undefined)).toBe(false);
  });
});

describe('hostFromURL', () => {
  it('extracts hostname from URL', () => {
    expect(hostFromURL('https://www.example.com/path')).toBe('example.com');
  });

  it('strips www prefix', () => {
    expect(hostFromURL('https://www.google.com')).toBe('google.com');
  });

  it('returns empty for invalid URL', () => {
    expect(hostFromURL('not-a-url')).toBe('');
  });
});

describe('storeTitleFromContent', () => {
  it('returns Untitled for empty content', () => {
    expect(storeTitleFromContent('')).toBe('Untitled');
    expect(storeTitleFromContent('   ')).toBe('Untitled');
  });

  it('truncates long titles', () => {
    const long = 'a'.repeat(100);
    expect(storeTitleFromContent(long)).toBe(`${'a'.repeat(57)}...`);
  });

  it('returns short titles as-is', () => {
    expect(storeTitleFromContent('hello world')).toBe('hello world');
  });

  it('collapses whitespace', () => {
    expect(storeTitleFromContent('hello   world')).toBe('hello world');
  });
});

describe('formatBytes', () => {
  it('formats bytes', () => {
    expect(formatBytes(500)).toBe('500 B');
  });

  it('formats KB', () => {
    expect(formatBytes(2048)).toBe('2.0 KB');
  });

  it('formats MB', () => {
    expect(formatBytes(1048576 * 2)).toBe('2.0 MB');
  });
});

describe('languageFromPath', () => {
  it('detects common extensions', () => {
    expect(languageFromPath('file.go')).toBe('go');
    expect(languageFromPath('file.tsx')).toBe('typescript');
    expect(languageFromPath('file.js')).toBe('javascript');
    expect(languageFromPath('file.html')).toBe('html');
    expect(languageFromPath('file.css')).toBe('css');
    expect(languageFromPath('file.json')).toBe('json');
    expect(languageFromPath('file.md')).toBe('markdown');
  });

  it('returns extension for unknown types', () => {
    expect(languageFromPath('file.xyz')).toBe('xyz');
  });

  it('handles files without extension', () => {
    expect(languageFromPath('Makefile')).toBe('makefile');
  });
});

describe('loopActivityState', () => {
  it('maps failed state', () => {
    expect(loopActivityState('failed')).toBe('failed');
  });

  it('maps waiting states', () => {
    expect(loopActivityState('waiting')).toBe('waiting');
    expect(loopActivityState('waiting_approval')).toBe('waiting');
    expect(loopActivityState('waiting_input')).toBe('waiting');
  });

  it('maps attention to blocked', () => {
    expect(loopActivityState('attention')).toBe('blocked');
  });

  it('defaults to completed', () => {
    expect(loopActivityState('running')).toBe('completed');
    expect(loopActivityState('')).toBe('completed');
  });
});

describe('summarizeJSON', () => {
  it('returns empty for empty object', () => {
    expect(summarizeJSON({})).toBe('');
  });

  it('truncates long JSON', () => {
    const large = { data: 'x'.repeat(200) };
    expect(summarizeJSON(large).length).toBe(120);
  });

  it('returns short JSON as-is', () => {
    expect(summarizeJSON({ key: 'value' })).toBe('{"key":"value"}');
  });

  it('handles null', () => {
    expect(summarizeJSON(null)).toBe('null');
  });
});

describe('summarizeAgentResult', () => {
  it('combines prefix and value', () => {
    expect(summarizeAgentResult('done', 'success')).toBe('done: success');
  });

  it('handles truncated output', () => {
    expect(summarizeAgentResult('', undefined, true)).toBe('Output truncated.');
  });

  it('crops long values', () => {
    const long = 'x'.repeat(200);
    const result = summarizeAgentResult('done', long);
    expect(result?.length).toBe(180);
    expect(result?.endsWith('...')).toBe(true);
  });

  it('returns undefined for empty output', () => {
    expect(summarizeAgentResult('')).toBeUndefined();
  });
});

describe('formatAgentActivityDetail', () => {
  it('returns undefined for empty input', () => {
    expect(formatAgentActivityDetail('')).toBeUndefined();
    expect(formatAgentActivityDetail(undefined)).toBeUndefined();
  });

  it('formats JSON', () => {
    const result = formatAgentActivityDetail('{"a":1}');
    expect(result).toContain('"a"');
  });

  it('returns long text as-is', () => {
    const long = 'x'.repeat(200);
    expect(formatAgentActivityDetail(long)).toBe(long);
  });
});

describe('tidyUrlLabel', () => {
  it('strips protocol', () => {
    expect(tidyUrlLabel('https://example.com')).toBe('example.com');
  });

  it('strips trailing slash', () => {
    expect(tidyUrlLabel('https://example.com/')).toBe('example.com');
  });

  it('strips www', () => {
    expect(tidyUrlLabel('https://www.example.com')).toBe('example.com');
  });
});

describe('formatDateTime', () => {
  it('formats a date string', () => {
    const result = formatDateTime('2024-01-15T10:30:00Z');
    expect(result).toContain('2024');
  });
});

describe('formatDate', () => {
  it('formats a date string', () => {
    const result = formatDate('2024-06-15T12:00:00Z');
    expect(result).toContain('Jun');
  });
});

describe('truncateText', () => {
  it('returns short text as-is', () => {
    expect(truncateText('hello', 10)).toBe('hello');
  });

  it('truncates long text with ellipsis', () => {
    expect(truncateText('hello world', 5)).toBe('hello...');
  });
});

describe('codeMeta', () => {
  it('includes language, lines, and bytes', () => {
    expect(codeMeta('go', 10, 200)).toContain('go');
    expect(codeMeta('go', 10, 200)).toContain('10 lines');
    expect(codeMeta('go', 10, 200)).toContain('200 bytes');
  });

  it('handles undefined language', () => {
    expect(codeMeta(undefined, 5, 100)).toContain('5 lines');
  });
});

describe('cleanMarkdownText', () => {
  it('collapses whitespace', () => {
    expect(cleanMarkdownText('hello   world')).toBe('hello world');
  });

  it('trims edges', () => {
    expect(cleanMarkdownText('  hello  ')).toBe('hello');
  });
});

describe('isSafeMarkdownHref', () => {
  it('accepts https and http', () => {
    expect(isSafeMarkdownHref('https://example.com')).toBe(true);
    expect(isSafeMarkdownHref('http://example.com')).toBe(true);
  });

  it('accepts mailto', () => {
    expect(isSafeMarkdownHref('mailto:test@example.com')).toBe(true);
  });

  it('rejects javascript', () => {
    expect(isSafeMarkdownHref('javascript:alert(1)')).toBe(false);
  });
});

describe('diffLineNumber', () => {
  it('shows added lines with +', () => {
    expect(diffLineNumber({ type: 'add', newLine: 5 })).toBe('+5');
  });

  it('shows removed lines with -', () => {
    expect(diffLineNumber({ type: 'remove', oldLine: 3 })).toBe('-3');
  });

  it('shows unchanged lines', () => {
    expect(diffLineNumber({ type: 'equal', newLine: 10 })).toBe('10');
  });
});

describe('statusProviderInfo', () => {
  it('returns info when model is present', () => {
    expect(statusProviderInfo({ name: 'Gemini', model: 'gemini-2.0' })).toEqual({
      name: 'Gemini',
      model: 'gemini-2.0',
    });
  });

  it('returns undefined when model is missing', () => {
    expect(statusProviderInfo({ name: 'Gemini' })).toBeUndefined();
    expect(statusProviderInfo(undefined)).toBeUndefined();
  });
});

describe('messageKey', () => {
  it('uses clientId when available', () => {
    expect(messageKey({ clientId: 'c1', role: 'user' }, 0)).toBe('c1');
  });

  it('falls back to id', () => {
    expect(messageKey({ id: 'm1', role: 'user' }, 0)).toBe('m1');
  });

  it('generates key from role and index', () => {
    expect(messageKey({ role: 'user' }, 5)).toContain('user');
  });
});
