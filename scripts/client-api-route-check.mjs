#!/usr/bin/env node

import { readFileSync } from 'node:fs';

const server = readFileSync('backend/internal/api/server.go', 'utf8');
const docs = readFileSync('docs/client-api.md', 'utf8');

const backendRoutes = [...server.matchAll(/HandleFunc\("([^"]+)"/g)]
  .map((match) => match[1])
  .filter((route) => route !== 'GET /')
  .sort();

const documentedRoutes = [...docs.matchAll(/`((?:GET|POST|PATCH|DELETE) [^`]+)`/g)]
  .map((match) => match[1].split('?')[0])
  .filter((route) => route !== 'GET /')
  .sort();

const uniqueDocumentedRoutes = [...new Set(documentedRoutes)];
const missing = backendRoutes.filter((route) => !uniqueDocumentedRoutes.includes(route));
const stale = uniqueDocumentedRoutes.filter((route) => !backendRoutes.includes(route));

if (missing.length > 0 || stale.length > 0) {
  if (missing.length > 0) {
    console.error(`Missing client API docs:\n${missing.join('\n')}`);
  }
  if (stale.length > 0) {
    console.error(`Stale client API docs:\n${stale.join('\n')}`);
  }
  process.exit(1);
}

console.log('PASS client api routes');
