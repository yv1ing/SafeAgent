// Shared client, console and case helpers for the protocol acceptance suites.
import process from 'node:process';

const base = (process.env.SAFEAGENT_BASE_URL ?? 'http://127.0.0.1:8080').replace(/\/+$/, '');
const key = process.env.SAFEAGENT_MATRIX_KEY;
const project = process.env.SAFEAGENT_PROJECT;
const adminPassword = process.env.SAFEAGENT_ADMIN_PASSWORD;
const routes = (process.env.SAFEAGENT_AVAILABLE_FORMATS ?? '').split(/\s+/).filter(Boolean);
if (!key || !project || !adminPassword) {
  console.error(
    'SAFEAGENT_MATRIX_KEY, SAFEAGENT_PROJECT and SAFEAGENT_ADMIN_PASSWORD are required',
  );
  process.exit(2);
}

const clientFormats = ['chat', 'responses', 'messages', 'gemini'];
const routeNames = {};
for (const format of clientFormats) {
  const value = process.env[`SAFEAGENT_${format.toUpperCase()}_ROUTE`];
  if (value) routeNames[format] = value;
}
const routeName = (format) => process.env[`SAFEAGENT_${format.toUpperCase()}_ROUTE`];

export { base, key, project, adminPassword, routes, routeName, routeNames };

function addToolCall(value, call) {
  const existing = value.toolCalls.find((entry) => entry.id === call.id && call.id);
  if (existing) {
    if (call.name) existing.name = call.name;
    existing.arguments += call.arguments ?? '';
    return;
  }
  value.toolCalls.push({ id: call.id, name: call.name ?? '', arguments: call.arguments ?? '' });
}

function parseSse(raw) {
  const blocks = raw.split(/\n\n/);
  const events = [];
  for (const block of blocks) {
    let event = '';
    let data = '';
    for (const line of block.split('\n')) {
      if (line.startsWith('event:')) event = line.slice(6).trim();
      if (line.startsWith('data:')) data += line.slice(5).trim();
    }
    if (data) events.push({ event, data });
  }
  return events;
}

function emptyValue() {
  return {
    text: '',
    reasoning: '',
    toolCalls: [],
    finish: '',
    usage: null,
    done: false,
    error: '',
  };
}

function collectChat(events) {
  const value = emptyValue();
  for (const { data } of events) {
    if (data === '[DONE]') {
      value.done = true;
      continue;
    }
    let frame;
    try {
      frame = JSON.parse(data);
    } catch {
      value.error = `invalid SSE JSON: ${data.slice(0, 120)}`;
      continue;
    }
    if (frame.error) value.error = JSON.stringify(frame.error);
    if (frame.usage) value.usage = frame.usage;
    for (const choice of frame.choices ?? []) {
      const delta = choice.delta ?? {};
      if (delta.content) value.text += delta.content;
      if (delta.reasoning_content) value.reasoning += delta.reasoning_content;
      for (const call of delta.tool_calls ?? []) {
        const index = call.index ?? 0;
        let target = value.toolCalls[index];
        if (!target) {
          target = { id: call.id || `chat-${index}`, name: '', arguments: '' };
          value.toolCalls[index] = target;
        }
        if (call.id) target.id = call.id;
        if (call.function?.name) target.name = call.function.name;
        if (call.function?.arguments) target.arguments += call.function.arguments;
      }
      if (choice.finish_reason) value.finish = choice.finish_reason;
    }
  }
  return value;
}

