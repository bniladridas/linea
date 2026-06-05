#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const baseURL = process.env.LINEA_UI_URL ?? 'http://localhost:8080/';
const chromePath = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const sendMessage = process.argv.includes('--send');
const checkSearchSources = process.argv.includes('--search-sources');
const checkLightTheme = process.argv.includes('--light-theme');
const checkMobile = process.argv.includes('--mobile');
const checkAttachment = process.argv.includes('--attachment');
const checkAgentReview = process.argv.includes('--agent-review');
const port = Number(process.env.LINEA_UI_SMOKE_PORT ?? 9300 + (process.pid % 500));
const agentReviewFile = process.env.LINEA_AGENT_REVIEW_FILE ?? 'README.md';
const agentReviewSummary = 'Agent review smoke';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
let agentReviewProposalId = '';
let agentReviewProposalCreated = false;

async function main() {
  const cleanupStartedAt = new Date().toISOString();
  const profileDir = await mkdtemp(join(tmpdir(), 'linea-ui-smoke-'));
  const chrome = launchChrome(profileDir, port);

  try {
    await waitForDevTools(() => chrome.stderrText());
    if (checkAgentReview) {
      await ensureAgentReviewProposal();
    }
    const result = await runProbe();
    if (!checkMobile && (!result.bodyText.includes('Linea') || !result.bodyText.includes('New'))) {
      throw new Error(`UI did not render expected Linea text. Body text: ${JSON.stringify(result.bodyText)}`);
    }
    if (!result.sidebarToggleWorks) {
      throw new Error('Conversation sidebar toggle did not hide and restore the sidebar.');
    }
    if (!result.composerExpands) {
      throw new Error('Composer did not expand for large pasted input.');
    }
    if (!result.composerShortcutWorks) {
      throw new Error('Composer did not submit with Cmd/Ctrl+Enter.');
    }
    if (!result.systemPanelWorks) {
      throw new Error('System status panel did not open from the sidebar footer.');
    }
    if (!result.themePanelWorks) {
      throw new Error('Theme panel did not open from the sidebar footer.');
    }
    if (!result.footerPanelCloses) {
      throw new Error('Sidebar footer panels did not close on outside click.');
    }
    if (!result.sidebarCloseStateWorks) {
      throw new Error(`Sidebar close visibility is wrong. ${result.sidebarCloseStateDetail}`);
    }
    if (!result.confirmDialogWorks) {
      throw new Error('Delete confirmation did not render as an app dialog.');
    }
    if (sendMessage && !result.bodyText.toLowerCase().includes('ok')) {
      throw new Error(`UI send smoke did not render assistant response. Body text: ${JSON.stringify(result.bodyText)}`);
    }
    if (sendMessage && !result.loadingWorks) {
      throw new Error('UI send smoke did not show the generating response state.');
    }
    if (sendMessage && !result.modelBadgeWorks) {
      throw new Error('UI send smoke did not render the response model badge.');
    }
    if (checkSearchSources && !result.sourcesWork) {
      throw new Error('UI search smoke did not render the sources panel.');
    }
    if (checkAttachment && !result.attachmentWorks) {
      throw new Error('UI attachment smoke did not render the selected file.');
    }
    if (checkAgentReview && !result.agentReviewWorks) {
      throw new Error(`UI agent review smoke did not render edit proposals. ${result.agentReviewDetail}`);
    }
    if (checkLightTheme && !result.lightThemeWorks) {
      throw new Error(`Light theme still has dark leaked state. ${result.lightThemeDetail}`);
    }
    if (checkMobile && !result.mobileWorks) {
      throw new Error(`Mobile layout check failed. ${result.mobileDetail}`);
    }
    if (result.errorCount > 0) {
      throw new Error(`Browser reported ${result.errorCount} runtime error(s).`);
    }
    console.log(`PASS ui render - ${baseURL}`);
    if (sendMessage) {
      console.log('PASS ui send - assistant response rendered');
    }
    if (checkSearchSources) {
      console.log('PASS ui search sources - sources panel rendered');
    }
    if (checkLightTheme) {
      console.log('PASS ui light theme - active states are readable');
    }
    if (checkMobile) {
      console.log('PASS ui mobile - layout fits narrow viewport');
    }
    if (checkAttachment) {
      console.log('PASS ui attachment - selected file rendered');
    }
    if (checkAgentReview) {
      console.log('PASS ui agent review - edit proposal rendered');
    }
  } finally {
    await closeBrowser(port);
    chrome.kill();
    await cleanupAgentReviewProposal();
    await cleanupSmokeConversations(cleanupStartedAt);
  }
}

