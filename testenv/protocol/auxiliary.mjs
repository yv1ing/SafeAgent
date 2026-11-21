// Exercises the endpoints an agent client uses outside generation: model listing, token counting,
// the Responses lifecycle, embeddings passthrough and the Responses WebSocket.
import process from 'node:process';

const base = process.env.SAFEAGENT_BASE_URL ?? 'http://127.0.0.1:8080';
const key = process.env.SAFEAGENT_MATRIX_KEY;
const routes = (process.env.SAFEAGENT_AVAILABLE_FORMATS ?? '').split(/\s+/).filter(Boolean);
if (!key) {
  console.error('SAFEAGENT_MATRIX_KEY is required');
  process.exit(2);
}

let passed = 0;
let skipped = 0;
const failures = [];
const routeName = (format) => process.env[`SAFEAGENT_${format.toUpperCase()}_ROUTE`];

async function request(method, path, body, headers = {}) {
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

function pass(name) {
  passed += 1;
  console.log(`  ok   ${name}`);
}

function skip(name, reason) {
  skipped += 1;
  console.log(`  skip ${name}: ${reason}`);
}

function fail(name, detail) {
  failures.push(`${name}: ${detail}`);
  console.log(`  FAIL ${name}: ${detail}`);
}

async function check(name, run) {
  try {
    await run();
  } catch (error) {
    fail(name, error instanceof Error ? error.message : String(error));
  }
}

await check('model list', async () => {
  const openai = await request('GET', '/v1/models');
  if (openai.status !== 200 || openai.payload?.object !== 'list') {
    throw new Error(`openai shape ${openai.status}`);
  }
  const anthropic = await request('GET', '/v1/models', undefined, {
    'anthropic-version': '2023-06-01',
  });
  if (anthropic.status !== 200 || !Array.isArray(anthropic.payload?.data)) {
    throw new Error(`anthropic shape ${anthropic.status}`);
  }
  const gemini = await request('GET', '/v1beta/models');
  if (gemini.status !== 200 || !Array.isArray(gemini.payload?.models)) {
    throw new Error(`gemini shape ${gemini.status}`);
  }
  const name = gemini.payload.models[0]?.name;
  if (!name) throw new Error('no models');
  const single = await request('GET', `/v1/models/${name.replace('models/', '')}`);
  if (single.status !== 200) throw new Error(`single model ${single.status}`);
  pass('model list');
});

for (const format of routes) {
  const name = routeName(format);
  if (!name) continue;
  if (format === 'messages') {
    await check(`count tokens ${format}`, async () => {
      const result = await request('POST', '/v1/messages/count_tokens', {
        model: name,
        messages: [{ role: 'user', content: 'hello' }],
      });
      if (result.status !== 200 || typeof result.payload?.input_tokens !== 'number') {
        throw new Error(`${result.status} ${result.text}`);
      }
      pass(`count tokens ${format}`);
    });
  }
  if (format === 'responses') {
    await check(`count tokens ${format}`, async () => {
      const result = await request('POST', '/v1/responses/input_tokens', {
        model: name,
        input: 'hello',
      });
      if (result.status !== 200 || typeof result.payload?.input_tokens !== 'number') {
        throw new Error(`${result.status} ${result.text}`);
      }
      pass(`count tokens ${format}`);
    });
  }
  if (format === 'gemini') {
    await check(`count tokens ${format}`, async () => {
      const result = await request('POST', `/v1beta/models/${name}:countTokens`, {
        contents: [{ role: 'user', parts: [{ text: 'hello' }] }],
      });
      if (result.status !== 200 || typeof result.payload?.totalTokens !== 'number') {
        throw new Error(`${result.status} ${result.text}`);
      }
      pass(`count tokens ${format}`);
    });
  }
}

if (routeName('responses')) {
  await check('responses lifecycle', async () => {
    const name = routeName('responses');
    const created = await request('POST', '/v1/responses', {
      model: name,
      input: 'remember this',
      store: true,
      max_output_tokens: 32,
    });
    if (created.status !== 200 || !created.payload?.id) {
      throw new Error(`create ${created.status} ${created.text}`);
    }
    const id = created.payload.id;
    const retrieved = await request('GET', `/v1/responses/${id}`);
    if (retrieved.status !== 200 || retrieved.payload?.object !== 'response') {
      throw new Error(`retrieve ${retrieved.status}`);
    }
    const items = await request('GET', `/v1/responses/${id}/input_items`);
    if (items.status !== 200 || !Array.isArray(items.payload?.data)) {
      throw new Error(`input items ${items.status}`);
    }
    const deleted = await request('DELETE', `/v1/responses/${id}`);
    if (deleted.status !== 200) throw new Error(`delete ${deleted.status}`);
    const gone = await request('GET', `/v1/responses/${id}`);
    if (gone.status !== 404) throw new Error(`retrieve after delete ${gone.status}`);
    const reused = await request('POST', '/v1/responses', {
      model: name,
      input: 'continue',
      previous_response_id: id,
    });
    if (reused.status !== 404)
      throw new Error(`previous_response_id after delete ${reused.status}`);
    pass('responses lifecycle');
  });

  await check('responses websocket', async () => {
    if (typeof WebSocket !== 'function') {
      skip('responses websocket', 'the Node runtime has no WebSocket');
      return;
    }
    const name = routeName('responses');
    const url = `${base.replace(/^http/, 'ws')}/v1/responses?key=${encodeURIComponent(key)}`;
    const socket = new WebSocket(url);
    const completed = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('websocket timed out')), 30000);
      socket.addEventListener('open', () => {
        socket.send(JSON.stringify({ type: 'response.create', model: name, input: 'socket' }));
      });
      socket.addEventListener('message', (event) => {
        if (String(event.data).includes('response.completed')) {
          clearTimeout(timer);
          resolve(true);
        }
      });
      socket.addEventListener('error', () => {
        clearTimeout(timer);
        reject(new Error('websocket error'));
      });
    });
    socket.close();
    if (!completed) throw new Error('no response.completed');
    pass('responses websocket');
  });
}

await check('embeddings passthrough', async () => {
  const name = routeName('chat');
  if (!name) {
    skip('embeddings passthrough', 'no Chat route');
    return;
  }
  const result = await request('POST', '/v1/embeddings', { model: name, input: 'hello' });
  if (result.status === 422) {
    skip('embeddings passthrough', 'no channel declares embeddings');
    return;
  }
  if (result.status !== 200 || !Array.isArray(result.payload?.data)) {
    throw new Error(`${result.status} ${result.text}`);
  }
  pass('embeddings passthrough');
});

console.log(`\n${passed} passed, ${skipped} skipped, ${failures.length} failed`);
if (failures.length > 0) {
  for (const failure of failures) console.error(failure);
  process.exit(1);
}
