// Exercises every available client format against every available channel format.
// A failure is a protocol error, a missing terminal event, or content that did not survive.
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const base = process.env.SAFEAGENT_BASE_URL ?? 'http://127.0.0.1:8080';
const key = process.env.SAFEAGENT_MATRIX_KEY;
const project = process.env.SAFEAGENT_PROJECT;
const adminPassword = process.env.SAFEAGENT_ADMIN_PASSWORD;
if (!key || !project || !adminPassword) {
  console.error(
    'SAFEAGENT_MATRIX_KEY, SAFEAGENT_PROJECT and SAFEAGENT_ADMIN_PASSWORD are required',
  );
  process.exit(2);
}

const allFormats = ['chat', 'responses', 'messages', 'gemini'];
const routes = (process.env.SAFEAGENT_AVAILABLE_FORMATS ?? '').split(/\s+/).filter(Boolean);
if (routes.length === 0) {
  console.error('SAFEAGENT_AVAILABLE_FORMATS is empty');
  process.exit(2);
}

const routeName = {};
for (const format of allFormats) {
  const value = process.env[`SAFEAGENT_${format.toUpperCase()}_ROUTE`];
  if (value) routeName[format] = value;
}

const clients = allFormats;
const enabled = (name, fallback) => (process.env[name] ?? fallback) !== '0';
const cases = [
  { kind: 'text', retry: 1 },
  { kind: 'tool', retry: 3 },
  { kind: 'parallel', retry: 3 },
  { kind: 'continuation', retry: 3 },
  { kind: 'redaction', retry: 1, verifyRedaction: true },
];
if (enabled('SAFEAGENT_TEST_REASONING', '1'))
  cases.push({ kind: 'reasoning', retry: 2, optional: true });
if (enabled('SAFEAGENT_TEST_MEDIA', '1')) cases.push({ kind: 'media', retry: 1, optional: true });
if (enabled('SAFEAGENT_TEST_STRUCTURED', '1'))
  cases.push({ kind: 'structured', retry: 2, optional: true });
if (enabled('SAFEAGENT_TEST_SERVER_TOOLS', '1'))
  cases.push({ kind: 'server_tool', retry: 2, optional: true });
cases.push({ kind: 'reject', retry: 1, noUpstream: true });

const chatTools = [
  {
    type: 'function',
    function: {
      name: 'get_weather',
      description: 'Get the current weather for a city',
      parameters: { type: 'object', properties: { city: { type: 'string' } }, required: ['city'] },
    },
  },
];
const chatTimeTool = {
  type: 'function',
  function: {
    name: 'get_time',
    description: 'Local time for a city',
    parameters: { type: 'object', properties: { city: { type: 'string' } }, required: ['city'] },
  },
};
const responsesTools = chatTools.map((tool) => ({
  type: 'function',
  name: tool.function.name,
  description: tool.function.description,
  parameters: tool.function.parameters,
}));
const responsesTimeTool = {
  type: 'function',
  name: chatTimeTool.function.name,
  description: chatTimeTool.function.description,
  parameters: chatTimeTool.function.parameters,
};
const messagesTools = chatTools.map((tool) => ({
  name: tool.function.name,
  description: tool.function.description,
  input_schema: tool.function.parameters,
}));
const messagesTimeTool = {
  name: chatTimeTool.function.name,
  description: chatTimeTool.function.description,
  input_schema: chatTimeTool.function.parameters,
};
const geminiTools = [
  {
    functionDeclarations: chatTools.map((tool) => ({
      name: tool.function.name,
      description: tool.function.description,
      parameters: tool.function.parameters,
    })),
  },
];
const geminiTimeTool = [
  {
    functionDeclarations: [chatTools[0], chatTimeTool].map((tool) => ({
      name: tool.function.name,
      description: tool.function.description,
      parameters: tool.function.parameters,
    })),
  },
];

function chatBody(route, stream, content, extra = {}) {
  return {
    model: routeName[route],
    stream,
    max_tokens: extra.max_tokens ?? 300,
    messages: [{ role: 'user', content }],
    ...extra,
  };
}

function responsesBody(route, stream, content, extra = {}) {
  return {
    model: routeName[route],
    stream,
    max_output_tokens: extra.max_output_tokens ?? 300,
    input: [{ role: 'user', content: [{ type: 'input_text', text: content }] }],
    ...extra,
  };
}