function launchChrome(profileDir, remotePort) {
  const args = [
    '--headless=new',
    '--disable-gpu',
    '--no-first-run',
    '--no-default-browser-check',
    `--user-data-dir=${profileDir}`,
    `--remote-debugging-port=${remotePort}`,
    'about:blank',
  ];
  const child = spawn(chromePath, args, { stdio: ['ignore', 'pipe', 'pipe'] });

  let stderr = '';
  child.stderr.on('data', (chunk) => {
    stderr += chunk.toString();
  });
  return {
    kill: () => child.kill('SIGTERM'),
    stderrText: () => stderr,
  };
}

async function closeBrowser(remotePort) {
  try {
    const version = await fetch(`http://127.0.0.1:${remotePort}/json/version`).then((res) => res.json());
    if (!version.webSocketDebuggerUrl) return;
    const ws = new WebSocket(version.webSocketDebuggerUrl);
    await new Promise((resolve) => ws.addEventListener('open', resolve, { once: true }));
    ws.send(JSON.stringify({ id: 1, method: 'Browser.close' }));
    await sleep(100);
    ws.close();
  } catch {
    // Best-effort browser cleanup.
  }
}

async function waitForDevTools(stderr) {
  const started = Date.now();
  while (Date.now() - started < 10_000) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/json/version`);
      if (res.ok) {
        return;
      }
    } catch {
      // Chrome is still starting.
    }
    await sleep(100);
  }
  const detail = stderr().trim();
  throw new Error(`Timed out waiting for Chrome DevTools.${detail ? ` Chrome stderr: ${detail.slice(-1000)}` : ''}`);
}

async function runProbe() {
  const tab = await fetch(`http://127.0.0.1:${port}/json/new?${encodeURIComponent(baseURL)}`, {
    method: 'PUT',
  }).then((res) => res.json());
  const ws = new WebSocket(tab.webSocketDebuggerUrl);
  await new Promise((resolve) => ws.addEventListener('open', resolve, { once: true }));

  let id = 0;
  const events = [];
  ws.addEventListener('message', (event) => {
    const message = JSON.parse(event.data);
    if (
      message.method === 'Runtime.exceptionThrown' ||
      message.method === 'Runtime.consoleAPICalled' ||
      message.method === 'Log.entryAdded'
    ) {
      events.push(message);
    }
  });

  const send = (method, params = {}) => new Promise((resolve, reject) => {
    const message = { id: ++id, method, params };
    const timeout = setTimeout(() => {
      ws.removeEventListener('message', onMessage);
      reject(new Error(`Timed out waiting for ${method}.`));
    }, 10_000);
    const onMessage = (event) => {
      const data = JSON.parse(event.data);
      if (data.id === message.id) {
        clearTimeout(timeout);
        ws.removeEventListener('message', onMessage);
        resolve(data);
      }
    };
    ws.addEventListener('message', onMessage);
    ws.send(JSON.stringify(message));
  });

  await send('Runtime.enable');
  await send('Log.enable');
  await send('Page.enable');
  if (checkMobile) {
    await send('Emulation.setDeviceMetricsOverride', {
      width: 390,
      height: 844,
      deviceScaleFactor: 2,
      mobile: true,
    });
  } else {
    await send('Emulation.setDeviceMetricsOverride', {
      width: 1440,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
    });
  }
  await send('Page.navigate', { url: baseURL });
  await sleep(3000);

  const sidebarInitial = await send('Runtime.evaluate', {
    expression: `(() => {
      const toggle = document.querySelector('button[aria-label="Hide conversations"], button[aria-label="Show conversations"]');
      const visible = Boolean(document.querySelector('.sidebar'));
      toggle?.click();
      return visible;
    })()`,
    returnByValue: true,
  });
  await sleep(300);
  const sidebarAfterFirstToggle = await send('Runtime.evaluate', {
    expression: `(() => {
      const visible = Boolean(document.querySelector('.sidebar'));
      document.querySelector('button[aria-label="Hide conversations"], button[aria-label="Show conversations"]')?.click();
      return visible;
    })()`,
    returnByValue: true,
  });
  await sleep(700);
  const sidebarAfterSecondToggle = await send('Runtime.evaluate', {
    expression: `Boolean(document.querySelector('.sidebar'))`,
    returnByValue: true,
  });
  await send('Runtime.evaluate', {
    expression: `(() => {
      if (!document.querySelector('.sidebar')) {
        document.querySelector('button[aria-label="Show conversations"]')?.click();
      }
      return true;
    })()`,
    returnByValue: true,
  });
  await sleep(300);
  const sidebarCloseState = await send('Runtime.evaluate', {
    expression: `(() => {
      const close = document.querySelector('.sidebar-close');
      if (!close) return { ok: false, reason: 'missing' };
      const style = getComputedStyle(close);
      return {
        ok: ${checkMobile ? 'style.display !== "none"' : 'style.display === "none"'},
        display: style.display,
      };
    })()`,
    returnByValue: true,
  });

  const composerExpands = await send('Runtime.evaluate', {
    expression: `(() => {
      const textarea = document.querySelector('textarea');
      const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set;
      const before = textarea.getBoundingClientRect().height;
      setter.call(textarea, Array.from({ length: 12 }, (_, index) => 'line ' + index).join('\\n'));
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
      const after = textarea.getBoundingClientRect().height;
      setter.call(textarea, '');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
      return after > before;
    })()`,
    returnByValue: true,
  });

  const composerShortcutWorks = await send('Runtime.evaluate', {
    expression: `(() => {
      const textarea = document.querySelector('textarea');
      if (!textarea || !textarea.placeholder.includes('⌘↵')) return false;
      const event = new KeyboardEvent('keydown', { key: 'Enter', metaKey: true, bubbles: true, cancelable: true });
      return textarea.dispatchEvent(event) === false || event.defaultPrevented;
    })()`,
    returnByValue: true,
  });

  await send('Runtime.evaluate', {
    expression: `(() => {
      const button = document.querySelector('.system-button');
      if (!button) return false;
      button.click();
      return true;
    })()`,
    returnByValue: true,
  });
  await sleep(300);
  const systemPanelWorks = await send('Runtime.evaluate', {
    expression: `Boolean(document.querySelector('.system-panel'))`,
    returnByValue: true,
  });

  let agentReviewObserved = null;
  if (checkAgentReview) {
    await send('Runtime.evaluate', {
      expression: `(() => {
        const details = Array.from(document.querySelectorAll('button')).find((button) => button.textContent.includes('Details'));
        details?.click();
        return Boolean(details);
      })()`,
      returnByValue: true,
    });
    await sleep(500);
    await send('Runtime.evaluate', {
      expression: `(() => {
        const input = document.querySelector('input[aria-label="Search workspace"]');
        if (!input) return false;
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
        setter.call(input, 'Linea');
        input.dispatchEvent(new Event('input', { bubbles: true }));
        input.closest('form')?.requestSubmit();
        return true;
      })()`,
      returnByValue: true,
    });
    await sleep(500);
    await send('Runtime.evaluate', {
      expression: `document.querySelector('.workspace-result')?.click()`,
      returnByValue: true,
    });
    await sleep(500);
    await send('Runtime.evaluate', {
      expression: `(() => {
        const setValue = (input, value) => {
          if (!input) return false;
          const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
          setter.call(input, value);
          input.dispatchEvent(new Event('input', { bubbles: true }));
          return true;
        };
        const loopForm = document.querySelector('.agent-loop-form');
        const loopGoal = loopForm?.querySelector('input[aria-label="Agent goal"]');
        const loopQuery = loopForm?.querySelector('input[aria-label="Search query"]');
        setValue(loopGoal, 'search Linea');
        setValue(loopQuery, 'Linea');
        loopForm?.requestSubmit();

        const commandForm = document.querySelector('.agent-command-form');
        const commandInput = commandForm?.querySelector('input[aria-label="Command"]');
        setValue(commandInput, 'printf ok');
        commandForm?.requestSubmit();

        const hookSection = Array.from(document.querySelectorAll('.details-list')).find((section) => section.querySelector('h3')?.textContent === 'Hooks');
        hookSection?.querySelector('.agent-card button')?.click();

        const skillSection = Array.from(document.querySelectorAll('.details-list')).find((section) => section.querySelector('h3')?.textContent === 'Skills');
        const skillButton = skillSection?.querySelector('.agent-card button:not(:disabled)');
        skillButton?.click();

        return Boolean(loopForm && commandForm && hookSection && skillSection);
      })()`,
      returnByValue: true,
    });
    await sleep(1000);
    agentReviewObserved = await send('Runtime.evaluate', {
      expression: `(() => {
        const dialog = document.querySelector('.details-dialog');
        const bodyText = dialog?.innerText ?? '';
        return {
          ok:
            Boolean(dialog) &&
            bodyText.includes('Workspace') &&
            Boolean(dialog.querySelector('.workspace-result')) &&
            Boolean(dialog.querySelector('.workspace-file .code-shell')) &&
            bodyText.includes('Edit proposals') &&
            bodyText.includes('Agent review smoke') &&
            bodyText.includes('Agent loop') &&
            Boolean(dialog.querySelector('.agent-loop-card')) &&
            bodyText.includes('Commands') &&
            bodyText.includes('Hooks') &&
            bodyText.includes('Skills') &&
            Boolean(dialog.querySelector('.proposal-diff')) &&
            Boolean(dialog.querySelector('.proposal-actions button')),
          text: bodyText.slice(0, 600),
        };
      })()`,
      returnByValue: true,
    });
    await send('Runtime.evaluate', {
      expression: `document.querySelector('.details-close')?.click()`,
      returnByValue: true,
    });
    await sleep(150);
  }

  await send('Runtime.evaluate', {
    expression: `document.querySelector('.system-button')?.click()`,
    returnByValue: true,
  });
  await sleep(150);
  await send('Runtime.evaluate', {
    expression: `document.querySelectorAll('.system-button')[1]?.click()`,
    returnByValue: true,
  });
  await sleep(300);
  const themePanelWorks = await send('Runtime.evaluate', {
    expression: `Boolean(document.querySelector('.theme-panel'))`,
    returnByValue: true,
  });
  await send('Runtime.evaluate', {
    expression: `(() => {
      document.querySelector('.chat')?.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
      return true;
    })()`,
    returnByValue: true,
  });
  await sleep(150);
  const footerPanelCloses = await send('Runtime.evaluate', {
    expression: `!document.querySelector('.system-panel') && !document.querySelector('.theme-panel')`,
    returnByValue: true,
  });
  await send('Runtime.evaluate', {
    expression: `document.querySelectorAll('.system-button')[1]?.click()`,
    returnByValue: true,
  });
  await sleep(100);
  await send('Runtime.evaluate', {
    expression: `(() => {
      const footer = document.querySelector('.sidebar-footer');
      footer?.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
      return true;
    })()`,
    returnByValue: true,
  });
  await sleep(150);
  const footerBlankCloses = await send('Runtime.evaluate', {
    expression: `!document.querySelector('.system-panel') && !document.querySelector('.theme-panel')`,
    returnByValue: true,
  });

  await send('Runtime.evaluate', {
    expression: `(() => {
      const menuButton = document.querySelector('.conversation-menu-button');
      if (!menuButton) return { ok: true, skipped: true };
      menuButton.click();
      return { ok: true, skipped: false };
    })()`,
    returnByValue: true,
  });
  await sleep(150);
  const confirmDialogObserved = await send('Runtime.evaluate', {
    expression: `(() => {
      if (!document.querySelector('.conversation-menu-button')) return { ok: true, skipped: true };
      const deleteButton = Array.from(document.querySelectorAll('.conversation-menu button')).find((button) => button.textContent.includes('Delete'));
      deleteButton?.click();
      return { clicked: Boolean(deleteButton), skipped: false };
    })()`,
    returnByValue: true,
  });
  await sleep(150);
  const confirmDialogRendered = await send('Runtime.evaluate', {
    expression: `(() => {
      const dialog = document.querySelector('.confirm-dialog');
      document.querySelector('.confirm-button')?.click();
      return { ok: Boolean(dialog), skipped: false };
    })()`,
    returnByValue: true,
  });

  let lightThemeObserved = null;
  if (checkLightTheme) {
    await send('Runtime.evaluate', {
      expression: `document.querySelectorAll('.system-button')[1]?.click()`,
      returnByValue: true,
    });
    await sleep(100);
    await send('Runtime.evaluate', {
      expression: `(() => {
        Array.from(document.querySelectorAll('.theme-panel button')).find((button) => button.textContent.includes('White'))?.click();
        return true;
      })()`,
      returnByValue: true,
    });
    await sleep(300);
    lightThemeObserved = await send('Runtime.evaluate', {
      expression: `(() => {
        const active = document.querySelector('.conversation.active');
        const newChat = document.querySelector('.new-chat');
        const systemButton = document.querySelector('.system-button');
        const badge = document.querySelector('.model-badge');
        const styles = (node) => {
          if (!node) return null;
          const style = getComputedStyle(node);
          return { background: style.backgroundColor, color: style.color, border: style.borderColor };
        };
        const isNearBlack = (rgb) => {
          const match = rgb.match(/\\d+/g)?.map(Number) ?? [];
          return match.length >= 3 && match[0] < 45 && match[1] < 45 && match[2] < 45;
        };
        const activeStyles = styles(active);
        const newChatStyles = styles(newChat);
        const systemStyles = styles(systemButton);
        const badgeStyles = styles(badge);
        return {
          ok:
            document.documentElement.dataset.theme === 'light' &&
            Boolean(activeStyles) &&
            !isNearBlack(activeStyles.background) &&
            !isNearBlack(newChatStyles?.background ?? '') &&
            !isNearBlack(systemStyles?.background ?? '') &&
            (!badgeStyles || !isNearBlack(badgeStyles.background)),
          activeStyles,
          newChatStyles,
          systemStyles,
          badgeStyles,
          theme: document.documentElement.dataset.theme,
        };
      })()`,
      returnByValue: true,
    });
  }

  let loadingObserved = null;
  let modelBadgeObserved = null;
  if (sendMessage) {
    await send('Runtime.evaluate', {
      expression: `(() => {
        ${startSavedChatExpression()}
        startSavedChat();
        const textarea = document.querySelector('textarea');
        const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set;
        setter.call(textarea, 'Reply with only ok.');
        textarea.dispatchEvent(new Event('input', { bubbles: true }));
        document.querySelector('form').requestSubmit();
        return true;
      })()`,
      returnByValue: true,
    });
    await sleep(150);
    loadingObserved = await send('Runtime.evaluate', {
      expression: `Boolean(document.querySelector('.response-loading')) || document.body.innerText.toLowerCase().includes('ok')`,
      returnByValue: true,
    });
    await sleep(7000);
    modelBadgeObserved = await send('Runtime.evaluate', {
      expression: `(() => {
        return Boolean(document.querySelector('.model-badge'));
      })()`,
      returnByValue: true,
    });
  }

  let sourcesObserved = null;
  if (checkSearchSources) {
    await send('Runtime.evaluate', {
      expression: `(() => {
        ${startSavedChatExpression()}
        startSavedChat();
        const textarea = document.querySelector('textarea');
        const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set;
        setter.call(textarea, 'search OpenAI');
        textarea.dispatchEvent(new Event('input', { bubbles: true }));
        document.querySelector('form').requestSubmit();
        return true;
      })()`,
      returnByValue: true,
    });
    await sleep(3500);
    await send('Runtime.evaluate', {
      expression: `document.querySelector('button[aria-label="Show sources"]')?.click()`,
      returnByValue: true,
    });
    await sleep(300);
    sourcesObserved = await send('Runtime.evaluate', {
      expression: `Boolean(document.querySelector('.sources-panel .source-card'))`,
      returnByValue: true,
    });
  }

  let attachmentObserved = null;
  if (checkAttachment) {
    await send('Runtime.evaluate', {
      expression: `(() => {
        ${startSavedChatExpression()}
        startSavedChat();
        const input = document.querySelector('input[type="file"]');
        if (!input) return false;
        const transfer = new DataTransfer();
        transfer.items.add(new File(['hello from smoke'], 'linea-smoke.txt', { type: 'text/plain' }));
        input.files = transfer.files;
        input.dispatchEvent(new Event('change', { bubbles: true }));
        return true;
      })()`,
      returnByValue: true,
    });
    await sleep(300);
    attachmentObserved = await send('Runtime.evaluate', {
      expression: `document.body.innerText.includes('linea-smoke.txt')`,
      returnByValue: true,
    });
  }

  let mobileObserved = null;
  if (checkMobile) {
    mobileObserved = await send('Runtime.evaluate', {
      expression: `(() => {
        const shell = document.querySelector('.shell');
        const sidebar = document.querySelector('.sidebar');
        const chat = document.querySelector('.chat');
        const composer = document.querySelector('.composer-row');
        const header = document.querySelector('.chat-header');
        const doc = document.documentElement;
        const body = document.body;
        const composerRect = composer?.getBoundingClientRect();
        const chatRect = chat?.getBoundingClientRect();
        const headerRect = header?.getBoundingClientRect();
        const sidebarRect = sidebar?.getBoundingClientRect();
        const viewportWidth = window.innerWidth;
        const viewportHeight = window.innerHeight;
        const horizontalOverflow = Math.max(doc.scrollWidth, body.scrollWidth) - viewportWidth;
        return {
          ok:
            Boolean(shell && chat && composer) &&
            horizontalOverflow <= 2 &&
            composerRect.bottom <= viewportHeight + 2 &&
            composerRect.left >= 0 &&
            composerRect.right <= viewportWidth + 2 &&
            chatRect.height >= 420 &&
            headerRect.height <= 80 &&
            (!sidebarRect || (
              sidebarRect.height <= viewportHeight + 2 &&
              sidebarRect.width <= Math.min(284, viewportWidth * 0.82) + 2
            )),
          viewportWidth,
          viewportHeight,
          horizontalOverflow,
          composer: composerRect ? { left: composerRect.left, right: composerRect.right, bottom: composerRect.bottom } : null,
          chatHeight: chatRect?.height ?? 0,
          headerHeight: headerRect?.height ?? 0,
          sidebarHeight: sidebarRect?.height ?? 0,
          sidebarWidth: sidebarRect?.width ?? 0,
        };
      })()`,
      returnByValue: true,
    });
  }

  const bodyText = await send('Runtime.evaluate', {
    expression: 'document.body.innerText',
    returnByValue: true,
  });
  await fetch(`http://127.0.0.1:${port}/json/close/${tab.id}`);
  ws.close();

  return {
    bodyText: bodyText.result?.result?.value ?? '',
    sidebarToggleWorks:
      typeof sidebarInitial.result?.result?.value === 'boolean' &&
      sidebarAfterFirstToggle.result?.result?.value !== sidebarInitial.result?.result?.value &&
      sidebarAfterSecondToggle.result?.result?.value === sidebarInitial.result?.result?.value,
    composerExpands: composerExpands.result?.result?.value === true,
    composerShortcutWorks: composerShortcutWorks.result?.result?.value === true,
    systemPanelWorks: systemPanelWorks.result?.result?.value === true,
    themePanelWorks: themePanelWorks.result?.result?.value === true,
    footerPanelCloses:
      footerPanelCloses.result?.result?.value === true &&
      footerBlankCloses.result?.result?.value === true,
    sidebarCloseStateWorks: sidebarCloseState.result?.result?.value?.ok === true,
    sidebarCloseStateDetail: JSON.stringify(sidebarCloseState.result?.result?.value ?? {}),
    confirmDialogWorks:
      confirmDialogObserved.result?.result?.value?.skipped === true ||
      confirmDialogRendered.result?.result?.value?.ok === true,
    lightThemeWorks: !checkLightTheme || lightThemeObserved?.result?.result?.value?.ok === true,
    lightThemeDetail: JSON.stringify(lightThemeObserved?.result?.result?.value ?? {}),
    mobileWorks: !checkMobile || mobileObserved?.result?.result?.value?.ok === true,
    mobileDetail: JSON.stringify(mobileObserved?.result?.result?.value ?? {}),
    loadingWorks: !sendMessage || loadingObserved?.result?.result?.value === true,
    modelBadgeWorks: !sendMessage || modelBadgeObserved?.result?.result?.value === true,
    sourcesWork: !checkSearchSources || sourcesObserved?.result?.result?.value === true,
    attachmentWorks: !checkAttachment || attachmentObserved?.result?.result?.value === true,
    agentReviewWorks: !checkAgentReview || agentReviewObserved?.result?.result?.value?.ok === true,
    agentReviewDetail: JSON.stringify(agentReviewObserved?.result?.result?.value ?? {}),
    errorCount: events.filter((event) => event.method === 'Runtime.exceptionThrown').length,
  };
}

