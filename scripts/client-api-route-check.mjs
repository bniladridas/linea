#!/usr/bin/env node

import { readFileSync } from 'node:fs';

const server = readFileSync('backend/internal/api/server.go', 'utf8');

const backendRoutes = [...server.matchAll(/HandleFunc\("([^"]+)"/g)]
  .map((match) => match[1])
  .filter((route) => route !== 'GET /')
  .sort();

checkDocumentedRoutes('docs/client-api.md', documentedInlineRoutes);
checkDocumentedRoutes('docs/reference.md', documentedTableRoutes);

console.log('PASS client api routes');

function checkDocumentedRoutes(path, readRoutes) {
  const documentedRoutes = readRoutes(readFileSync(path, 'utf8')).filter((route) => route !== 'GET /').sort();
  const uniqueDocumentedRoutes = [...new Set(documentedRoutes)];
  const duplicated = duplicatedRoutes(documentedRoutes);
  const missing = backendRoutes.filter((route) => !uniqueDocumentedRoutes.includes(route));
  const stale = uniqueDocumentedRoutes.filter((route) => !backendRoutes.includes(route));
  if (missing.length === 0 && stale.length === 0 && duplicated.length === 0) {
    return;
  }
  if (duplicated.length > 0) {
    console.error(`Duplicated client API docs in ${path}:\n${duplicated.join('\n')}`);
  }
  if (missing.length > 0) {
    console.error(`Missing client API docs in ${path}:\n${missing.join('\n')}`);
  }
  if (stale.length > 0) {
    console.error(`Stale client API docs in ${path}:\n${stale.join('\n')}`);
  }
  process.exit(1);
}

function duplicatedRoutes(routes) {
  return routes.filter((route, index) => routes.indexOf(route) !== index);
}

function documentedInlineRoutes(content) {
  return [...content.matchAll(/`((?:GET|POST|PATCH|DELETE|PUT) [^`]+)`/g)].map((match) => match[1].split('?')[0]);
}

function documentedTableRoutes(content) {
  return [...content.matchAll(/^\| `(GET|POST|PATCH|DELETE|PUT)` \| `([^`]+)` \|$/gm)].map(
    (match) => `${match[1]} ${match[2]}`,
  );
}