function messagesBody(route, stream, content, extra = {}) {
  return {
    model: routeName[route],
    stream,
    max_tokens: extra.max_tokens ?? 300,
    messages: [{ role: 'user', content }],
    ...extra,
  };
}

function geminiBody(route, stream, content, extra = {}) {
  return {
    contents: [{ role: 'user', parts: [{ text: content }] }],
    generationConfig: { maxOutputTokens: extra.max_output_tokens ?? 300 },
    ...extra,
  };
}

function promptFor(kind) {
  switch (kind) {
    case 'text':
      return 'Reply with exactly: PONG';
    case 'reasoning':
      return 'A box has 3 red and 4 blue balls. Two balls are drawn without replacement. What is the probability both are blue? Reply with the fraction only.';
    case 'media':
      return 'What single color fills the image? Reply with the color name only.';
    case 'structured':
      return 'Return JSON with the key answer and the value 42. Do not add any other text.';
    case 'tool':
      return 'Call get_weather for Paris. Do not answer with text.';
    case 'parallel':
      return 'Call both tools now: get_weather for Paris and get_time for Tokyo. Do not answer with text.';
    case 'continuation':
      return 'What is the weather in Paris? Use the tool result.';
    case 'redaction':
      return 'Repeat this address exactly and nothing else: alice@example.com';
    case 'server_tool':
      return 'Search the web for the current population of Paris and answer with the number.';
    default:
      return 'hello';
  }
}