function startSavedChatExpression() {
  return `
    function startSavedChat() {
      const newChat = document.querySelector('.new-chat');
      if (!newChat) return false;
      newChat.click();
      const savedChat = Array.from(document.querySelectorAll('.new-chat-menu button'))
        .find((button) => button.textContent.trim() === 'Saved');
      if (!savedChat) return false;
      savedChat.click();
      return true;
    }
  `;
}

async function ensureAgentReviewProposal() {
  const proposalsResponse = await fetch(`${trimSlash(baseURL)}/api/agent/edit-proposals`);
  const proposals = await proposalsResponse.json().catch(() => []);
  if (proposalsResponse.ok && Array.isArray(proposals)) {
    const existing = proposals.find((proposal) => proposal.path === agentReviewFile && proposal.summary === agentReviewSummary);
    if (existing?.id) {
      agentReviewProposalId = existing.id;
      return;
    }
  }

  const fileResponse = await fetch(`${trimSlash(baseURL)}/api/agent/workspace/file?path=${encodeURIComponent(agentReviewFile)}`);
  const file = await fileResponse.json().catch(() => ({}));
  if (!fileResponse.ok) {
    throw new Error(
      `Agent review smoke requires a running server with LINEA_WORKSPACE_DIR and ${agentReviewFile}. ` +
        `File read returned ${fileResponse.status}: ${JSON.stringify(file)}`,
    );
  }
  const proposedContent = `${String(file.content ?? '').replace(/\s*$/, '')}\n\nAgent review smoke proposal.\n`;
  const proposalResponse = await fetch(`${trimSlash(baseURL)}/api/agent/edit-proposals`, {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({
      path: agentReviewFile,
      content: proposedContent,
      summary: agentReviewSummary,
    }),
  });
  const proposal = await proposalResponse.json().catch(() => ({}));
  if (!proposalResponse.ok || !proposal.id) {
    throw new Error(`Agent edit proposal returned ${proposalResponse.status}: ${JSON.stringify(proposal)}`);
  }
  agentReviewProposalId = proposal.id;
  agentReviewProposalCreated = true;
}

