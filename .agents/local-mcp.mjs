const protocolVersion = '2024-11-05';

let buffer = Buffer.alloc(0);

process.stdin.on('data', (chunk) => {
  buffer = Buffer.concat([buffer, chunk]);
  readMessages();
});

function readMessages() {
  while (true) {
    const separator = buffer.indexOf('\r\n\r\n');
    if (separator < 0) {
      return;
    }

    const header = buffer.subarray(0, separator).toString('utf8');
    const match = header.match(/^Content-Length:\s*(\d+)$/im);
    if (!match) {
      process.exitCode = 1;
      return;
    }

    const length = Number(match[1]);
    const start = separator + 4;
    const end = start + length;
    if (buffer.length < end) {
      return;
    }

    const body = buffer.subarray(start, end).toString('utf8');
    buffer = buffer.subarray(end);
    handleMessage(JSON.parse(body));
  }
}

function handleMessage(message) {
  if (!message.id && message.method === 'notifications/initialized') {
    return;
  }

  switch (message.method) {
    case 'initialize':
      respond(message.id, {
        protocolVersion,
        capabilities: {
          tools: {},
          resources: {},
          prompts: {},
        },
        serverInfo: {
          name: 'local-linea',
          version: '0.1.0',
        },
      });
      return;

    case 'tools/list':
      respond(message.id, {
        tools: [
          {
            name: 'linea_status',
            description: 'Read local Linea status.',
            inputSchema: {
              type: 'object',
              properties: {
                detail: {
                  type: 'string',
                  description: 'Optional detail level.',
                },
              },
            },
          },
          {
            name: 'echo',
            description: 'Echo a message through the local MCP server.',
            inputSchema: {
              type: 'object',
              properties: {
                message: {
                  type: 'string',
                },
              },
            },
          },
        ],
      });
      return;

    case 'tools/call':
      callTool(message);
      return;

    case 'resources/list':
      respond(message.id, {
        resources: [
          {
            uri: 'memory:readme',
            name: 'README',
            description: 'Demo MCP resource.',
            mimeType: 'text/markdown',
          },
          {
            uri: 'memory:status',
            name: 'Status',
            description: 'Current local MCP status.',
            mimeType: 'application/json',
          },
        ],
      });
      return;

    case 'resources/read':
      readResource(message);
      return;

    case 'prompts/list':
      respond(message.id, {
        prompts: [
          {
            name: 'review',
            description: 'Create a short review prompt.',
          },
          {
            name: 'summarize',
            description: 'Create a short summary prompt.',
          },
        ],
      });
      return;

    case 'prompts/get':
      getPrompt(message);
      return;

    default:
      respondError(message.id, -32601, `Unknown method: ${message.method}`);
  }
}

function callTool(message) {
  const name = message.params?.name;
  const args = message.params?.arguments ?? {};

  if (name === 'linea_status') {
    respond(message.id, {
      content: [
        {
          type: 'text',
          text: `local MCP server is working${args.detail ? ` (${args.detail})` : ''}`,
        },
      ],
    });
    return;
  }

  if (name === 'echo') {
    respond(message.id, {
      content: [
        {
          type: 'text',
          text: String(args.message ?? 'hello from mcp'),
        },
      ],
    });
    return;
  }

  respondError(message.id, -32602, `Unknown tool: ${name}`);
}

function readResource(message) {
  const uri = message.params?.uri;

  if (uri === 'memory:readme') {
    respond(message.id, {
      contents: [
        {
          uri,
          mimeType: 'text/markdown',
          text: '# local mcp\n\nThis resource came from `.agents/local-mcp.mjs`.',
        },
      ],
    });
    return;
  }

  if (uri === 'memory:status') {
    respond(message.id, {
      contents: [
        {
          uri,
          mimeType: 'application/json',
          text: JSON.stringify({ ok: true, server: 'local-linea' }),
        },
      ],
    });
    return;
  }

  respondError(message.id, -32602, `Unknown resource: ${uri}`);
}

function getPrompt(message) {
  const name = message.params?.name;
  const args = message.params?.arguments ?? {};
  const topic = String(args.topic ?? 'the current work');

  if (name === 'review') {
    respond(message.id, {
      messages: [
        {
          role: 'user',
          content: {
            type: 'text',
            text: `Review ${topic}. Focus on correctness, regressions, and missing tests.`,
          },
        },
      ],
    });
    return;
  }

  if (name === 'summarize') {
    respond(message.id, {
      messages: [
        {
          role: 'user',
          content: {
            type: 'text',
            text: `Summarize ${topic} in concise engineering terms.`,
          },
        },
      ],
    });
    return;
  }

  respondError(message.id, -32602, `Unknown prompt: ${name}`);
}

function respond(id, result) {
  write({ jsonrpc: '2.0', id, result });
}

function respondError(id, code, message) {
  write({ jsonrpc: '2.0', id, error: { code, message } });
}

function write(message) {
  const body = JSON.stringify(message);
  process.stdout.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
}
