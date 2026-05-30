#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const baseURL = process.env.LINEA_UI_URL ?? 'http://127.0.0.1:8080/';
const chromePath = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const port = Number(process.env.LINEA_CODE_SMOKE_PORT ?? 9600 + (process.pid % 400));

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function main() {
  const conversation = await createConversation();
  await sendMessage(conversation.id);

  const profileDir = await mkdtemp(join(tmpdir(), 'linea-code-smoke-'));
  const chrome = spawn(chromePath, [
    '--headless=new',
    '--disable-gpu',
    '--no-first-run',
    `--user-data-dir=${profileDir}`,
    `--remote-debugging-port=${port}`,
    'about:blank',
  ], { stdio: ['ignore', 'pipe', 'pipe'] });

  let stderr = '';
  chrome.stderr.on('data', (chunk) => {
    stderr += chunk.toString();
  });

  try {
    await waitForDevTools(() => stderr);
    const result = await checkCodeRender(conversation.title);
    if (!result.clicked) {
      throw new Error('Could not find the code smoke conversation in the sidebar.');
    }
    if (!result.hasCode) {
      throw new Error('Fenced code did not render as a code block.');
    }
    if (result.hasFenceInBody) {
      throw new Error('Fenced code markers are visible in the rendered message.');
    }
    if (!result.hasCodeMeta) {
      throw new Error(`Code metadata did not render. Saw: ${result.codeMetaText || 'none'}`);
    }
    if (!result.hasLineNumbers) {
      throw new Error('Code line numbers did not render.');
    }
    if (!result.previewWorks) {
      throw new Error('HTML code preview did not open.');
    }
    console.log('PASS ui code render - fenced code rendered with metadata and preview');
  } finally {
    chrome.kill('SIGTERM');
  }
}

async function createConversation() {
  const title = `Code render smoke ${Date.now()}`;
  let response;
  try {
    response = await fetch(`${trimSlash(baseURL)}/api/conversations`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title }),
    });
  } catch (error) {
    throw new Error(`Could not reach API at ${baseURL}: ${error.cause?.message ?? error.message}`);
  }
  if (!response.ok) {
    throw new Error(`Could not create conversation: ${response.status}`);
  }
  return response.json();
}

async function sendMessage(conversationId) {
  const form = new FormData();
  form.append('content', 'Create a tiny single-page website in one index.html file. Include HTML, CSS, and JavaScript. Return only one fenced html code block.');
  const response = await fetch(`${trimSlash(baseURL)}/api/conversations/${conversationId}/messages`, {
    method: 'POST',
    body: form,
  });
  if (!response.ok) {
    throw new Error(`Could not send message: ${response.status}`);
  }
  await response.text();
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

async function checkCodeRender(title) {
  const tab = await fetch(`http://127.0.0.1:${port}/json/new?${encodeURIComponent(baseURL)}`, {
    method: 'PUT',
  }).then((res) => res.json());
  const ws = new WebSocket(tab.webSocketDebuggerUrl);
  await new Promise((resolve) => ws.addEventListener('open', resolve, { once: true }));

  let id = 0;
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
  await send('Page.enable');
  await send('Emulation.setDeviceMetricsOverride', {
    width: 1440,
    height: 900,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await send('Page.navigate', { url: baseURL });
  await sleep(2500);

  const clicked = await send('Runtime.evaluate', {
    expression: `(() => {
      const title = ${JSON.stringify(title)};
      const item = Array.from(document.querySelectorAll('.conversation-select')).find((button) => button.innerText.includes(title));
      item?.click();
      return Boolean(item);
    })()`,
    returnByValue: true,
  });
  await sleep(1500);
  const rendered = await send('Runtime.evaluate', {
    expression: `(() => ({
      hasCode: Boolean(document.querySelector('.message-content pre code')),
      preCodeCount: document.querySelectorAll('.message-content pre code').length,
      codeTopCount: document.querySelectorAll('.code-top').length,
      codeMetaText: document.querySelector('.code-top')?.innerText ?? '',
      hasCodeMeta: (document.querySelector('.code-top')?.innerText ?? '').includes('line') && (document.querySelector('.code-top')?.innerText ?? '').includes('byte'),
      hasLineNumbers: Array.from(document.querySelectorAll('.code-line-number')).slice(0, 3).map((node) => node.textContent?.trim()).join(',') === '1,2,3',
      hasFenceInBody: document.body.innerText.includes('___never___') || document.body.innerText.includes('\`\`\`html'),
    }))()`,
    returnByValue: true,
  });
  await send('Runtime.evaluate', {
    expression: `document.querySelector('.code-action')?.click()`,
    returnByValue: true,
  });
  await sleep(300);
  const previewed = await send('Runtime.evaluate', {
    expression: `Boolean(document.querySelector('.code-preview'))`,
    returnByValue: true,
  });

  await fetch(`http://127.0.0.1:${port}/json/close/${tab.id}`);
  ws.close();

  return {
    clicked: clicked.result?.result?.value === true,
    previewWorks: previewed.result?.result?.value === true,
    ...(rendered.result?.result?.value ?? {}),
  };
}

function trimSlash(value) {
  return value.replace(/\/$/, '');
}

main().catch((error) => {
  console.error(`FAIL ui code render - ${error.message}`);
  process.exitCode = 1;
});
