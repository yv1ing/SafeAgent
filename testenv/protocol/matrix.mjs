// Exercises every client format against every upstream channel format. A failure is any protocol
// error, a missing terminal event, or content that did not survive the conversion.
import process from 'node:process';

const base = process.env.SAFEAGENT_BASE_URL ?? 'http://127.0.0.1:8080';
const key = process.env.SAFEAGENT_MATRIX_KEY;
if (!key) {
  console.error('SAFEAGENT_MATRIX_KEY is required');
  process.exit(2);
}

const formats = {
  chat: { path: '/v1/chat/completions', model: process.env.SAFEAGENT_CHAT_MODEL },
  responses: { path: '/v1/responses', model: process.env.SAFEAGENT_RESPONSES_MODEL },
  messages: { path: '/v1/messages', model: process.env.SAFEAGENT_MESSAGES_MODEL },
};

const tools = {
  chat: [
    {
      type: 'function',
      function: {
        name: 'get_weather',
        description: 'Get the current weather for a city',
        parameters: {
          type: 'object',
          properties: { city: { type: 'string' } },
          required: ['city'],
        },
      },
    },
  ],
  responses: [
    {
      type: 'function',
      name: 'get_weather',
      description: 'Get the current weather for a city',
      parameters: { type: 'object', properties: { city: { type: 'string' } }, required: ['city'] },
    },
  ],
  messages: [
    {
      name: 'get_weather',
      description: 'Get the current weather for a city',
      input_schema: {
        type: 'object',
        properties: { city: { type: 'string' } },
        required: ['city'],
      },
    },
  ],
};

