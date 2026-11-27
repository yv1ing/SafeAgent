// Exercises the endpoints an agent client uses outside generation: model listing, token counting,
// the Responses lifecycle, embeddings passthrough and the Responses WebSocket. It then checks the
// console accounting of those calls: auxiliary lane, standalone trajectory, no failure counter, and the
// completed/unsupported or completed/not_found outcomes of expected protocol refusals.
import process from 'node:process';
import {
  base,
  consoleGet,
  consoleRequest,
  counters,
  gatewayRequest,
  key,
  login,
  project,
  recordFail,
  recordPass,
  recordSkip,
  routeName,
  routes,
  summary,
} from '../lib/client.mjs';

const stamp = `${Date.now()}-${process.pid}`;

function pass(name) {
  recordPass();
  console.log(`  ok   ${name}`);
}

function skip(name, reason) {
  recordSkip();
  console.log(`  skip ${name}: ${reason}`);
}

function fail(name, detail) {
  recordFail(name, detail);
  console.log(`  FAIL ${name}: ${detail}`);
}

async function check(name, run) {
  try {
    await run();
  } catch (error) {
    fail(name, error instanceof Error ? error.message : String(error));
  }
}

const callPage = async (path) => {
  const page = await consoleGet(path);
  return page?.items ?? [];
};

const totalOf = (values) => values.reduce((sum, value) => sum + value, 0);

await login();

const beforeCalls = await callPage(`/projects/${project}/calls?limit=100`);
const beforeIds = new Set(beforeCalls.map((call) => call.id));
const beforeStats = await consoleGet(`/projects/${project}/stats?window=24h`);
const beforeMonitor = await consoleGet('/monitor?window=24h');
const projectMonitor = (snapshot) =>
  (snapshot?.projects ?? []).find((entry) => entry.project_id === project) ?? {
    calls: 0,
    failures: 0,
  };
const beforeProjectMonitor = projectMonitor(beforeMonitor);
const beforeFailures = beforeStats?.totals?.failures ?? 0;

await check('model list', async () => {
  const openai = await gatewayRequest('GET', '/v1/models');
  if (openai.status !== 200 || openai.payload?.object !== 'list') {
    throw new Error(`openai shape ${openai.status}`);
  }
  const anthropic = await gatewayRequest('GET', '/v1/models', undefined, {
    'anthropic-version': '2023-06-01',
  });
  if (anthropic.status !== 200 || !Array.isArray(anthropic.payload?.data)) {
    throw new Error(`anthropic shape ${anthropic.status}`);
  }
  const gemini = await gatewayRequest('GET', '/v1beta/models');
  if (gemini.status !== 200 || !Array.isArray(gemini.payload?.models)) {
    throw new Error(`gemini shape ${gemini.status}`);
  }
  const name = gemini.payload.models[0]?.name;
  if (!name) throw new Error('no models');
  const single = await gatewayRequest('GET', `/v1beta/models/${name.replace(/^models\//, '')}`);
  if (single.status !== 200 || single.payload?.name !== name)
    throw new Error(`single model ${single.status}`);
  pass('model list');
});