function requestFor(client, route, kind, stream) {
  const prompt = promptFor(kind);
  if (kind === 'text') {
    return {
      body:
        client === 'chat'
          ? chatBody(route, stream, prompt)
          : client === 'responses'
            ? responsesBody(route, stream, prompt)
            : client === 'messages'
              ? messagesBody(route, stream, prompt)
              : geminiBody(route, stream, prompt),
      expect: (value) => /\bPONG\b/i.test(value.text),
    };
  }
  if (kind === 'reasoning') {
    return {
      body:
        client === 'chat'
          ? chatBody(route, stream, prompt, { max_tokens: 900 })
          : client === 'responses'
            ? responsesBody(route, stream, prompt, {
                max_output_tokens: 900,
                reasoning: { effort: 'low', summary: 'auto' },
              })
            : client === 'messages'
              ? messagesBody(route, stream, prompt, { max_tokens: 900 })
              : geminiBody(route, stream, prompt, {
                  max_output_tokens: 900,
                  generationConfig: {
                    maxOutputTokens: 900,
                    thinkingConfig: { includeThoughts: true, thinkingBudget: 512 },
                  },
                }),
      expect: (value) => /\b(2\/7|two sevenths)\b/i.test(value.text) || /0\.28/.test(value.text),
      optional: (value) => value.reasoning.length === 0,
    };
  }
  if (kind === 'media') {
    const png =
      'iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAGUlEQVR4nGP4z8DwnxLMMGrAqAGjBgwXAwAwxP4QHCfkAAAAAABJRU5ErkJggg==';
    const body =
      client === 'chat'
        ? chatBody(route, stream, [
            { type: 'text', text: prompt },
            { type: 'image_url', image_url: { url: `data:image/png;base64,${png}` } },
          ])
        : client === 'responses'
          ? responsesBody(route, stream, prompt, {
              input: [
                {
                  role: 'user',
                  content: [
                    { type: 'input_text', text: prompt },
                    { type: 'input_image', image_url: `data:image/png;base64,${png}` },
                  ],
                },
              ],
            })
          : client === 'messages'
            ? messagesBody(route, stream, [
                { type: 'text', text: prompt },
                {
                  type: 'image',
                  source: { type: 'base64', media_type: 'image/png', data: png },
                },
              ])
            : geminiBody(route, stream, prompt, {
                contents: [
                  {
                    role: 'user',
                    parts: [{ text: prompt }, { inlineData: { mimeType: 'image/png', data: png } }],
                  },
                ],
              });
    return {
      body,
      expect: (value) => /white|red|black|transparent/i.test(value.text),
      optional: (value, response) =>
        response === 422 || /unsupported|not supported|media|image|vision/i.test(value.error ?? ''),
    };
  }
  if (kind === 'structured') {
    if (client === 'messages') return null;
    return {
      body:
        client === 'chat'
          ? chatBody(route, stream, prompt, {
              response_format: { type: 'json_object' },
            })
          : client === 'responses'
            ? responsesBody(route, stream, prompt, {
                text: { format: { type: 'json_object' } },
              })
            : client === 'messages'
              ? messagesBody(route, stream, prompt)
              : geminiBody(route, stream, prompt, {
                  generationConfig: { maxOutputTokens: 300, responseMimeType: 'application/json' },
                }),
      expect: (value) => {
        try {
          return JSON.parse(value.text).answer === 42;
        } catch {
          return false;
        }
      },
      optional: (value, response) =>
        response === 422 || /unsupported|response_format|json/i.test(value.error ?? ''),
    };
  }
  if (kind === 'tool') {
    return {
      body:
        client === 'chat'
          ? chatBody(route, stream, prompt, { tools: chatTools })
          : client === 'responses'
            ? responsesBody(route, stream, prompt, { tools: responsesTools })
            : client === 'messages'
              ? messagesBody(route, stream, prompt, { tools: messagesTools })
              : geminiBody(route, stream, prompt, { tools: geminiTools }),
      expect: (value) =>
        value.toolCalls.some((call) => call.name === 'get_weather' && /Paris/.test(call.arguments)),
    };
  }
  if (kind === 'parallel') {
    return {
      body:
        client === 'chat'
          ? chatBody(route, stream, prompt, {
              tools: chatTools.concat([chatTimeTool]),
            })
          : client === 'responses'
            ? responsesBody(route, stream, prompt, {
                tools: responsesTools.concat([responsesTimeTool]),
              })
            : client === 'messages'
              ? messagesBody(route, stream, prompt, {
                  tools: messagesTools.concat([messagesTimeTool]),
                })
              : geminiBody(route, stream, prompt, { tools: geminiTimeTool }),
      expect: (value) =>
        ['get_weather', 'get_time'].every((name) =>
          value.toolCalls.some((call) => call.name === name),
        ) &&
        value.toolCalls.some((call) => /Paris/.test(call.arguments)) &&
        value.toolCalls.some((call) => /Tokyo/.test(call.arguments)),
    };
  }
  if (kind === 'continuation') {
    const history =
      client === 'chat'
        ? chatBody(route, stream, prompt, {
            max_tokens: 160,
            tools: chatTools,
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
          })
        : client === 'responses'
          ? responsesBody(route, stream, prompt, {
              max_output_tokens: 160,
              tools: responsesTools,
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
            })
          : client === 'messages'
            ? messagesBody(route, stream, prompt, {
                max_tokens: 160,
                tools: messagesTools,
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
              })
            : geminiBody(route, stream, prompt, {
                contents: [
                  { role: 'user', parts: [{ text: prompt }] },
                  {
                    role: 'model',
                    parts: [
                      { text: 'I should call the weather tool.', thought: true },
                      { functionCall: { name: 'get_weather', args: { city: 'Paris' } } },
                    ],
                  },
                  {
                    role: 'user',
                    parts: [
                      {
                        functionResponse: {
                          name: 'get_weather',
                          response: { content: '22C and sunny' },
                        },
                      },
                    ],
                  },
                ],
                tools: geminiTools,
              });
    return { body: history, expect: (value) => /22|sunny|warm/i.test(value.text) };
  }
  if (kind === 'redaction') {
    return {
      body:
        client === 'chat'
          ? chatBody(route, stream, prompt, { max_tokens: 200 })
          : client === 'responses'
            ? responsesBody(route, stream, prompt, { max_output_tokens: 200 })
            : client === 'messages'
              ? messagesBody(route, stream, prompt, { max_tokens: 200 })
              : geminiBody(route, stream, prompt, { max_output_tokens: 200 }),
      expect: (value) => value.text.includes('alice@example.com'),
    };
  }
  if (kind === 'server_tool') {
    if (client !== 'responses' && client !== 'messages') return null;
    if (client === 'responses') {
      return {
        body: responsesBody(route, stream, prompt, { tools: [{ type: 'web_search' }] }),
        expect: (value) => value.toolCalls.length > 0 || value.text.length > 0,
        optional: (value, response) =>
          response === 422 ||
          /unsupported|not supported|server tool|web_search|web search/i.test(value.error ?? '') ||
          (value.toolCalls.length === 0 && value.text.trim() === '' && value.reasoning.length > 0),
      };
    }
    return {
      body: messagesBody(route, stream, prompt, {
        tools: [{ type: 'web_search_20250305', name: 'web_search', max_uses: 1 }],
      }),
      expect: (value) => value.toolCalls.length > 0 || value.text.length > 0,
      optional: (value, response) =>
        response === 422 ||
        /unsupported|not supported|server tool|web_search|web search/i.test(value.error ?? '') ||
        (value.toolCalls.length === 0 && value.text.trim() === '' && value.reasoning.length > 0),
    };
  }
  if (kind === 'reject') {
    if (client === 'chat') {
      return {
        body: chatBody(route, stream, 'hello', { n: 2 }),
        reject: { status: 422, code: 'unsupported_multi_choice' },
      };
    }
    if (client === 'gemini') {
      return {
        body: geminiBody(route, stream, 'hello', {
          generationConfig: { maxOutputTokens: 16, candidateCount: 2 },
        }),
        reject: { status: 422, message: 'Multiple candidates are not supported.' },
      };
    }
    if (client === 'responses') {
      return {
        body: responsesBody(route, stream, 'hello', { store: true }),
        reject: { status: 422, code: 'state_not_supported' },
      };
    }
    return {
      body: messagesBody(route, stream, 'hello'),
      reject: { code: 'unsupported' },
      skip: true,
    };
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
  if (client === 'gemini') {
    return { 'content-type': 'application/json', 'x-goog-api-key': key };
  }
  return { 'content-type': 'application/json', authorization: `Bearer ${key}` };
}

function endpoint(client, route, stream) {
  if (client === 'chat') return `${base}/v1/chat/completions`;
  if (client === 'responses') return `${base}/v1/responses`;
  if (client === 'messages') return `${base}/v1/messages`;
  const action = stream ? 'streamGenerateContent?alt=sse' : 'generateContent';
  return `${base}/v1beta/models/${routeName[route]}:${action}`;
}

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
  const value = {
    text: '',
    reasoning: '',
    toolCalls: [],
    finish: '',
    usage: null,
    done: false,
    error: '',
  };
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

function summarize(value) {
  const usage = value.usage
    ? ` usage=${value.usage.input_tokens ?? value.usage.prompt_tokens ?? value.usage.promptTokenCount ?? '?'}/${value.usage.output_tokens ?? value.usage.completion_tokens ?? value.usage.candidatesTokenCount ?? '?'}`
    : '';
  return `text=${JSON.stringify(value.text.slice(0, 60))} tools=${value.toolCalls.length} finish=${value.finish}${reasoning(value)}${usage}`;
}

function reasoning(value) {
  return value.reasoning ? ' reasoning=yes' : '';
}

function jsonValue(client, parsed) {
  if (client === 'chat') {
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
  if (client === 'responses') {
    return {
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
  if (client === 'messages') {
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

async function call(client, route, kind, stream) {
  const definition = requestFor(client, route, kind, stream);
  if (!definition) return { skip: true, detail: 'client does not support this case' };
  if (definition.skip) return { skip: true, detail: 'case is not defined for this client' };
  const response = await fetch(endpoint(client, route, stream), {
    method: 'POST',
    headers: headers(client),
    body: JSON.stringify(definition.body),
  });
  const raw = await response.text();
  if (definition.reject) {
    if (!response.ok) {
      const expected = definition.reject;
      let okay = true;
      if (expected instanceof RegExp) okay = expected.test(raw);
      if (expected.status) okay = okay && response.status === expected.status;
      if (expected.code) okay = okay && raw.includes(expected.code);
      if (expected.message) okay = okay && raw.includes(expected.message);
      return {
        okay,
        detail: okay
          ? `rejected HTTP ${response.status}`
          : `HTTP ${response.status}: ${raw.slice(0, 300)}`,
      };
    }
    return {
      okay: false,
      detail: `expected rejection, got HTTP ${response.status}: ${raw.slice(0, 300)}`,
    };
  }
  if (!response.ok) {
    const optional = definition.optional?.({ error: raw }, response.status);
    return {
      okay: false,
      skip: Boolean(optional),
      detail: `HTTP ${response.status}: ${raw.slice(0, 500)}`,
    };
  }
  let value;
  if (!stream) {
    let parsed;
    try {
      parsed = JSON.parse(raw);
    } catch {
      return { okay: false, detail: `non-JSON response: ${raw.slice(0, 300)}` };
    }
    value = jsonValue(client, parsed);
  } else {
    const events = parseSse(raw);
    value =
      client === 'chat'
        ? collectChat(events)
        : client === 'responses'
          ? collectResponses(events)
          : client === 'messages'
            ? collectMessages(events)
            : collectGemini(events);
  }
  if (definition.optional?.(value, 200)) {
    return { skip: true, detail: 'the model or channel does not support this case' };
  }
  const okay = !value.error && value.done && definition.expect(value);
  return {
    okay,
    detail: okay
      ? summarize(value)
      : `${value.error || 'expectation failed'} | text=${value.text.slice(0, 200)} tools=${JSON.stringify(value.toolCalls)}`,
  };
}

let cookie = '';
async function login() {
  const response = await fetch(`${base}/console/api/session`, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      'X-SafeAgent-Request': '1',
      Origin: base,
    },
    body: JSON.stringify({ username: 'admin', password: adminPassword }),
  });
  if (!response.ok) throw new Error(`console login failed: HTTP ${response.status}`);
  cookie = (response.headers.get('set-cookie') ?? '').split(';')[0];
}

async function calls() {
  const response = await fetch(`${base}/console/api/projects/${project}/calls?limit=100`, {
    headers: { cookie },
  });
  if (!response.ok) return [];
  return (await response.json()).items ?? [];
}

async function redactionProved(route, before, since) {
  for (let attempt = 0; attempt < 10; attempt++) {
    const items = await calls();
    const found = items.find(
      (call) =>
        !before.has(call.id) &&
        call.public_model === routeName[route] &&
        call.redaction_hits > 0 &&
        (since === undefined || Date.parse(call.created_at) >= since - 2000),
    );
    if (found) return true;
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  return false;
}

await login();
const report = {
  base,
  project,
  routes,
  clients,
  passed: 0,
  failed: 0,
  skipped: 0,
  failures: [],
  skips: [],
  results: [],
};
for (const stream of [true, false]) {
  for (const definition of cases) {
    for (const client of clients) {
      for (const route of routes) {
        const label = `${stream ? 'stream' : 'json'} ${definition.kind} ${client}->${route}`;
        let result;
        let attempts = 0;
        let tries = 0;
        const before = definition.verifyRedaction
          ? new Set((await calls()).map((call) => call.id))
          : null;
        const since = Date.now();
        for (; attempts < definition.retry; attempts++) {
          tries++;
          try {
            result = await call(client, route, definition.kind, stream);
          } catch (error) {
            result = { okay: false, detail: String(error) };
          }
          if (result.okay || result.skip) break;
        }
        if (result.skip) {
          report.skipped++;
          report.skips.push({ label, attempts: tries, detail: result.detail });
          report.results.push({
            label,
            attempts: tries,
            state: 'skipped',
            detail: result.detail,
          });
          console.log(`SKIP ${label} ${result.detail}`);
          continue;
        }
        if (result.okay && definition.verifyRedaction) {
          const proved = await redactionProved(route, before, since);
          if (!proved) {
            result = { okay: false, detail: 'the call did not record a redaction hit' };
          }
        }
        if (result.okay) {
          report.passed++;
          report.results.push({
            label,
            attempts: tries,
            state: 'passed',
            detail: result.detail,
          });
          console.log(`PASS ${label}${tries > 1 ? ` (attempt ${tries})` : ''} ${result.detail}`);
        } else {
          report.failed++;
          report.failures.push({ label, attempts: tries, detail: result.detail });
          report.results.push({
            label,
            attempts: tries,
            state: 'failed',
            detail: result.detail,
          });
          console.log(`FAIL ${label} after ${tries} attempts ${result.detail}`);
        }
      }
    }
  }
}
const out = path.join(
  path.dirname(new URL(import.meta.url).pathname),
  '..',
  'runtime',
  'acceptance',
);
fs.mkdirSync(out, { recursive: true });
fs.writeFileSync(path.join(out, 'matrix.json'), JSON.stringify(report, null, 2));
console.log(`\n${report.passed} passed, ${report.failed} failed, ${report.skipped} skipped`);
if (report.failed) process.exit(1);
