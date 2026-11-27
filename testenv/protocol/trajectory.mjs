// Drives a session through the gateway, then reads the trajectory projection. It checks stable
// event/call identities, one trajectory per session, causal edges, coverage and audit status.
import process from 'node:process';
import {
  consoleGet,
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
const session = `trajectory-${stamp}`;

function pass(name) {
  recordPass();
  console.log(`  ok   ${name}`);
}

function fail(name, detail) {
  recordFail(name, detail);
  console.log(`  FAIL ${name}: ${detail}`);
}

function expect(condition, name, detail = '') {
  if (condition) pass(name);
  else fail(name, detail);
}

function chat(body) {
  return gatewayRequest('POST', '/v1/chat/completions', body, {
    'X-SafeAgent-Session': session,
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
    { role: 'assistant', content: firstMessage.content ?? null, tool_calls: [toolCall] },
    { role: 'tool', tool_call_id: toolCall.id, content: '22C and sunny' },
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

const ids = new Set(driven.map((entry) => entry.trajectory_id).filter(Boolean));
expect(ids.size === 1, 'session calls share one trajectory', `trajectories=${[...ids]}`);
const trajectoryID = [...ids][0];
if (!trajectoryID) throw new Error('the session has no trajectory id');

const detail = await consoleGet(`/projects/${project}/trajectories/${trajectoryID}`);
const again = await consoleGet(`/projects/${project}/trajectories/${trajectoryID}`);
expect(
  JSON.stringify(detail.events.map((event) => event.id)) ===
    JSON.stringify(again.events.map((event) => event.id)),
  'event ids are stable across reads',
);
expect(
  (detail.calls ?? []).length === 3,
  'the trajectory lists all driven calls',
  `calls=${detail.calls?.length}`,
);
expect(
  (detail.events ?? []).some((event) => event.kind === 'trajectory.start'),
  'the trajectory has a start event',
);
expect(
  (detail.events ?? []).some((event) => event.kind === 'action.executed'),
  'the trajectory has an executed action',
);
expect(
  (detail.edges ?? []).some((edge) => edge.relation === 'used_data'),
  'input segments are linked to actions',
);
expect(
  (detail.edges ?? []).some((edge) => edge.relation === 'derived_from'),
  'actions are linked to output segments',
);
expect(
  ['verified', 'in_progress'].includes(detail.integrity.status),
  'the trajectory chain verifies',
  detail.integrity.status,
);
expect(
  detail.state.coverage >= detail.state.coverage_threshold,
  'coverage meets the threshold',
  `${detail.state.coverage}/${detail.state.coverage_threshold}`,
);
expect(
  (detail.events ?? []).every((event) => event.hash && event.prev_hash !== undefined),
  'every event carries chain hashes',
);

const event = detail.events.find((entry) => entry.kind === 'action.proposed');
if (event) {
  const why = await consoleGet(`/projects/${project}/trajectories/${trajectoryID}/why/${event.id}`);
  expect(Array.isArray(why.items), 'why returns a causal edge list');
} else {
  fail('why query', 'no action.proposed event');
}

const summaries = await consoleGet(
  `/projects/${project}/trajectories?agent_key_id=${driven[0].agent_key_id}&limit=100`,
);
expect(
  (summaries.items ?? []).some((entry) => entry.id === trajectoryID),
  'the trajectory is listed by agent key',
);

summary();