function collectResponses(events) {
  const value = emptyValue();
  for (const { data } of events) {
    let frame;
    try {
      frame = JSON.parse(data);
    } catch {
      value.error = `invalid SSE JSON: ${data.slice(0, 120)}`;
      continue;
    }
    if (frame.error || frame.type === 'error') value.error = JSON.stringify(frame.error ?? frame);
    if (frame.type === 'response.output_text.delta') value.text += frame.delta;
    if (
      frame.type === 'response.reasoning_text.delta' ||
      frame.type === 'response.reasoning_summary_text.delta'
    )
      value.reasoning += frame.delta;
    if (frame.type === 'response.output_item.done') {
      const item = frame.item ?? {};
      if (item.type === 'function_call')
        addToolCall(value, { id: item.call_id, name: item.name, arguments: item.arguments });
      if (item.type === 'custom_tool_call')
        addToolCall(value, { id: item.call_id, name: item.name, arguments: item.input });
      if (item.type === 'web_search_call' || item.type === 'code_interpreter_call')
        addToolCall(value, { id: item.id, name: item.type, arguments: '{}' });
    }
    if (
      frame.type === 'response.completed' ||
      frame.type === 'response.incomplete' ||
      frame.type === 'response.failed'
    ) {
      value.done = true;
      value.finish = frame.response?.status ?? frame.type;
      value.usage = frame.response?.usage ?? value.usage;
      if (frame.response?.error) value.error = JSON.stringify(frame.response.error);
    }
  }
  return value;
}

function collectMessages(events) {
  const value = emptyValue();
  const blocks = new Map();
  for (const { event, data } of events) {
    if (data === '[DONE]') {
      value.done = true;
      continue;
    }
    let frame;
    try {
      frame = JSON.parse(data);
    } catch {
      value.error = `invalid SSE JSON: ${data.slice(0, 120)}`;
      continue;
    }
    if (frame.type === 'error') value.error = JSON.stringify(frame.error ?? frame);
    if (event === 'message_start') value.usage = frame.message?.usage ?? value.usage;
    if (event === 'content_block_start') blocks.set(frame.index, frame.content_block ?? {});
    if (event === 'content_block_delta') {
      const delta = frame.delta ?? {};
      if (delta.type === 'text_delta') value.text += delta.text;
      if (delta.type === 'thinking_delta') value.reasoning += delta.thinking;
      if (delta.type === 'input_json_delta') {
        const block = blocks.get(frame.index) ?? {};
        block.partial = (block.partial ?? '') + (delta.partial_json ?? '');
      }
    }
    if (event === 'content_block_stop') {
      const block = blocks.get(frame.index);
      if (block?.type === 'tool_use')
        addToolCall(value, { id: block.id, name: block.name, arguments: block.partial ?? '{}' });
      if (block?.type === 'server_tool_use' || block?.type === 'mcp_tool_use')
        addToolCall(value, { id: block.id, name: block.name, arguments: block.partial ?? '{}' });
    }
    if (event === 'message_delta') {
      value.finish = frame.delta?.stop_reason ?? value.finish;
      if (frame.usage) value.usage = { ...(value.usage ?? {}), ...frame.usage };
    }
    if (event === 'message_stop') value.done = true;
  }
  return value;
}

function collectGemini(events) {
  const value = emptyValue();
  const frames = events.length ? events : [{ data: '' }];
  for (const { data } of frames) {
    if (!data) continue;
    let frame;
    try {
      frame = JSON.parse(data);
    } catch {
      value.error = `invalid SSE JSON: ${data.slice(0, 120)}`;
      continue;
    }
    if (frame.error) {
      value.error = JSON.stringify(frame.error);
      continue;
    }
    if (frame.usageMetadata) value.usage = frame.usageMetadata;
    for (const candidate of frame.candidates ?? []) {
      value.finish = candidate.finishReason ?? value.finish;
      for (const part of candidate.content?.parts ?? []) {
        if (part.thought) value.reasoning += part.text ?? '';
        else if (part.text) value.text += part.text;
        if (part.functionCall)
          addToolCall(value, {
            id: part.functionCall.id,
            name: part.functionCall.name,
            arguments: JSON.stringify(part.functionCall.args ?? {}),
          });
      }
    }
    if ((frame.candidates ?? []).length > 0) value.done = true;
  }
  return value;
}

function chatValue(parsed) {
  return {
    text: parsed.choices?.[0]?.message?.content ?? '',
    reasoning: parsed.choices?.[0]?.message?.reasoning_content ?? '',
    toolCalls: (parsed.choices?.[0]?.message?.tool_calls ?? []).map((entry) => ({
      id: entry.id,
      name: entry.function?.name,
      arguments: entry.function?.arguments ?? '',
    })),
    finish: parsed.choices?.[0]?.finish_reason ?? '',
    usage: parsed.usage,
    done: true,
    error: '',
  };
}

