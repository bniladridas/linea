#!/usr/bin/env node

import { readFile } from 'node:fs/promises';

const env = await loadEnv('.env');
const prompt = 'Reply with only ok.';
const selectedProviders = new Set(process.argv.slice(2).filter((arg) => !arg.startsWith('--')));
const configuredOnly = process.argv.includes('--configured');

const providers = [
  {
    name: 'gemini',
    key: env.GEMINI_API_KEY,
    model: env.GEMINI_MODEL || 'gemini-2.5-flash-lite',
    list: listGemini,
    test: testGemini,
  },
  {
    name: 'sambanova',
    key: env.SAMBANOVA_API_KEY,
    enabled: envBool(env.SAMBANOVA_ENABLED, true),
    baseURL: env.SAMBANOVA_BASE_URL || 'https://api.sambanova.ai/v1',
    model: env.SAMBANOVA_MODEL || 'gpt-oss-120b',
    list: listOpenAICompatible,
    test: testOpenAICompatible,
  },
  {
    name: 'cerebras',
    key: env.CEREBRAS_API_KEY,
    enabled: envBool(env.CEREBRAS_ENABLED, true),
    baseURL: env.CEREBRAS_BASE_URL || 'https://api.cerebras.ai/v1',
    model: env.CEREBRAS_MODEL || 'gpt-oss-120b',
    list: listOpenAICompatible,
    test: testOpenAICompatible,
  },
  {
    name: 'ollama',
    baseURL: env.OLLAMA_BASE_URL || 'http://localhost:11434',
    model: env.OLLAMA_MODEL || 'qwen2.5-coder:1.5b',
    list: listOllama,
    test: testOllama,
  },
];

let hasFailure = false;
for (const provider of providers) {
  if (selectedProviders.size > 0 && !selectedProviders.has(provider.name)) {
    continue;
  }
	const header = provider.baseURL ? `${provider.name} (${provider.baseURL})` : provider.name;
	console.log(header);
  if (provider.enabled === false && selectedProviders.size === 0) {
    console.log('  WARN disabled');
    continue;
  }
	if ('key' in provider && !provider.key) {
		console.log('  WARN key not set');
		continue;
	}

	let models;
	try {
		models = configuredOnly ? [] : await provider.list(provider);
	} catch (error) {
    hasFailure = true;
    console.log(`  FAIL list models - ${redact(error.message)}`);
    continue;
  }

  const seen = new Set();
  const configuredModel = provider.name === 'gemini' ? normalizeGeminiModel(provider.model) : provider.model;
  const ordered = [configuredModel, ...models].filter(Boolean).filter((model) => {
    if (seen.has(model)) return false;
    seen.add(model);
    return true;
  });

  if (ordered.length === 0) {
    hasFailure = true;
    console.log('  FAIL no chat models found');
    continue;
  }

  for (const model of ordered) {
    try {
      const text = await provider.test(provider, model);
      if (text.trim() === '') {
        hasFailure = true;
        console.log(`  FAIL ${model} - empty response`);
      } else {
        console.log(`  PASS ${model}`);
      }
    } catch (error) {
      hasFailure = true;
      console.log(`  FAIL ${model} - ${redact(error.message)}`);
    }
  }
}

if (hasFailure) {
  process.exitCode = 1;
}

async function loadEnv(path) {
  const values = {};
  let text = '';
  try {
    text = await readFile(path, 'utf8');
  } catch {
    return values;
  }
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const equals = trimmed.indexOf('=');
    if (equals === -1) continue;
    const key = trimmed.slice(0, equals).trim();
    let value = trimmed.slice(equals + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    values[key] = value;
  }
  return values;
}

async function listGemini(provider) {
	const payload = await requestJSON(`https://generativelanguage.googleapis.com/v1beta/models?key=${encodeURIComponent(provider.key)}`);
	return (payload.models || [])
		.filter((model) => Array.isArray(model.supportedGenerationMethods) && model.supportedGenerationMethods.includes('generateContent'))
		.map((model) => model.name)
		.filter((name) => name && /^models\/(gemini|gemma)/i.test(name))
		.filter((name) => !/embedding|aqa|tts|image|robotics|computer|deep-research|banana|antigravity|lyria/i.test(name));
}

