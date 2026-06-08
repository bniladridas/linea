#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const port = Number(process.env.LINEA_AGENT_AUTONOMY_SMOKE_PORT ?? 9900 + (process.pid % 200));
const baseURL = process.env.LINEA_AGENT_URL ?? `http://127.0.0.1:${port}`;
const binary = process.env.LINEA_BIN ?? './bin/linea';
const root = mkdtempSync(join(tmpdir(), 'linea-agent-autonomy-smoke-'));
const workspace = join(root, 'workspace');
const mcpServerPath = join(root, 'mcp-server.mjs');
const mcpConfigPath = join(root, 'mcp.json');
const mcpShutdownMarker = join(root, 'mcp-shutdown.txt');
const settingsPath = join(root, 'settings.json');

mkdirSync(workspace);
writeFileSync(join(workspace, 'package.json'), '{"scripts":{"test":"echo ok"}}\n');
writeFileSync(join(workspace, 'README.md'), '# smoke\n');
writeFileSync(mcpServerPath, mcpServerSource());
writeFileSync(
  mcpConfigPath,
  JSON.stringify(
    {
      mcpServers: {
        smoke: {
          command: process.execPath,
          args: [mcpServerPath],
          env: { LINEA_MCP_SHUTDOWN_MARKER: mcpShutdownMarker },
          tools: [
            {
              name: 'echo',
              description: 'Echo a message.',
              inputSchema: {
                type: 'object',
                required: ['message'],
                properties: { message: { type: 'string' } },
              },
            },
          ],
          resources: [{ uri: 'docs://readme', name: 'README', description: 'Smoke resource.' }],
        },
      },
    },
    null,
    2,
  ),
);

let server;
try {
  server = startServer();
  await waitForHealth();
  await runChecks();
  await stopServerAndCheckMCPShutdown(server);
  server = undefined;
  console.log(`PASS agent autonomy smoke - ${baseURL}`);
} finally {
  if (server) {
    server.kill('SIGTERM');
  }
  rmSync(root, { recursive: true, force: true });
}

function startServer() {
  const url = new URL(baseURL);
  const child = spawn(binary, [], {
    env: {
      ...process.env,
      API_ADDR: `${url.hostname}:${url.port}`,
      WEB_ORIGIN: baseURL,
      DATABASE_URL: '',
      LINEA_AGENT_DEVELOPER_MODE: '1',
      LINEA_AGENT_WORKSPACE_TRUST: 'full',
      LINEA_COMMAND_ALLOWLIST: '',
      LINEA_ENV_FILE: '/dev/null',
      LINEA_MCP_CONFIG: mcpConfigPath,
      LINEA_SETTINGS_FILE: settingsPath,
      LINEA_SKILLS_DIR: '',
      LINEA_WORKSPACE_DIR: workspace,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let stderr = '';
  child.stderr.on('data', (chunk) => {
    stderr += chunk.toString();
  });
  child.on('exit', (code) => {
    if (code !== 0 && code !== null) {
      console.error(`linea exited with ${code}${stderr ? `\n${stderr.trim()}` : ''}`);
    }
  });
  return child;
}

async function runChecks() {
  const developerLoop = await requestJSON('/api/agent/loops', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ goal: 'run command', mode: 'developer', command: 'printf autonomy' }),
  });
  assert(developerLoop.state === 'completed', 'developer loop did not complete');
  assert(hasStep(developerLoop, 'command_check', 'completed'), 'developer command was not checked');
  assert(hasStep(developerLoop, 'command_run', 'completed'), 'developer command did not run');

  const blockedWrapper = await requestJSON('/api/agent/loops', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ goal: 'run command', mode: 'developer', command: 'sh -c rm -rf .' }),
  });
  assert(blockedWrapper.state === 'attention', 'shell wrapper command did not stop at attention');
  assert(hasStep(blockedWrapper, 'command_check', 'blocked'), 'shell wrapper command was not blocked');
  assert(!hasStep(blockedWrapper, 'command_run', 'completed'), 'shell wrapper command ran');

  const mcpBoundary = await requestJSON('/api/agent/loops', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ goal: 'use mcp tool echo', mode: 'auto' }),
  });
  assert(mcpBoundary.state === 'attention', 'MCP missing-arg loop did not stop at attention');
  assert(hasStep(mcpBoundary, 'mcp_boundary', 'completed'), 'MCP missing-arg boundary was not recorded');
  assert(!hasStep(mcpBoundary, 'mcp_call', 'completed'), 'MCP missing-arg tool call ran');

  const mcpCall = await requestJSON('/api/agent/loops', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ goal: 'use mcp tool echo with message "hello autonomy"', mode: 'auto' }),
  });
  assert(mcpCall.state === 'completed', 'MCP explicit-arg loop did not complete');
  assert(hasStep(mcpCall, 'mcp_call', 'completed'), 'MCP explicit-arg tool call did not run');

  const subscription = await requestJSON('/api/agent/mcp-resources/subscribe', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ uri: 'docs://readme' }),
  });
  assert(subscription.state === 'active', 'MCP subscription did not become active');

  const events = await waitFor(() => requestJSON('/api/agent/mcp-events'), (items) => Array.isArray(items) && items.length > 0);
  assert(events.some((event) => event.uri === 'docs://readme'), 'MCP subscription event was not recorded');

  const subscriptions = await requestJSON('/api/agent/mcp-subscriptions');
  assert(subscriptions.some((item) => item.id === subscription.id && item.state === 'active'), 'MCP subscription was not listed as active');
}