function responsesValue(parsed) {
  return {
    text: (parsed.output ?? [])
      .flatMap((item) =>
        (item.content ?? []).filter((part) => part.type === 'output_text').map((part) => part.text),
      )
      .join(''),
    reasoning: (parsed.output ?? [])
      .flatMap((item) =>
        (item.content ?? [])
          .filter((part) => part.type === 'reasoning_text')
          .map((part) => part.text),
      )
      .join(''),
    toolCalls: (parsed.output ?? [])
      .filter(
        (item) =>
          item.type === 'function_call' ||
          item.type === 'custom_tool_call' ||
          item.type === 'web_search_call' ||
          item.type === 'code_interpreter_call',
      )
      .map((item) => ({
        id: item.call_id ?? item.id,
        name: item.type === 'function_call' ? item.name : item.type,
        arguments: item.arguments ?? item.input ?? '{}',
      })),
    finish: parsed.status ?? '',
    usage: parsed.usage,
    done: true,
    error: parsed.error ? JSON.stringify(parsed.error) : '',
  };
}

function messagesValue(parsed) {
  return {
    text: (parsed.content ?? [])
      .filter((block) => block.type === 'text')
      .map((block) => block.text)
      .join(''),
    reasoning: (parsed.content ?? [])
      .filter((block) => block.type === 'thinking')
      .map((block) => block.thinking)
      .join(''),
    toolCalls: (parsed.content ?? [])
      .filter(
        (block) =>
          block.type === 'tool_use' ||
          block.type === 'server_tool_use' ||
          block.type === 'mcp_tool_use',
      )
      .map((block) => ({
        id: block.id,
        name: block.name,
        arguments: JSON.stringify(block.input ?? {}),
      })),
    finish: parsed.stop_reason ?? '',
    usage: parsed.usage,
    done: true,
    error: parsed.error ? JSON.stringify(parsed.error) : '',
  };
}

function geminiValue(parsed) {
  const candidate = parsed.candidates?.[0];
  return {
    text: (candidate?.content?.parts ?? [])
      .filter((part) => !part.thought)
      .map((part) => part.text ?? '')
      .join(''),
    reasoning: (candidate?.content?.parts ?? [])
      .filter((part) => part.thought)
      .map((part) => part.text ?? '')
      .join(''),
    toolCalls: (candidate?.content?.parts ?? [])
      .filter((part) => part.functionCall)
      .map((part) => ({
        id: part.functionCall.id,
        name: part.functionCall.name,
        arguments: JSON.stringify(part.functionCall.args ?? {}),
      })),
    finish: candidate?.finishReason ?? '',
    usage: parsed.usageMetadata,
    done: true,
    error: parsed.error ? JSON.stringify(parsed.error) : '',
  };
}

export const clientSpecs = {
  chat: {
    path: '/v1/chat/completions',
    authHeaders: () => ({ 'content-type': 'application/json', authorization: `Bearer ${key}` }),
    endpoint: () => `${base}/v1/chat/completions`,
    body: (route, stream, content, extra = {}) => ({
      model: routeNames[route],
      stream,
      max_tokens: extra.max_tokens ?? 300,
      messages: [{ role: 'user', content }],
      ...extra,
    }),
    json: chatValue,
  },
  responses: {
    path: '/v1/responses',
    authHeaders: () => ({ 'content-type': 'application/json', authorization: `Bearer ${key}` }),
    endpoint: () => `${base}/v1/responses`,
    body: (route, stream, content, extra = {}) => ({
      model: routeNames[route],
      stream,
      max_output_tokens: extra.max_output_tokens ?? 300,
      input: [{ role: 'user', content: [{ type: 'input_text', text: content }] }],
      ...extra,
    }),
    json: responsesValue,
  },
  messages: {
    path: '/v1/messages',
    authHeaders: () => ({
      'content-type': 'application/json',
      'x-api-key': key,
      'anthropic-version': '2023-06-01',
    }),
    endpoint: () => `${base}/v1/messages`,
    body: (route, stream, content, extra = {}) => ({
      model: routeNames[route],
      stream,
      max_tokens: extra.max_tokens ?? 300,
      messages: [{ role: 'user', content }],
      ...extra,
    }),
    json: messagesValue,
  },
  gemini: {
    path: '/v1beta/models',
    authHeaders: () => ({ 'content-type': 'application/json', 'x-goog-api-key': key }),
    endpoint: (route, stream) =>
      `${base}/v1beta/models/${routeNames[route]}:${stream ? 'streamGenerateContent?alt=sse' : 'generateContent'}`,
    body: (route, stream, content, extra = {}) => ({
      contents: [{ role: 'user', parts: [{ text: content }] }],
      generationConfig: { maxOutputTokens: extra.max_output_tokens ?? 300 },
      ...extra,
    }),
    json: geminiValue,
  },
};

