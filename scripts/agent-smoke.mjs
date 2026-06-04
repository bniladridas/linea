#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const shouldStart = process.argv.includes('--start');
const memoryMode = process.env.LINEA_AGENT_SMOKE_MEMORY === '1';
const port = Number(process.env.LINEA_AGENT_SMOKE_PORT ?? 9700 + (process.pid % 300));
const baseURL = process.env.LINEA_AGENT_URL ?? `http://127.0.0.1:${port}`;
const binary = process.env.LINEA_BIN ?? './bin/linea';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

let server;
try {
  if (shouldStart) {
    server = startServer();
    await waitForHealth();
  }
  await runChecks();
  console.log(`PASS agent smoke - ${baseURL}`);
} finally {
  if (server) {
    server.kill('SIGTERM');
  }
}

function startServer() {
  const url = new URL(baseURL);
  const addr = `${url.hostname}:${url.port}`;
  const child = spawn(binary, [], {
    env: {
      ...process.env,
      API_ADDR: addr,
      WEB_ORIGIN: baseURL,
      ...(memoryMode
        ? {
            DATABASE_URL: '',
            LINEA_COMMAND_ALLOWLIST: '',
            LINEA_ENV_FILE: '/dev/null',
            LINEA_MCP_CONFIG: '',
            LINEA_SETTINGS_FILE: join(tmpdir(), `linea-agent-smoke-${process.pid}.json`),
            LINEA_SKILLS_DIR: '',
            LINEA_WORKSPACE_DIR: '',
          }
        : {}),
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

async function waitForHealth() {
  const started = Date.now();
  while (Date.now() - started < 10_000) {
    try {
      const payload = await requestJSON('/healthz');
      if (payload.status === 'ok') {
        return;
      }
    } catch {
      // Server is still starting.
    }
    await sleep(100);
  }
  throw new Error(`Timed out waiting for Linea at ${baseURL}.`);
}

async function runChecks() {
  const health = await requestJSON('/healthz');
  assert(health.status === 'ok', 'healthz did not return ok');

  const agent = await requestJSON('/api/agent');
  assert(agent.mode === 'local', 'agent mode is not local');
  assert(Array.isArray(agent.tools) && agent.tools.length > 0, 'agent tools missing');
  assert(Array.isArray(agent.subagents) && agent.subagents.length > 0, 'agent subagents missing');
  assert(Array.isArray(agent.hooks) && agent.hooks.length > 0, 'agent hooks missing');
  assert(Array.isArray(agent.skills) && agent.skills.length > 0, 'agent skills missing');
  assert(Array.isArray(agent.boundaries) && agent.boundaries.length > 0, 'agent boundaries missing');

  const summary = await requestJSON('/api/agent/run-summary');
  assert(typeof summary.state === 'string' && summary.state.length > 0, 'agent summary state missing');

  const run = await requestJSON('/api/agent/runs', { method: 'POST' });
  assert(run.id && run.summary, 'agent run snapshot missing fields');

  const runs = await requestJSON('/api/agent/runs');
  assert(Array.isArray(runs) && runs.some((item) => item.id === run.id), 'agent run snapshot was not listed');

  const subagents = await requestJSON('/api/agent/subagents');
  assert(Array.isArray(subagents) && subagents.some((item) => item.id === 'review'), 'review subagent missing');

  const mcpServers = await requestJSON('/api/agent/mcp-servers');
  assert(Array.isArray(mcpServers), 'mcp server list is not an array');

  const mcpTools = await requestJSON('/api/agent/mcp-tools');
  assert(Array.isArray(mcpTools), 'mcp tool list is not an array');

  const trace = await requestJSON('/api/agent/traces', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ event: 'smoke trace', state: 'ready', detail: 'agent api' }),
  });
  assert(trace.id && trace.event === 'smoke trace', 'agent trace was not created');

  const traces = await requestJSON('/api/agent/traces');
  assert(Array.isArray(traces) && traces.some((item) => item.id === trace.id), 'agent trace was not listed');

  const hookRun = await requestJSON('/api/agent/hook-runs', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ hookId: 'after_check', state: 'completed', detail: 'smoke' }),
  });
  assert(hookRun.id && hookRun.hookId === 'after_check', 'agent hook run was not created');

  const hookExecution = await requestJSON('/api/agent/hooks/after_check/run', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ detail: 'smoke execution' }),
  });
  assert(hookExecution.hookRun?.state === 'completed', 'agent hook execution did not complete');

  const hookRuns = await requestJSON('/api/agent/hook-runs');
  assert(Array.isArray(hookRuns) && hookRuns.some((item) => item.id === hookRun.id), 'agent hook run was not listed');

  const readySkill = agent.skills.find((item) => item.state === 'ready');
  let skillRunCreated = false;
  if (readySkill) {
    const skillExecution = await requestJSON(`/api/agent/skills/${encodeURIComponent(readySkill.id)}/run`, {
      method: 'POST',
      headers: jsonHeaders(),
      body: JSON.stringify({ detail: 'smoke skill' }),
    });
    assert(skillExecution.skillRun?.skillId === readySkill.id, 'agent skill execution did not create a run');
    skillRunCreated = true;
  } else {
    const plannedSkill = agent.skills[0];
    const blockedSkill = await requestOptionalJSON(`/api/agent/skills/${encodeURIComponent(plannedSkill.id)}/run`, [400], {
      method: 'POST',
      headers: jsonHeaders(),
      body: JSON.stringify({ detail: 'smoke planned skill' }),
    });
    assert(blockedSkill.ok, 'planned skill should return a safe error');
  }

  const skillRuns = await requestJSON('/api/agent/skill-runs');
  assert(Array.isArray(skillRuns), 'agent skill run list is not an array');

  const approval = await requestJSON('/api/agent/command-approvals', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ command: 'printf smoke', state: 'pending', detail: 'smoke' }),
  });
  assert(approval.id && approval.state === 'pending', 'agent command approval was not created');

  const approvals = await requestJSON('/api/agent/command-approvals');
  assert(Array.isArray(approvals) && approvals.some((item) => item.id === approval.id), 'agent command approval was not listed');

  const commandCheck = await requestJSON('/api/agent/command-checks', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ command: 'printf smoke' }),
  });
  assert(commandCheck.id && commandCheck.allowed === false, 'unapproved command check should be blocked');

  const commandChecks = await requestJSON('/api/agent/command-checks');
  assert(Array.isArray(commandChecks) && commandChecks.some((item) => item.id === commandCheck.id), 'agent command check was not listed');

  const blockedRun = await requestOptionalJSON('/api/agent/command-runs', [400], {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ command: 'printf smoke' }),
  });
  assert(blockedRun.ok, 'blocked command run did not return a safe error');

  const commandRuns = await requestJSON('/api/agent/command-runs');
  assert(Array.isArray(commandRuns), 'agent command run list is not an array');

  await checkWorkspaceEndpoints();

  const proposals = await requestJSON('/api/agent/edit-proposals');
  assert(Array.isArray(proposals), 'agent edit proposal list is not an array');

  const diagnostics = await requestOptionalJSON('/api/agent/workspace/diagnostics', [200, 404]);
  assert(diagnostics.ok, 'diagnostics endpoint returned an unexpected status');

  const finalSummary = await requestJSON('/api/agent/run-summary');
  assert(finalSummary.traceEvents > 0, 'agent summary did not count traces');
  assert(finalSummary.hookRuns > 0, 'agent summary did not count hook runs');
  if (skillRunCreated) {
    assert(finalSummary.skillRuns > 0, 'agent summary did not count skill runs');
  }
  assert(finalSummary.commandApprovals > 0, 'agent summary did not count command approvals');
  assert(finalSummary.commandChecks > 0, 'agent summary did not count command checks');
}