async function testGemini(provider, model) {
  const payload = await requestJSON(`https://generativelanguage.googleapis.com/v1beta/${model}:generateContent?key=${encodeURIComponent(provider.key)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      contents: [{ role: 'user', parts: [{ text: prompt }] }],
    }),
  });
  return (payload.candidates || [])
    .flatMap((candidate) => candidate.content?.parts || [])
    .map((part) => part.text || '')
    .join('');
}

async function listOpenAICompatible(provider) {
  const payload = await requestJSON(`${trimSlash(provider.baseURL)}/models`, {
    headers: { Authorization: `Bearer ${provider.key}` },
  });
  return (payload.data || [])
    .map((model) => model.id)
    .filter((id) => id && !/embed|rerank|guard|moderation|image|audio|whisper|tts/i.test(id));
}

async function testOpenAICompatible(provider, model) {
	const text = await requestText(`${trimSlash(provider.baseURL)}/chat/completions`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${provider.key}`,
			'Content-Type': 'application/json',
		},
		body: JSON.stringify({
			model,
			messages: [{ role: 'user', content: prompt }],
			temperature: 0,
			stream: true,
		}),
	});
	return streamContent(text);
}

async function listOllama(provider) {
  const payload = await requestJSON(`${trimSlash(provider.baseURL)}/api/tags`);
  return (payload.models || []).map((model) => model.name).filter(Boolean);
}

async function testOllama(provider, model) {
  const payload = await requestJSON(`${trimSlash(provider.baseURL)}/api/generate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model,
      prompt,
      stream: false,
      options: { temperature: 0, num_predict: 8 },
    }),
  });
  return payload.response || '';
}

async function requestJSON(url, options = {}) {
	const text = await requestText(url, options);
	if (!text) return {};
	try {
		return JSON.parse(text);
	} catch {
		throw new Error(`invalid json: ${text.slice(0, 240)}`);
	}
}

async function requestText(url, options = {}) {
	const controller = new AbortController();
	const timeout = setTimeout(() => controller.abort(), 30_000);
	try {
		const res = await fetch(url, { ...options, signal: controller.signal });
		const text = await res.text();
		if (!res.ok) {
			let payload = {};
			try {
				payload = JSON.parse(text);
			} catch {
				// Keep the raw body below.
			}
			const detail = payload.error?.message || payload.message || text || res.statusText;
			throw new Error(`${res.status} ${res.statusText}: ${detail}`);
		}
		return text;
	} finally {
		clearTimeout(timeout);
	}
}

function streamContent(text) {
	let output = '';
	for (const line of text.split(/\r?\n/)) {
		const trimmed = line.trim();
		if (!trimmed.startsWith('data:')) continue;
		const data = trimmed.slice(5).trim();
		if (!data || data === '[DONE]') continue;
		const payload = JSON.parse(data);
		if (payload.error?.message) {
			throw new Error(payload.error.message);
		}
		for (const choice of payload.choices || []) {
			output += choice.delta?.content || choice.message?.content || choice.text || '';
		}
	}
	return output;
}

function normalizeGeminiModel(model) {
	if (!model) return model;
	return model.startsWith('models/') ? model : `models/${model}`;
}

function envBool(value, fallback) {
  switch (value) {
    case '1':
    case 'true':
    case 'TRUE':
    case 'yes':
    case 'YES':
    case 'on':
    case 'ON':
      return true;
    case '0':
    case 'false':
    case 'FALSE':
    case 'no':
    case 'NO':
    case 'off':
    case 'OFF':
      return false;
    default:
      return fallback;
  }
}

function trimSlash(value) {
  return String(value || '').replace(/\/+$/, '');
}

function redact(message) {
  return String(message)
    .replace(/AIza[0-9A-Za-z_-]+/g, '[redacted]')
    .replace(/Bearer\s+[0-9A-Za-z._-]+/gi, 'Bearer [redacted]')
    .replace(/\s+/g, ' ')
    .slice(0, 260);
}