for (const format of routes) {
  const name = routeName(format);
  if (!name) continue;
  if (format === 'messages') {
    await check(`count tokens ${format}`, async () => {
      const result = await gatewayRequest('POST', '/v1/messages/count_tokens', {
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
      const result = await gatewayRequest('POST', '/v1/responses/input_tokens', {
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
      const result = await gatewayRequest('POST', `/v1beta/models/${name}:countTokens`, {
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
    const created = await gatewayRequest('POST', '/v1/responses', {
      model: name,
      input: 'remember this',
      store: true,
      max_output_tokens: 32,
    });
    if (created.status !== 200 || !created.payload?.id) {
      throw new Error(`create ${created.status} ${created.text}`);
    }
    const id = created.payload.id;
    const retrieved = await gatewayRequest('GET', `/v1/responses/${id}`);
    if (retrieved.status !== 200 || retrieved.payload?.object !== 'response') {
      throw new Error(`retrieve ${retrieved.status}`);
    }
    const items = await gatewayRequest('GET', `/v1/responses/${id}/input_items`);
    if (items.status !== 200 || !Array.isArray(items.payload?.data)) {
      throw new Error(`input items ${items.status}`);
    }
    const deleted = await gatewayRequest('DELETE', `/v1/responses/${id}`);
    if (deleted.status !== 200) throw new Error(`delete ${deleted.status}`);
    const gone = await gatewayRequest('GET', `/v1/responses/${id}`);
    if (gone.status !== 404) throw new Error(`retrieve after delete ${gone.status}`);
    const reused = await gatewayRequest('POST', '/v1/responses', {
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

await check('expected auxiliary refusals', async () => {
  const name = routeName('chat') ?? routeName(routes[0]);
  if (!name) throw new Error('no configured route');
  const missing = await gatewayRequest('GET', `/v1/responses/response-missing-${stamp}`);
  if (missing.status !== 404) throw new Error(`missing response ${missing.status}`);
  const mismatch = await gatewayRequest('POST', '/v1/embeddings', { model: name, input: 'hello' });
  if (mismatch.status !== 422) throw new Error(`kind mismatch ${mismatch.status}`);
  pass('expected auxiliary refusals');
});

await check('embeddings passthrough', async () => {
  const name = routeName('chat');
  if (!name) {
    skip('embeddings passthrough', 'no Chat route');
    return;
  }
  const result = await gatewayRequest('POST', '/v1/embeddings', { model: name, input: 'hello' });
  if (result.status === 422) {
    const reason =
      result.payload?.error?.code === 'model_kind_mismatch'
        ? 'the Chat route targets a generation model'
        : 'no channel declares embeddings';
    skip('embeddings passthrough', reason);
    return;
  }
  if (result.status !== 200 || !Array.isArray(result.payload?.data)) {
    throw new Error(`${result.status} ${result.text}`);
  }
  pass('embeddings passthrough');
});

// Let the audit writer settle the auxiliary records before the console reads them.
await new Promise((resolve) => setTimeout(resolve, 500));
const allCalls = await callPage(`/projects/${project}/calls?limit=100&include_auxiliary=true`);
const auxiliary = allCalls.filter((call) => call.lane === 'auxiliary');
const newCalls = allCalls.filter((call) => !beforeIds.has(call.id));
const newMain = newCalls.filter((call) => call.lane === 'main');
const afterCalls = await callPage(`/projects/${project}/calls?limit=100`);
const afterStats = await consoleGet(`/projects/${project}/stats?window=24h`);
const afterMonitor = projectMonitor(await consoleGet('/monitor?window=24h'));

await check('auxiliary calls are excluded from the default list', async () => {
  const listed = new Set(afterCalls.map((call) => call.id));
  const leaked = auxiliary.filter((call) => listed.has(call.id));
  if (leaked.length > 0) {
    throw new Error(`${leaked.length} auxiliary calls were listed by default`);
  }
  const newlyListed = afterCalls.filter((call) => !beforeIds.has(call.id)).map((call) => call.id);
  const listedSet = new Set(newlyListed);
  const expected = new Set(newMain.map((call) => call.id));
  const missing = [...expected].filter((id) => !listedSet.has(id));
  if (missing.length > 0)
    throw new Error(`${missing.length} main calls were missing from the list`);
  if (newlyListed.length !== expected.size) {
    throw new Error(`${newlyListed.length - expected.size} non-main calls were listed by default`);
  }
  const byLane = await callPage(`/projects/${project}/calls?limit=100&lane=auxiliary`);
  if (byLane.length === 0 || byLane.some((call) => call.lane !== 'auxiliary')) {
    throw new Error('the lane=auxiliary filter did not return only auxiliary calls');
  }
  pass('auxiliary calls are excluded from the default list and selectable by lane');
});

await check('auxiliary calls carry the derived axes', async () => {
  if (auxiliary.length === 0) throw new Error('no auxiliary call was recorded');
  const operations = new Set(auxiliary.map((call) => call.operation));
  if (!operations.has('models')) throw new Error('no models auxiliary call');
  if (!operations.has('responses_retrieve'))
    throw new Error('no responses_retrieve auxiliary call');
  if (!operations.has('embeddings')) throw new Error('no embeddings auxiliary call');
  for (const call of auxiliary) {
    if (call.operation === 'models' || call.operation === 'model') {
      if (call.purpose !== 'model_list') throw new Error(`${call.id} purpose ${call.purpose}`);
    }
    if (call.operation === 'count_tokens' && call.purpose !== 'count_tokens') {
      throw new Error(`${call.id} purpose ${call.purpose}`);
    }
    if (call.operation === 'embeddings' && call.purpose !== 'embedding') {
      throw new Error(`${call.id} purpose ${call.purpose}`);
    }
    if (call.session_id !== '') throw new Error(`${call.id} has session ${call.session_id}`);
    if (!call.trajectory_id) throw new Error(`${call.id} has no trajectory`);
    if (call.state === 'failed') throw new Error(`${call.id} is a failed call`);
    if (call.outcome === 'upstream_error' || call.outcome === 'client_error') {
      throw new Error(`${call.id} counted as a failure with outcome ${call.outcome}`);
    }
  }
  pass('auxiliary calls carry the derived axes');
});

await check('expected refusals are completed calls, not failures', async () => {
  const notFound = auxiliary.find(
    (call) => call.operation === 'responses_retrieve' && call.outcome === 'not_found',
  );
  if (!notFound) throw new Error('no responses_retrieve not_found call');
  if (notFound.state !== 'completed') throw new Error(`not_found state ${notFound.state}`);
  const unsupported = auxiliary.find(
    (call) => call.operation === 'embeddings' && call.outcome === 'unsupported',
  );
  if (!unsupported) throw new Error('no embeddings unsupported call');
  if (unsupported.state !== 'completed') throw new Error(`unsupported state ${unsupported.state}`);
  if (unsupported.error_code !== 'model_kind_mismatch') {
    throw new Error(`unsupported error code ${unsupported.error_code}`);
  }
  pass('expected refusals are completed calls, not failures');
});

await check('auxiliary calls are standalone trajectories', async () => {
  if (auxiliary.some((call) => call.session_id !== '')) {
    throw new Error('an auxiliary call joined a session');
  }
  if (!auxiliary.every((call) => Boolean(call.trajectory_id))) {
    throw new Error('an auxiliary call has no trajectory');
  }
  pass('auxiliary calls are standalone trajectories');
});

await check('stats and monitor count the main lane only', async () => {
  const failedMain = newMain.filter((call) => call.state === 'failed').length;
  const callDelta = (afterStats?.totals?.calls ?? 0) - (beforeStats?.totals?.calls ?? 0);
  if (callDelta !== newMain.length) {
    throw new Error(`stats counted ${callDelta} calls for ${newMain.length} main calls`);
  }
  const failureDelta = (afterStats?.totals?.failures ?? 0) - beforeFailures;
  if (failureDelta !== failedMain) {
    throw new Error(`stats counted ${failureDelta} failures for ${failedMain} failed main calls`);
  }
  const monitorDelta = (afterMonitor.calls ?? 0) - (beforeProjectMonitor.calls ?? 0);
  if (monitorDelta !== newMain.length) {
    throw new Error(`monitor counted ${monitorDelta} calls for ${newMain.length} main calls`);
  }
  const monitorFailureDelta = (afterMonitor.failures ?? 0) - (beforeProjectMonitor.failures ?? 0);
  if (monitorFailureDelta !== failedMain) {
    throw new Error(
      `monitor counted ${monitorFailureDelta} failures for ${failedMain} failed main calls`,
    );
  }
  pass('stats and monitor count the main lane only');
});

// A project that only made auxiliary calls must report an empty main-lane denominator, while its
// auxiliary call is still visible through the explicit filter.
const temporaryProject = `Aux Only ${stamp}`;
let temporaryID = '';
await check('an auxiliary-only project has empty denominators', async () => {
  const createdProject = await consoleRequest('POST', '/projects', {
    name: temporaryProject,
    retention_days: 1,
  });
  if (createdProject.status !== 200) {
    throw new Error(
      `project create ${createdProject.status} ${JSON.stringify(createdProject.payload)}`,
    );
  }
  temporaryID = createdProject.payload.id;
  const routesPage = await consoleGet(`/projects/${project}/routes`);
  const targetRoute = (routesPage ?? [])[0];
  const target = targetRoute?.targets?.[0];
  if (!target) throw new Error('the lab project has no route target to reuse');
  const createdRoute = await consoleRequest('POST', `/projects/${temporaryID}/routes`, {
    name: targetRoute.name,
    targets: [{ channel_id: target.channel_id, model_id: target.model_id }],
  });
  if (createdRoute.status !== 200) {
    throw new Error(`route create ${createdRoute.status} ${JSON.stringify(createdRoute.payload)}`);
  }
  const createdKey = await consoleRequest('POST', `/projects/${temporaryID}/keys`, {
    label: 'aux-only',
    role: 'agent',
    enabled: true,
  });
  if (createdKey.status !== 200) throw new Error(`key create ${createdKey.status}`);
  const temporaryKey = createdKey.payload.secret;
  const listed = await fetch(`${base}/v1/models`, {
    headers: { Authorization: `Bearer ${temporaryKey}` },
  });
  if (!listed.ok) throw new Error(`temporary model list ${listed.status}`);
  await new Promise((resolve) => setTimeout(resolve, 500));

  const stats = await consoleGet(`/projects/${temporaryID}/stats?window=24h`);
  if ((stats?.totals?.calls ?? -1) !== 0) throw new Error(`stats calls ${stats?.totals?.calls}`);
  if ((stats?.totals?.failures ?? -1) !== 0)
    throw new Error(`stats failures ${stats?.totals?.failures}`);
  const series = await consoleGet(`/projects/${temporaryID}/series?window=24h&bucket=1h`);
  const requests = totalOf((series?.buckets ?? []).map((bucket) => bucket.requests));
  if (requests !== 0) throw new Error(`series counted ${requests} requests`);
  const monitorNow = await consoleGet('/monitor?window=24h');
  const row = (monitorNow?.projects ?? []).find((entry) => entry.project_id === temporaryID);
  if ((row?.calls ?? 0) !== 0) throw new Error(`monitor calls ${row?.calls}`);
  const trajectories = await consoleGet(`/projects/${temporaryID}/trajectories?limit=10`);
  if ((trajectories?.items ?? []).length !== 1 || trajectories.items[0].session_id !== '') {
    throw new Error('the auxiliary call did not create exactly one standalone trajectory');
  }
  const visible = await consoleGet(
    `/projects/${temporaryID}/calls?limit=10&include_auxiliary=true`,
  );
  const entries = visible?.items ?? [];
  if (
    entries.length !== 1 ||
    entries[0].operation !== 'models' ||
    entries[0].lane !== 'auxiliary'
  ) {
    throw new Error(
      `auxiliary call list ${JSON.stringify(entries.map((entry) => entry.operation))}`,
    );
  }
  const hidden = await consoleGet(`/projects/${temporaryID}/calls?limit=10`);
  if ((hidden?.items ?? []).length !== 0)
    throw new Error('the auxiliary call was listed by default');
  pass('an auxiliary-only project has empty denominators');
});

if (temporaryID) {
  await consoleRequest('DELETE', `/projects/${temporaryID}`);
}
summary();
if (counters.failed > 0) {
  for (const failure of counters.failures) console.error(failure);
  process.exit(1);
}
