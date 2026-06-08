#!/usr/bin/env node

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';

const roots = ['README.md', 'docs', '.github/workflows/README.md'];
const failures = [];

for (const file of docsFiles(roots)) {
  const content = readFileSync(file, 'utf8');
  for (const link of localLinks(content)) {
    const target = path.normalize(path.join(path.dirname(file), link));
    if (!existsSync(target)) {
      failures.push(`${file}: ${link}`);
    }
  }
}

if (failures.length > 0) {
  console.error(`Broken local docs links:\n${failures.join('\n')}`);
  process.exit(1);
}

console.log('PASS docs links');

function docsFiles(entries) {
  const files = [];
  for (const entry of entries) {
    if (!existsSync(entry)) {
      continue;
    }
    const info = statSync(entry);
    if (info.isDirectory()) {
      for (const name of readdirSync(entry).sort()) {
        const nested = path.join(entry, name);
        if (statSync(nested).isFile() && nested.endsWith('.md')) {
          files.push(nested);
        }
      }
      continue;
    }
    if (info.isFile() && entry.endsWith('.md')) {
      files.push(entry);
    }
  }
  return files;
}

function localLinks(content) {
  const links = [];
  for (const match of content.matchAll(/!?\[[^\]]*\]\(([^)]+)\)/g)) {
    links.push(match[1]);
  }
  for (const match of content.matchAll(/<img\s+[^>]*src="([^"]+)"/g)) {
    links.push(match[1]);
  }
  return links.map(cleanLink).filter(isLocalFileLink);
}

function cleanLink(link) {
  return link.trim().replace(/^<|>$/g, '').split('#')[0].split('?')[0];
}

function isLocalFileLink(link) {
  return (
    link !== '' &&
    !link.startsWith('/') &&
    !link.startsWith('http://') &&
    !link.startsWith('https://') &&
    !link.startsWith('mailto:') &&
    !link.startsWith('data:')
  );
}