async function stopServerAndCheckMCPShutdown(child) {
  const exited = waitForExit(child);
  child.kill('SIGTERM');
  const result = await withTimeout(exited, 10_000, 'Timed out waiting for Linea shutdown.');
  assert(result.code === 0, `Linea exited with ${result.code ?? result.signal}`);
  await waitFor(
    async () => readFileSync(mcpShutdownMarker, 'utf8'),
    (content) => content.includes('exit'),
    'Timed out waiting for MCP session shutdown marker.',
  );
}

async function waitForHealth() {
  await waitFor(async () => requestJSON('/healthz'), (payload) => payload.status === 'ok', `Timed out waiting for Linea at ${baseURL}.`);
}

async function waitFor(read, accept, message = 'Timed out waiting for condition.') {
  const started = Date.now();
  let lastError;
  while (Date.now() - started < 10_000) {
    try {
      const value = await read();
      if (accept(value)) {
        return value;
      }
    } catch (error) {
      lastError = error;
    }
    await sleep(100);
  }
  throw new Error(lastError ? `${message} ${lastError.message}` : message);
}

async function requestJSON(path, options = {}) {
  const response = await fetch(`${trimSlash(baseURL)}${path}`, options);
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(`${path} returned ${response.status}: ${JSON.stringify(body)}`);
  }
  return body;
}

function mcpServerSource() {
  return String.raw`
import { appendFileSync } from 'node:fs';

let buffer = Buffer.alloc(0);
const marker = process.env.LINEA_MCP_SHUTDOWN_MARKER;

process.on('exit', () => {
  if (marker) appendFileSync(marker, 'exit\n');
});

process.stdin.on('data', (chunk) => {
  buffer = Buffer.concat([buffer, chunk]);
  while (true) {
    const headerEnd = buffer.indexOf('\r\n\r\n');
    if (headerEnd < 0) return;
    const header = buffer.subarray(0, headerEnd).toString();
    const match = header.match(/Content-Length:\s*(\d+)/i);
    if (!match) process.exit(1);
    const length = Number(match[1]);
    const bodyStart = headerEnd + 4;
    const bodyEnd = bodyStart + length;
    if (buffer.length < bodyEnd) return;
    const message = JSON.parse(buffer.subarray(bodyStart, bodyEnd).toString());
    buffer = buffer.subarray(bodyEnd);
    handle(message);
  }
});

process.stdin.on('end', () => process.exit(0));

function handle(message) {
  const method = message.method;
  if (method === 'initialize') {
    send({ jsonrpc: '2.0', id: message.id, result: { protocolVersion: '2024-11-05', capabilities: {}, serverInfo: { name: 'smoke', version: '0' } } });
    return;
  }
  if (method === 'tools/call') {
    send({ jsonrpc: '2.0', id: message.id, result: { content: [{ type: 'text', text: message.params?.arguments?.message ?? '' }] } });
    return;
  }
  if (method === 'resources/subscribe') {
    const uri = message.params?.uri ?? 'docs://readme';
    send({ jsonrpc: '2.0', id: message.id, result: {} });
    send({ jsonrpc: '2.0', method: 'notifications/resources/updated', params: { uri, message: 'updated' } });
    return;
  }
  if (method === 'resources/unsubscribe') {
    send({ jsonrpc: '2.0', id: message.id, result: {} });
    return;
  }
  if (message.id !== undefined) {
    send({ jsonrpc: '2.0', id: message.id, result: {} });
  }
}

function send(message) {
  const body = JSON.stringify(message);
  process.stdout.write('Content-Length: ' + Buffer.byteLength(body) + '\r\n\r\n' + body);
}
`;
}

function hasStep(loop, kind, state) {
  return Array.isArray(loop.steps) && loop.steps.some((step) => step.kind === kind && step.state === state);
}

function waitForExit(child) {
  return new Promise((resolve) => {
    child.once('exit', (code, signal) => resolve({ code, signal }));
  });
}

function withTimeout(promise, ms, message) {
  let timer;
  return Promise.race([
    promise.finally(() => clearTimeout(timer)),
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(message)), ms);
    }),
  ]);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function jsonHeaders() {
  return { 'Content-Type': 'application/json' };
}

function trimSlash(value) {
  return value.replace(/\/+$/, '');
}