async function cleanupAgentReviewProposal() {
  if (!agentReviewProposalId || !agentReviewProposalCreated) return;
  try {
    await fetch(`${trimSlash(baseURL)}/api/agent/edit-proposals/${encodeURIComponent(agentReviewProposalId)}`, {
      method: 'PATCH',
      headers: jsonHeaders(),
      body: JSON.stringify({ status: 'rejected', detail: 'smoke cleanup' }),
    });
  } catch {
    // Best-effort cleanup.
  }
}

async function cleanupSmokeConversations(startedAt) {
  const titles = new Set(['Reply with only ok.', 'search OpenAI']);
  try {
    const response = await fetch(`${trimSlash(baseURL)}/api/conversations`);
    if (!response.ok) return;
    const conversations = await response.json();
    await Promise.all(
      conversations
        .filter((conversation) => titles.has(conversation.title))
        .filter((conversation) => new Date(conversation.createdAt).getTime() >= new Date(startedAt).getTime())
        .map((conversation) =>
          fetch(`${trimSlash(baseURL)}/api/conversations/${conversation.id}`, {
            method: 'DELETE',
          }),
        ),
    );
  } catch {
    // Best-effort cleanup.
  }
}

function jsonHeaders() {
  return { 'Content-Type': 'application/json' };
}

function trimSlash(value) {
  return value.replace(/\/$/, '');
}

main().catch((error) => {
  console.error(`FAIL ui smoke - ${error.message}`);
  process.exitCode = 1;
});
