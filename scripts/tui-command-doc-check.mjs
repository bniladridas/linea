#!/usr/bin/env node

import { readFileSync } from 'node:fs';

const helpSource = readFileSync('backend/internal/tui/agent.go', 'utf8');
const helpBlock = helpSource.match(/func agentHelp\(\) string \{[\s\S]*?return strings\.Join\(\[\]string\{([\s\S]*?)\}, "\\n"\)/);

if (!helpBlock) {
  console.error('Could not find TUI agent help commands.');
  process.exit(1);
}

const commands = [...helpBlock[1].matchAll(/"([^"]+)"/g)]
  .map((match) => match[1])
  .filter((line) => line.startsWith(':'))
  .map(normalizeCommand)
  .sort();

checkDocs('docs/client-api.md', commands);
checkDocs('docs/reference.md', commands);

console.log('PASS tui command docs');

function checkDocs(path, expectedCommands) {
  const content = readFileSync(path, 'utf8');
  const missing = expectedCommands.filter((command) => !content.includes(command));
  if (missing.length === 0) {
    return;
  }
  console.error(`Missing TUI command docs in ${path}:\n${missing.join('\n')}`);
  process.exit(1);
}

function normalizeCommand(command) {
  return command.replace(/\s+/g, ' ').trim();
}