export async function send(client, route, body, stream = false, extraHeaders = {}) {
  const spec = clientSpecs[client];
  const response = await fetch(spec.endpoint(route, stream), {
    method: 'POST',
    headers: { ...spec.authHeaders(), ...extraHeaders },
    body: JSON.stringify(body),
  });
  return { status: response.status, text: await response.text() };
}

export function parseResponse(client, raw, stream) {
  if (!stream) {
    let parsed;
    try {
      parsed = JSON.parse(raw);
    } catch {
      return null;
    }
    return clientSpecs[client].json(parsed);
  }
  const events = parseSse(raw);
  if (client === 'chat') return collectChat(events);
  if (client === 'responses') return collectResponses(events);
  if (client === 'messages') return collectMessages(events);
  return collectGemini(events);
}

export async function gatewayRequest(method, path, body, headers = {}) {
  const response = await fetch(`${base}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${key}`,
      'Content-Type': 'application/json',
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  let payload = null;
  try {
    payload = text ? JSON.parse(text) : null;
  } catch {
    payload = text;
  }
  return { status: response.status, payload, text };
}

let cookie = '';

export async function login() {
  const response = await fetch(`${base}/console/api/session`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'X-SafeAgent-Request': '1', Origin: base },
    body: JSON.stringify({ username: 'admin', password: adminPassword }),
  });
  if (!response.ok) throw new Error(`console login failed: HTTP ${response.status}`);
  cookie = (response.headers.get('set-cookie') ?? '').split(';')[0];
}

export async function consoleRequest(method, path, body) {
  const response = await fetch(`${base}/console/api${path}`, {
    method,
    headers: {
      cookie,
      'content-type': 'application/json',
      'X-SafeAgent-Request': '1',
      Origin: base,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  let payload = null;
  try {
    payload = text ? JSON.parse(text) : null;
  } catch {
    payload = text;
  }
  return { status: response.status, payload };
}

export async function consoleGet(path) {
  const response = await consoleRequest('GET', path);
  return response.status >= 200 && response.status < 300 ? response.payload : null;
}

export async function listCalls(path = `/projects/${project}/calls?limit=100`) {
  const page = await consoleGet(path);
  return page?.items ?? [];
}

export async function pollNewCall(before, predicate, attempts = 20, interval = 500) {
  for (let attempt = 0; attempt < attempts; attempt++) {
    const found = (await listCalls()).find((call) => !before.has(call.id) && predicate(call));
    if (found) return found;
    await new Promise((resolve) => setTimeout(resolve, interval));
  }
  return null;
}

export const counters = { passed: 0, failed: 0, skipped: 0, failures: [] };

export function recordPass() {
  counters.passed += 1;
}

export function recordFail(name, detail = '') {
  counters.failed += 1;
  counters.failures.push(detail ? `${name}: ${detail}` : name);
}

export function recordSkip() {
  counters.skipped += 1;
}

export function summary() {
  const skipped = counters.skipped > 0 ? `, ${counters.skipped} skipped` : '';
  console.log(`\n${counters.passed} passed, ${counters.failed} failed${skipped}`);
}