function request(client, route, kind, stream) {
  const target = formats[route];
  const common = { model: target.model, stream };
  if (kind === 'text') {
    const marker = 'PONG';
    const body =
      client === 'chat'
        ? {
            ...common,
            max_tokens: 300,
            messages: [{ role: 'user', content: `Reply with exactly: ${marker}` }],
          }
        : client === 'responses'
          ? {
              ...common,
              max_output_tokens: 300,
              input: [
                {
                  role: 'user',
                  content: [{ type: 'input_text', text: `Reply with exactly: ${marker}` }],
                },
              ],
            }
          : {
              ...common,
              max_tokens: 300,
              messages: [{ role: 'user', content: `Reply with exactly: ${marker}` }],
            };
    return { body, expect: (value) => /\bPONG\b/i.test(value.text), marker };
  }
  if (kind === 'tool') {
    const prompt = 'Call get_weather for Paris. Do not answer with text.';
    const body =
      client === 'chat'
        ? {
            ...common,
            max_tokens: 300,
            tools: tools.chat,
            messages: [{ role: 'user', content: prompt }],
          }
        : client === 'responses'
          ? {
              ...common,
              max_output_tokens: 300,
              tools: tools.responses,
              input: [{ role: 'user', content: [{ type: 'input_text', text: prompt }] }],
            }
          : {
              ...common,
              max_tokens: 300,
              tools: tools.messages,
              messages: [{ role: 'user', content: prompt }],
            };
    return {
      body,
      expect: (value) =>
        value.toolCalls.some((call) => call.name === 'get_weather' && /Paris/.test(call.arguments)),
    };
  }
  if (kind === 'continuation') {
    const prompt = 'What is the weather in Paris? Use the tool.';
    const body =
      client === 'chat'
        ? {
            ...common,
            max_tokens: 120,
            tools: tools.chat,
            messages: [
              { role: 'user', content: prompt },
              {
                role: 'assistant',
                content: null,
                reasoning_content: 'I should call the weather tool.',
                tool_calls: [
                  {
                    id: 'call_matrix',
                    type: 'function',
                    function: { name: 'get_weather', arguments: '{"city":"Paris"}' },
                  },
                ],
              },
              { role: 'tool', tool_call_id: 'call_matrix', content: '22C and sunny' },
            ],
          }
        : client === 'responses'
          ? {
              ...common,
              max_output_tokens: 120,
              tools: tools.responses,
              input: [
                { role: 'user', content: [{ type: 'input_text', text: prompt }] },
                {
                  type: 'reasoning',
                  id: 'rs_matrix',
                  summary: [],
                  content: [{ type: 'reasoning_text', text: 'I should call the weather tool.' }],
                },
                {
                  type: 'function_call',
                  call_id: 'call_matrix',
                  name: 'get_weather',
                  arguments: '{"city":"Paris"}',
                },
                { type: 'function_call_output', call_id: 'call_matrix', output: '22C and sunny' },
              ],
            }
          : {
              ...common,
              max_tokens: 120,
              tools: tools.messages,
              messages: [
                { role: 'user', content: prompt },
                {
                  role: 'assistant',
                  content: [
                    { type: 'thinking', thinking: 'I should call the weather tool.' },
                    {
                      type: 'tool_use',
                      id: 'call_matrix',
                      name: 'get_weather',
                      input: { city: 'Paris' },
                    },
                  ],
                },
                {
                  role: 'user',
                  content: [
                    { type: 'tool_result', tool_use_id: 'call_matrix', content: '22C and sunny' },
                  ],
                },
              ],
            };
    return { body, expect: (value) => /22|sunny|warm/i.test(value.text) };
  }
  if (kind === 'parallel') {
    const prompt =
      'Call both tools now: get_weather for Paris and get_time for Tokyo. Do not answer with text.';
    const definitions = {
      chat: tools.chat.concat([
        {
          type: 'function',
          function: {
            name: 'get_time',
            description: 'Local time for a city',
            parameters: {
              type: 'object',
              properties: { city: { type: 'string' } },
              required: ['city'],
            },
          },
        },
      ]),
      responses: tools.responses.concat([
        {
          type: 'function',
          name: 'get_time',
          description: 'Local time for a city',
          parameters: {
            type: 'object',
            properties: { city: { type: 'string' } },
            required: ['city'],
          },
        },
      ]),
      messages: tools.messages.concat([
        {
          name: 'get_time',
          description: 'Local time for a city',
          input_schema: {
            type: 'object',
            properties: { city: { type: 'string' } },
            required: ['city'],
          },
        },
      ]),
    };
    const body =
      client === 'chat'
        ? {
            ...common,
            max_tokens: 350,
            tools: definitions.chat,
            messages: [{ role: 'user', content: prompt }],
          }
        : client === 'responses'
          ? {
              ...common,
              max_output_tokens: 350,
              tools: definitions.responses,
              input: [{ role: 'user', content: [{ type: 'input_text', text: prompt }] }],
            }
          : {
              ...common,
              max_tokens: 350,
              tools: definitions.messages,
              messages: [{ role: 'user', content: prompt }],
            };
    return {
      body,
      expect: (value) =>
        ['get_weather', 'get_time'].every((name) =>
          value.toolCalls.some((call) => call.name === name),
        ) &&
        value.toolCalls.some((call) => /Paris/.test(call.arguments)) &&
        value.toolCalls.some((call) => /Tokyo/.test(call.arguments)),
    };
  }
  if (kind === 'redaction') {
    const prompt = 'Repeat this address exactly and nothing else: alice@example.com';
    const body =
      client === 'chat'
        ? { ...common, max_tokens: 200, messages: [{ role: 'user', content: prompt }] }
        : client === 'responses'
          ? {
              ...common,
              max_output_tokens: 200,
              input: [{ role: 'user', content: [{ type: 'input_text', text: prompt }] }],
            }
          : { ...common, max_tokens: 200, messages: [{ role: 'user', content: prompt }] };
    return { body, expect: (value) => value.text.includes('alice@example.com') };
  }
  throw new Error(`unknown case ${kind}`);
}

function headers(client) {
  if (client === 'messages') {
    return {
      'content-type': 'application/json',
      'x-api-key': key,
      'anthropic-version': '2023-06-01',
    };
  }
  return { 'content-type': 'application/json', authorization: `Bearer ${key}` };
}