async function checkWorkspaceEndpoints() {
  const file = await requestOptionalJSON('/api/agent/workspace/file?path=README.md', [200, 404]);
  if (file.status === 404) {
    assert(file.body?.error, 'workspace disabled response missing error');
    const search = await requestOptionalJSON('/api/agent/workspace/search?q=Linea', [404]);
    assert(search.ok, 'workspace search should be off when workspace is off');
    return;
  }
  assert(file.ok && file.body?.path === 'README.md', 'workspace file read failed');

  const search = await requestJSON('/api/agent/workspace/search?q=Linea');
  assert(Array.isArray(search), 'workspace search did not return a list');

  const diagnostics = await requestJSON('/api/agent/workspace/diagnostics');
  assert(Array.isArray(diagnostics), 'workspace diagnostics did not return a list');

  const proposal = await requestJSON('/api/agent/edit-proposals', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ path: 'README.md', content: file.body.content, summary: 'smoke proposal' }),
  });
  assert(proposal.id && proposal.status === 'pending', 'agent edit proposal was not created');

  const reviewed = await requestJSON(`/api/agent/edit-proposals/${encodeURIComponent(proposal.id)}`, {
    method: 'PATCH',
    headers: jsonHeaders(),
    body: JSON.stringify({ status: 'rejected', detail: 'smoke review' }),
  });
  assert(reviewed.status === 'rejected', 'agent edit proposal review failed');
}

async function requestOptionalJSON(path, statuses, options = {}) {
  const response = await fetch(`${trimSlash(baseURL)}${path}`, options);
  const body = await response.json().catch(() => ({}));
  return { ok: statuses.includes(response.status), status: response.status, body };
}

async function requestJSON(path, options = {}) {
  const response = await fetch(`${trimSlash(baseURL)}${path}`, options);
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(`${path} returned ${response.status}: ${JSON.stringify(body)}`);
  }
  return body;
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
