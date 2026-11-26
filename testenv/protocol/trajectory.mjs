// Drives a multi-turn tool-loop conversation through the gateway, then reads the session trajectory
// from the console. It checks that the graph, the flat node list and the replay share stable ids, that
// every tool call is paired with its result, that the replay steps are ordered, and that auxiliary
// protocol calls never appear inside a conversation.
import process from 'node:process';
import {
  consoleRequest,
  counters,
  gatewayRequest,
  login,
  pollNewCall,
  project,
  recordFail,
  recordPass,
  routeName,
  routes,
  summary,
} from '../lib/client.mjs';

if (routes.length === 0) {
  console.error('SAFEAGENT_AVAILABLE_FORMATS must not be empty');
  process.exit(2);
}
const route = routes.includes('chat') ? 'chat' : routes[0];
const model = routeName(route);
if (!model) {
  console.error(`SAFEAGENT_${route.toUpperCase()}_ROUTE is required`);
  process.exit(2);
}

const stamp = `${Date.now()}-${process.pid}`;
const sessionHeader = `trajectory-${stamp}`;

function pass(name) {
  recordPass();
  console.log(`  ok   ${name}`);
}

function fail(name, detail) {
  recordFail(name, detail);
  console.log(`  FAIL ${name}: ${detail}`);
}

async function consoleGet(path) {
  const response = await consoleRequest('GET', path);
  if (response.status !== 200) throw new Error(`console GET ${path} returned ${response.status}`);
  return response.payload;
}

function chat(body) {
  return gatewayRequest('POST', '/v1/chat/completions', body, {
    'X-SafeAgent-Session': sessionHeader,
  });
}

async function newCall(known, attempts = 40) {
  return pollNewCall(known, () => true, attempts);
}

const tools = [
  {
    type: 'function',
    function: {
      name: 'get_weather',
      description: 'Get the current weather for a city',
      parameters: { type: 'object', properties: { city: { type: 'string' } }, required: ['city'] },
    },
  },
];

await login();
const initial = await consoleGet(`/projects/${project}/calls?limit=100`);
const known = new Set((initial.items ?? []).map((call) => call.id));
const driven = [];

const first = await chat({
  model,
  stream: false,
  max_tokens: 200,
  tools,
  messages: [{ role: 'user', content: 'Call get_weather for Paris. Do not answer with text.' }],
});
if (first.status !== 200)
  throw new Error(`first call returned ${first.status}: ${first.text.slice(0, 300)}`);
const firstMessage = first.payload?.choices?.[0]?.message ?? {};
const toolCall = (firstMessage.tool_calls ?? [])[0];
if (!toolCall?.id || toolCall.function?.name !== 'get_weather') {
  throw new Error(`the model did not call the tool: ${first.text.slice(0, 300)}`);
}
let call = await newCall(known);
if (!call) throw new Error('the first call was not recorded');
known.add(call.id);
driven.push(call);

const second = await chat({
  model,
  stream: false,
  max_tokens: 200,
  tools,
  messages: [
    { role: 'user', content: 'Call get_weather for Paris. Do not answer with text.' },
    {
      role: 'assistant',
      content: firstMessage.content ?? null,
      tool_calls: [toolCall],
    },
    {
      role: 'tool',
      tool_call_id: toolCall.id,
      content: '22C and sunny',
    },
  ],
});
if (second.status !== 200)
  throw new Error(`second call returned ${second.status}: ${second.text.slice(0, 300)}`);
call = await newCall(known);
if (!call) throw new Error('the second call was not recorded');
known.add(call.id);
driven.push(call);
const secondText = second.payload?.choices?.[0]?.message?.content ?? '';

const third = await chat({
  model,
  stream: false,
  max_tokens: 200,
  tools,
  messages: [
    { role: 'user', content: 'Call get_weather for Paris. Do not answer with text.' },
    { role: 'assistant', content: firstMessage.content ?? null, tool_calls: [toolCall] },
    { role: 'tool', tool_call_id: toolCall.id, content: '22C and sunny' },
    { role: 'assistant', content: secondText },
    { role: 'user', content: 'Reply with exactly: DONE' },
  ],
});
if (third.status !== 200)
  throw new Error(`third call returned ${third.status}: ${third.text.slice(0, 300)}`);
call = await newCall(known);
if (!call) throw new Error('the third call was not recorded');
known.add(call.id);
driven.push(call);

const sessions = new Set(driven.map((entry) => entry.session_id).filter(Boolean));
if (sessions.size !== 1) throw new Error(`calls joined ${sessions.size} conversations`);
const sessionID = [...sessions][0];
if (driven.some((entry) => entry.lane !== 'main' || entry.operation !== 'generation')) {
  throw new Error('a driven call is not a main generation call');
}

// Wait for the asynchronous analysis of the driven calls before reading the graph twice: a finding
// that lands between two reads would change the stable node set for a reason unrelated to identity.
const settled = new Set();
const deadline = Date.now() + 300_000;
while (settled.size < driven.length && Date.now() < deadline) {
  const page = await consoleGet(`/projects/${project}/calls?limit=100`);
  for (const entry of page.items ?? []) {
    if (
      driven.some((call) => call.id === entry.id) &&
      (entry.analysis_state === 'completed' || entry.analysis_state === 'failed')
    ) {
      settled.add(entry.id);
    }
  }
  if (settled.size < driven.length) await new Promise((resolve) => setTimeout(resolve, 1000));
}

