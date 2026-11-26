// Exercises every available client format against every available channel format.
// A failure is a protocol error, a missing terminal event, or content that did not survive.
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import {
  base,
  clientSpecs,
  consoleGet,
  counters,
  listCalls,
  login,
  parseResponse,
  pollNewCall,
  project,
  recordFail,
  recordPass,
  recordSkip,
  routeNames,
  routes,
  send,
  summary,
} from '../lib/client.mjs';

const clients = ['chat', 'responses', 'messages', 'gemini'];
if (routes.length === 0) {
  console.error('SAFEAGENT_AVAILABLE_FORMATS is empty');
  process.exit(2);
}

const chatBody = clientSpecs.chat.body;
const responsesBody = clientSpecs.responses.body;
const messagesBody = clientSpecs.messages.body;
const geminiBody = clientSpecs.gemini.body;
const enabled = (name, fallback) => (process.env[name] ?? fallback) !== '0';

const cases = [
  { kind: 'text', retry: 2 },
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
cases.push({ kind: 'reject', retry: 1 });

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
      optional: (value) => (value.reasoning ?? []).length === 0,
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
          (value.toolCalls.length === 0 &&
            value.text.trim() === '' &&
            (value.reasoning ?? []).length > 0),
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
        (value.toolCalls.length === 0 &&
          value.text.trim() === '' &&
          (value.reasoning ?? []).length > 0),
    };
  }
  if (kind === 'reject') {
    if (client === 'chat') {
      if (route === 'chat' && stream) {
        return {
          body: chatBody(route, stream, 'hello', { n: 2 }),
          reject: { status: 422, code: 'unsupported_multi_choice' },
          rejectOptional: true,
          expect: () => true,
        };
      }
      return {
        body: chatBody(route, stream, 'hello', { n: 2 }),
        reject: { status: 422, code: 'unsupported_multi_choice' },
      };
    }
    if (client === 'gemini') {
      if (route === 'gemini' && stream) {
        return {
          body: geminiBody(route, stream, 'hello', {
            generationConfig: { maxOutputTokens: 16, candidateCount: 2 },
          }),
          reject: { status: 422, message: 'Multiple candidates' },
          rejectOptional: true,
          expect: () => true,
        };
      }
      return {
        body: geminiBody(route, stream, 'hello', {
          generationConfig: { maxOutputTokens: 16, candidateCount: 2 },
        }),
        reject: { status: 422, message: 'Multiple candidates' },
      };
    }
    if (client === 'responses') {
      return {
        body: responsesBody(route, stream, 'hello', { background: true }),
        reject: { status: 422, code: 'background_not_supported' },
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

function summarize(value) {
  const usage = value.usage
    ? ` usage=${value.usage.input_tokens ?? value.usage.prompt_tokens ?? value.usage.promptTokenCount ?? '?'}/${value.usage.output_tokens ?? value.usage.completion_tokens ?? value.usage.candidatesTokenCount ?? '?'}`
    : '';
  return `text=${JSON.stringify(value.text.slice(0, 60))} tools=${value.toolCalls.length} finish=${value.finish}${value.reasoning ? ' reasoning=yes' : ''}${usage}`;
}

async function call(client, route, kind, stream) {
  const definition = requestFor(client, route, kind, stream);
  if (!definition) return { skip: true, detail: 'client does not support this case' };
  if (definition.skip) return { skip: true, detail: 'case is not defined for this client' };
  const response = await send(client, route, definition.body, stream);
  const raw = response.text;
  const ok = response.status >= 200 && response.status < 300;
  if (definition.reject) {
    if (!ok) {
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
    if (!definition.rejectOptional) {
      return {
        okay: false,
        detail: `expected rejection, got HTTP ${response.status}: ${raw.slice(0, 300)}`,
      };
    }
  }
  if (!ok) {
    const optional = definition.optional?.({ error: raw }, response.status);
    return {
      okay: false,
      skip: Boolean(optional),
      detail: `HTTP ${response.status}: ${raw.slice(0, 500)}`,
    };
  }
  const value = parseResponse(client, raw, stream);
  if (!value) {
    return { okay: false, detail: `non-JSON response: ${raw.slice(0, 300)}` };
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

async function redactionProved(route, before, since) {
  for (let attempt = 0; attempt < 10; attempt++) {
    const items = await listCalls();
    const found = items.find(
      (call) =>
        !before.has(call.id) &&
        call.public_model === routeNames[route] &&
        call.redaction_values > 0 &&
        (since === undefined || Date.parse(call.created_at) >= since - 2000),
    );
    if (found) return true;
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  return false;
}

// Real traffic must carry the classification axes the console filters on: a main call is a main
// generation chat call that the worker analyzes, and a declared subagent call is a side call that
// joins the same conversation without being analyzed.
async function classifyTraffic() {
  const route = routes.includes('chat') ? 'chat' : routes[0];
  if (!route || !routeNames[route]) return { skip: 'no configured route' };
  const token = `matrix-classification-${Date.now()}`;
  const sessionHeader = { 'X-SafeAgent-Session': token };
  const before = new Set((await listCalls()).map((call) => call.id));
  const mainBody = chatBody(route, false, 'Reply with exactly: PONG', { max_tokens: 16 });

  const mainResponse = await send('chat', route, mainBody, false, sessionHeader);
  if (mainResponse.status !== 200) {
    return { fail: `main call returned HTTP ${mainResponse.status}` };
  }
  const mainCall = await pollNewCall(before, (call) => call.public_model === routeNames[route]);
  if (!mainCall) return { fail: 'the main call was not recorded' };

  const sideBefore = new Set((await listCalls()).map((call) => call.id));
  const sideResponse = await send('chat', route, mainBody, false, {
    ...sessionHeader,
    'X-Openai-Subagent': '1',
  });
  if (sideResponse.status !== 200) {
    return { fail: `side call returned HTTP ${sideResponse.status}` };
  }
  const sideCall = await pollNewCall(sideBefore, (call) => call.lane === 'side');
  if (!sideCall) return { fail: 'the side call was not recorded' };

  const problems = [];
  const expect = (condition, message) => {
    if (!condition) problems.push(message);
  };
  expect(mainCall.lane === 'main', `main lane is ${mainCall.lane}`);
  expect(mainCall.operation === 'generation', `main operation is ${mainCall.operation}`);
  expect(mainCall.purpose === 'chat', `main purpose is ${mainCall.purpose}`);
  expect(
    mainCall.purpose_source === 'default',
    `main purpose source is ${mainCall.purpose_source}`,
  );
  expect(mainCall.outcome === 'ok', `main outcome is ${mainCall.outcome}`);
  expect(mainCall.state === 'completed', `main state is ${mainCall.state}`);
  expect(mainCall.resolution === 'exact', `main resolution is ${mainCall.resolution}`);
  expect(Boolean(mainCall.session_id), 'the main call has no session');
  expect(sideCall.lane === 'side', `side lane is ${sideCall.lane}`);
  expect(sideCall.operation === 'generation', `side operation is ${sideCall.operation}`);
  expect(sideCall.purpose === 'subagent', `side purpose is ${sideCall.purpose}`);
  expect(
    sideCall.purpose_source === 'declared',
    `side purpose source is ${sideCall.purpose_source}`,
  );
  expect(
    sideCall.session_id === mainCall.session_id,
    'the side call did not join the conversation',
  );
  expect(
    sideCall.analysis_state === 'skipped',
    `side analysis state is ${sideCall.analysis_state}`,
  );

  const conversations = await consoleGet(`/projects/${project}/conversations?limit=100`);
  const conversation = (conversations?.items ?? []).find(
    (entry) => entry.id === mainCall.session_id,
  );
  expect(Boolean(conversation), 'the conversation was not listed');
  expect((conversation?.calls ?? 0) >= 2, `conversation call count is ${conversation?.calls}`);
  expect(
    ['pending', 'running', 'completed'].includes(mainCall.analysis_state),
    `main analysis state is ${mainCall.analysis_state}`,
  );

  if (problems.length > 0) return { fail: problems.join('; ') };
  return {
    detail: `main=${mainCall.id} side=${sideCall.id} session=${mainCall.session_id}`,
  };
}

await login();
const results = [];
for (const stream of [true, false]) {
  for (const definition of cases) {
    for (const client of clients) {
      for (const route of routes) {
        const label = `${stream ? 'stream' : 'json'} ${definition.kind} ${client}->${route}`;
        let result;
        let attempts = 0;
        let tries = 0;
        const before = definition.verifyRedaction
          ? new Set((await listCalls()).map((call) => call.id))
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
          recordSkip();
          results.push({
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
          recordPass();
          results.push({
            label,
            attempts: tries,
            state: 'passed',
            detail: result.detail,
          });
          console.log(`PASS ${label}${tries > 1 ? ` (attempt ${tries})` : ''} ${result.detail}`);
        } else {
          recordFail(label, result.detail);
          results.push({
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

const classification = await classifyTraffic();
if (classification.skip) {
  recordSkip();
  results.push({
    label: 'call classification',
    state: 'skipped',
    detail: classification.skip,
  });
  console.log(`SKIP call classification ${classification.skip}`);
} else if (classification.fail) {
  recordFail('call classification', classification.fail);
  results.push({
    label: 'call classification',
    state: 'failed',
    detail: classification.fail,
  });
  console.log(`FAIL call classification ${classification.fail}`);
} else {
  recordPass();
  results.push({
    label: 'call classification',
    state: 'passed',
    detail: classification.detail,
  });
  console.log(`PASS call classification ${classification.detail}`);
}

const report = {
  base,
  project,
  routes,
  clients,
  passed: counters.passed,
  failed: counters.failed,
  skipped: counters.skipped,
  failures: results
    .filter((entry) => entry.state === 'failed')
    .map(({ label, attempts, detail }) => ({ label, attempts, detail })),
  skips: results
    .filter((entry) => entry.state === 'skipped')
    .map(({ label, attempts, detail }) => ({ label, attempts, detail })),
  results,
};
const out = path.join(path.dirname(new URL(import.meta.url).pathname), '..', '..', 'build', 'lab');
fs.mkdirSync(out, { recursive: true });
fs.writeFileSync(path.join(out, 'matrix.json'), JSON.stringify(report, null, 2));
summary();
if (counters.failed) process.exit(1);