function addToolCall(value, call) {
  const existing = value.toolCalls.find((entry) => entry.id === call.id);
  if (existing) {
    existing.name = call.name || existing.name;
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

function collectChat(events) {
  const value = {
    text: '',
    reasoning: '',
    toolCalls: [],
    finish: '',
    usage: null,
    done: false,
    error: '',
  };
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
  const value = {
    text: '',
    reasoning: '',
    toolCalls: [],
    finish: '',
    usage: null,
    done: false,
    error: '',
  };
  for (const { event, data } of events) {
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
  const value = {
    text: '',
    reasoning: '',
    toolCalls: [],
    finish: '',
    usage: null,
    done: false,
    error: '',
  };
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
    }
    if (event === 'message_delta') {
      value.finish = frame.delta?.stop_reason ?? value.finish;
      if (frame.usage) value.usage = { ...(value.usage ?? {}), ...frame.usage };
    }
    if (event === 'message_stop') value.done = true;
  }
  return value;
}

async function call(client, route, kind, stream) {
  const { body, expect, marker } = request(client, route, kind, stream);
  const response = await fetch(base + formats[client].path, {
    method: 'POST',
    headers: headers(client),
    body: JSON.stringify(body),
  });
  const raw = await response.text();
  if (!response.ok)
    return { okay: false, detail: `HTTP ${response.status}: ${raw.slice(0, 500)}`, marker };
  if (!stream) {
    let parsed;
    try {
      parsed = JSON.parse(raw);
    } catch {
      return { okay: false, detail: `non-JSON response: ${raw.slice(0, 300)}` };
    }
    const value =
      client === 'chat'
        ? {
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
          }
        : client === 'responses'
          ? {
              text: (parsed.output ?? [])
                .flatMap((item) =>
                  (item.content ?? [])
                    .filter((part) => part.type === 'output_text')
                    .map((part) => part.text),
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
                .filter((item) => item.type === 'function_call' || item.type === 'custom_tool_call')
                .map((item) => ({
                  id: item.call_id,
                  name: item.name,
                  arguments: item.arguments ?? item.input ?? '',
                })),
              finish: parsed.status ?? '',
              usage: parsed.usage,
              done: true,
              error: parsed.error ? JSON.stringify(parsed.error) : '',
            }
          : {
              text: (parsed.content ?? [])
                .filter((block) => block.type === 'text')
                .map((block) => block.text)
                .join(''),
              reasoning: (parsed.content ?? [])
                .filter((block) => block.type === 'thinking')
                .map((block) => block.thinking)
                .join(''),
              toolCalls: (parsed.content ?? [])
                .filter((block) => block.type === 'tool_use')
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
    const okay = !value.error && value.done && expect(value);
    return {
      okay,
      detail: okay
        ? summarize(value)
        : `${value.error || 'expectation failed'} | text=${value.text.slice(0, 200)} tools=${JSON.stringify(value.toolCalls)}`,
      marker,
    };
  }
  const events = parseSse(raw);
  const value =
    client === 'chat'
      ? collectChat(events)
      : client === 'responses'
        ? collectResponses(events)
        : collectMessages(events);
  const terminal = value.done;
  const okay = !value.error && terminal && expect(value);
  return {
    okay,
    detail: okay
      ? summarize(value)
      : `${value.error || (terminal ? 'expectation failed' : 'missing terminal event')} | text=${value.text.slice(0, 200)} tools=${JSON.stringify(value.toolCalls)}`,
  };
}

function summarize(value) {
  const usage = value.usage
    ? ` usage=${value.usage.input_tokens ?? value.usage.prompt_tokens ?? '?'}/${value.usage.output_tokens ?? value.usage.completion_tokens ?? '?'}`
    : '';
  return `text=${JSON.stringify(value.text.slice(0, 60))} tools=${value.toolCalls.length} finish=${value.finish}${usage}`;
}

const clients = ['chat', 'responses', 'messages'];
const routes = ['chat', 'responses', 'messages'];
const cases = ['text', 'tool', 'parallel', 'continuation', 'redaction'];
const failures = [];
let passed = 0;
for (const stream of [true, false]) {
  for (const kind of cases) {
    for (const client of clients) {
      for (const route of routes) {
        const label = `${stream ? 'stream' : 'json'} ${kind} ${client}->${route}`;
        let result;
        let attempts = 0;
        for (; attempts < 3; attempts++) {
          try {
            result = await call(client, route, kind, stream);
          } catch (error) {
            result = { okay: false, detail: String(error) };
          }
          if (result.okay) break;
        }
        if (result.okay) {
          passed++;
          console.log(
            `PASS ${label}${attempts > 0 ? ` (retry ${attempts})` : ''} ${result.detail}`,
          );
        } else {
          failures.push({ label, detail: result.detail });
          console.log(`FAIL ${label} ${result.detail}`);
        }
      }
    }
  }
}
console.log(`\n${passed} passed, ${failures.length} failed`);
if (failures.length) {
  console.log(JSON.stringify(failures, null, 2));
  process.exit(1);
}