const trajectoryPath = `/projects/${project}/conversations/${sessionID}/trajectory`;
const trajectory = await consoleGet(trajectoryPath);
const again = await consoleGet(trajectoryPath);

const nodes = trajectory.nodes ?? [];
const edges = trajectory.edges ?? [];
const steps = trajectory.steps ?? [];
const ids = new Set(nodes.map((node) => node.id));
const callNodes = nodes.filter((node) => node.kind === 'call');
const turnNodes = nodes.filter((node) => node.kind === 'turn');
const itemNodes = nodes.filter((node) => node.kind === 'request' || node.kind === 'response');
const toolNodes = nodes.filter((node) => node.kind === 'tool_call');
const resultNodes = nodes.filter((node) => node.kind === 'tool_result');

const patterns = {
  session: /^session$/,
  call: /^call:[^:]+$/,
  turn: /^turn:[^:]+:\d+$/,
  request: /^item:[^:]+:(request|instructions|params|tools):\d+$/,
  response: /^item:[^:]+:response:\d+$/,
  tool_call: /^tool:[^:]+:.+$/,
  tool_result: /^tool-result:[^:]+:.+$/,
  attempt: /^attempt:[^:]+:\d+$/,
  decision: /^decision:.+$/,
  finding: /^finding:.+$/,
  redaction: /^redaction:.+$/,
};

if (nodes.length > 0 && nodes.every((node) => patterns[node.kind]?.test(node.id))) {
  pass('every node id encodes its kind and owner');
} else {
  fail('node ids', nodes.map((node) => `${node.kind}:${node.id}`).join(', '));
}

const idsAgain = (again.nodes ?? []).map((node) => node.id).join('|');
const edgesAgain = (again.edges ?? []).map((edge) => edge.id).join('|');
if (
  idsAgain === nodes.map((node) => node.id).join('|') &&
  edgesAgain === edges.map((edge) => edge.id).join('|')
) {
  pass('node and edge ids are stable across reads');
} else {
  fail('node ids changed between two reads of the same session');
}

if (
  callNodes.length === driven.length &&
  callNodes.every((node) => node.call?.operation === 'generation')
) {
  pass('the trajectory carries one call node per call');
} else {
  fail('call nodes', `${callNodes.length} nodes for ${driven.length} calls`);
}

// A conversation may end right after the model asked for a tool, so a tool call without a result
// is a legitimate trailing call. Every recorded result must point at the call it answers, though.
if (
  resultNodes.length >= 1 &&
  resultNodes.every((node) => {
    const pair = toolNodes.find(
      (call) => call.tool_call?.tool_call_id === node.tool_result?.tool_call_id,
    );
    if (!pair) return false;
    return edges.some(
      (edge) => edge.kind === 'consumes' && edge.from === node.id && edge.to === pair.id,
    );
  })
) {
  pass('every tool result is paired with its call by a consumes edge');
} else {
  fail('tool pairs', `${toolNodes.length} tool calls, ${resultNodes.length} results`);
}

if (turnNodes.length >= 2 && itemNodes.length >= 3) {
  pass('the graph carries the turns and their items');
} else {
  fail('turn nodes', `${turnNodes.length} turns, ${itemNodes.length} items`);
}

if (callNodes.every((node) => node.call?.lane !== 'auxiliary')) {
  pass('no auxiliary call appears inside the trajectory');
} else {
  fail('an auxiliary call was projected into the conversation');
}

const stats = trajectory.stats ?? {};
if (
  stats.calls === callNodes.length &&
  stats.turns === turnNodes.length &&
  stats.items === itemNodes.length &&
  stats.tool_calls === toolNodes.length
) {
  pass('the trajectory stats agree with the node list');
} else {
  fail('trajectory stats', JSON.stringify(stats));
}

// Audit sequences restart with every call, so the replay is ordered by call and, inside one call,
// by event sequence. Wall-clock time still has to be non-decreasing across the whole conversation.
let ordered = steps.length > 0;
let previousSequence = null;
let previousCall = null;
let previousAt = null;
for (const step of steps) {
  if (previousCall === step.call_id) {
    if (previousSequence !== null && step.sequence <= previousSequence) ordered = false;
  }
  if (previousAt !== null && Date.parse(step.at) < previousAt) ordered = false;
  if (!Array.isArray(step.node_ids) || step.node_ids.length === 0) ordered = false;
  if ((step.node_ids ?? []).some((id) => !ids.has(id))) ordered = false;
  previousSequence = step.sequence;
  previousCall = step.call_id;
  previousAt = Date.parse(step.at);
}
if (ordered) {
  pass('replay steps are ordered and reference existing nodes');
} else {
  fail('replay step order', `${steps.length} steps`);
}

const conversations = await consoleGet(`/projects/${project}/conversations?limit=100`);
const conversation = (conversations.items ?? []).find((entry) => entry.id === sessionID);
if (conversation && conversation.calls === callNodes.length) {
  pass('the conversation row counts the same calls as the trajectory');
} else {
  fail('conversation row', JSON.stringify(conversation));
}

summary();
if (counters.failed > 0) {
  for (const failure of counters.failures) console.error(failure);
  process.exit(1);
}
