// Browser acceptance for the operator console: every page renders without a client error or a
// failed API call, and the primary CRUD paths persist through the API.
import { chromium } from 'playwright';
import fs from 'node:fs';

const base = process.env.SAFEAGENT_BASE_URL ?? 'http://host.docker.internal:8080';
const consoleBase = `${base}/console`;
const password = process.env.SAFEAGENT_ADMIN_PASSWORD;
const project = process.env.SAFEAGENT_PROJECT;
const shots = '/screenshots';
if (!password || !project) {
  console.error('SAFEAGENT_ADMIN_PASSWORD and SAFEAGENT_PROJECT are required');
  process.exit(2);
}
fs.mkdirSync(shots, { recursive: true });

const stamp = Date.now();
const failures = [];
const browser = await chromium.launch({ args: ['--no-sandbox'] });
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
  colorScheme: 'light',
  locale: 'en-US',
});
const page = await context.newPage();
let recording = false;
page.on('pageerror', (error) => failures.push(`pageerror: ${error.message}`));
page.on('console', (message) => {
  if (recording && message.type() === 'error') failures.push(`console: ${message.text()}`);
});
page.on('response', (response) => {
  if (!recording) return;
  const url = response.url();
  if (response.status() >= 400 && !url.includes('/favicon')) {
    failures.push(`http ${response.status()}: ${url}`);
  }
});

async function shot(name, fullPage = true) {
  await page.screenshot({ path: `${shots}/${name}.png`, fullPage });
}

async function waitForPage() {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(700);
  const errorBoundary = await page.getByText(/something went wrong|出了点问题|加载失败/i).count();
  if (errorBoundary) failures.push(`error boundary on ${page.url()}`);
}

async function api(path, method = 'GET', data) {
  const response = await page.request.fetch(`${consoleBase}/api${path}`, {
    method,
    headers: { 'X-SafeAgent-Request': '1', Origin: base, 'Content-Type': 'application/json' },
    data: data === undefined ? undefined : JSON.stringify(data),
  });
  if (!response.ok()) throw new Error(`${method} ${path} -> HTTP ${response.status()}`);
  return response.status() === 204 ? null : response.json();
}

async function step(name, run) {
  try {
    await run();
  } catch (error) {
    failures.push(`${name}: ${error.message}`);
  }
}

function requireValue(condition, message) {
  if (!condition) throw new Error(message);
}

async function pick(scope, label, option) {
  await scope.getByLabel(label).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}

async function dialog() {
  const value = page.getByRole('dialog');
  await value.waitFor({ timeout: 10_000 });
  return value;
}

async function confirm(label) {
  const value = await dialog();
  await value.getByRole('button', { name: label, exact: true }).click();
  await value.waitFor({ state: 'hidden', timeout: 10_000 }).catch(() => {});
}

recording = true;
await page.goto(`${consoleBase}/login`, { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(400);
await shot('01-login');
await page.locator('input[autocomplete="username"]').fill('admin');
await page.locator('input[autocomplete="current-password"]').fill(password);
await page.locator('button[type="submit"]').click();
await page.waitForURL((url) => !url.pathname.endsWith('/login'), { timeout: 30_000 });
await waitForPage();
await shot('02-monitor');

const projectName = `UI Acceptance ${stamp}`;
let projectId = '';
let userId = '';
let channelId = '';

await step('project create', async () => {
  await page.goto(`${consoleBase}/projects`, { waitUntil: 'domcontentloaded' });
  await waitForPage();
  await page.getByRole('button', { name: 'New project' }).click();
  const form = await dialog();
  await form.getByLabel('Name').fill(projectName);
  await form.getByRole('button', { name: 'Create', exact: true }).click();
  await page.getByText(projectName, { exact: true }).waitFor({ timeout: 10_000 });
  const projects = await api('/projects');
  const created = projects.find((entry) => entry.name === projectName);
  requireValue(created, 'the project was not listed by the API');
  projectId = created.id;
});

if (projectId) {
  const renamed = `${projectName} Renamed`;
  await step('project settings', async () => {
    await page.goto(`${consoleBase}/projects/${projectId}/settings`, {
      waitUntil: 'domcontentloaded',
    });
    await waitForPage();
    await page.getByLabel('Name').fill(renamed);
    await page.getByLabel('Default output limit').fill('1234');
    await page.getByRole('button', { name: 'Save', exact: true }).first().click();
    await page.waitForTimeout(700);
    const loaded = await api(`/projects/${projectId}`);
    requireValue(loaded.name === renamed, 'the project name was not saved');
    requireValue(loaded.default_max_output_tokens === 1234, 'the project limit was not saved');
  });

  const channelOptions = await api(`/projects/${projectId}/channel-options`).catch(() => []);
  if (channelOptions.length > 0) {
    const routeName = `ui-route-${stamp}`;
    await step('route crud', async () => {
      await page.goto(`${consoleBase}/projects/${projectId}/routes`, {
        waitUntil: 'domcontentloaded',
      });
      await waitForPage();
      await page.getByRole('button', { name: 'New route' }).click();
      const form = await dialog();
      await form.getByLabel('Name').fill(routeName);
      await form.getByRole('button', { name: 'Add target' }).click();
      await form.getByRole('button', { name: 'Create', exact: true }).click();
      await form.waitFor({ state: 'hidden', timeout: 10_000 });
      let routes = await api(`/projects/${projectId}/routes`);
      const created = routes.find((entry) => entry.name === routeName);
      requireValue(created, 'the route was not created');
      const row = page.locator('tr').filter({ hasText: routeName });
      await row.waitFor({ timeout: 10_000 });
      await row.getByRole('button', { name: 'Edit' }).click();
      const edit = await dialog();
      await edit.getByLabel('Weight').first().fill('2');
      await edit.getByRole('button', { name: 'Save', exact: true }).click();
      await edit.waitFor({ state: 'hidden', timeout: 10_000 });
      routes = await api(`/projects/${projectId}/routes`);
      requireValue(
        routes.find((entry) => entry.name === routeName)?.targets?.[0]?.weight === 2,
        'the route weight was not saved',
      );
      await row.getByRole('button', { name: 'Delete' }).click();
      await confirm('Delete');
      routes = await api(`/projects/${projectId}/routes`);
      requireValue(!routes.some((entry) => entry.name === routeName), 'the route was not deleted');
    });
  }

  await step('key crud', async () => {
    const label = `ui-key-${stamp}`;
    await page.goto(`${consoleBase}/projects/${projectId}/keys`, {
      waitUntil: 'domcontentloaded',
    });
    await waitForPage();
    await page.getByRole('button', { name: 'New key' }).click();
    const form = await dialog();
    await form.getByLabel('Label').fill(label);
    await form.getByRole('button', { name: 'Create', exact: true }).click();
    const secret = await dialog();
    await secret.getByRole('button', { name: 'Secret stored' }).click();
    let keys = await api(`/projects/${projectId}/keys`);
    const created = keys.find((entry) => entry.label === label);
    requireValue(created, 'the agent key was not created');
    const row = page.locator('tr').filter({ hasText: label });
    await row.waitFor({ timeout: 10_000 });
    await row.getByRole('button', { name: 'Edit' }).click();
    const edit = await dialog();
    await edit.getByLabel('Label').fill(`${label}-edited`);
    await edit.getByRole('button', { name: 'Save', exact: true }).click();
    await edit.waitFor({ state: 'hidden', timeout: 10_000 });
    keys = await api(`/projects/${projectId}/keys`);
    requireValue(
      keys.some((entry) => entry.label === `${label}-edited`),
      'the key was not updated',
    );
    const editedRow = page.locator('tr').filter({ hasText: `${label}-edited` });
    await editedRow.waitFor({ timeout: 10_000 });
    await editedRow.getByRole('button', { name: 'Revoke' }).click();
    await confirm('Revoke');
    keys = await api(`/projects/${projectId}/keys`);
    requireValue(
      keys.find((entry) => entry.label === `${label}-edited`)?.revoked === true,
      'the key was not revoked',
    );
  });

  await step('policy save and restore', async () => {
    await page.goto(`${consoleBase}/projects/${projectId}/policies`, {
      waitUntil: 'domcontentloaded',
    });
    await waitForPage();
    const document = {
      rules: [
        {
          id: 'ui-record',
          name: 'UI record',
          enabled: true,
          priority: 1,
          phase: 'request',
          when: 'false',
          action: 'record',
          severity: 'info',
          reason: 'browser acceptance',
        },
      ],
    };
    await page.getByLabel('Policy document').fill(JSON.stringify(document, null, 2));
    await page.getByRole('button', { name: 'Check' }).click();
    await page.getByText('The document is valid.').waitFor({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Save and activate' }).click();
    await page.getByText('The policy is active.').waitFor({ timeout: 10_000 });
    let active = await api(`/projects/${projectId}/policies/active`);
    requireValue(active.document.rules?.length === 1, 'the policy was not saved');
    const previous = page.locator('li').filter({ hasText: 'v1' });
    await previous.getByRole('button', { name: 'Restore' }).click();
    await page.getByText('The earlier version is active again.').waitFor({ timeout: 10_000 });
    active = await api(`/projects/${projectId}/policies/active`);
    requireValue((active.document.rules ?? []).length === 0, 'the policy was not restored');
  });

  await step('redaction rule crud', async () => {
    const label = `UI_RULE_${stamp}`;
    await page.goto(`${consoleBase}/projects/${projectId}/redaction`, {
      waitUntil: 'domcontentloaded',
    });
    await waitForPage();
    await page.getByRole('button', { name: 'New rule' }).click();
    const form = await dialog();
    await pick(form, 'Type', 'Regular expression');
    await form.getByLabel('Pattern').fill('ui-[0-9]+');
    await form.getByLabel('Label').fill(label);
    await form.getByLabel('Sample text').fill('prefix ui-123 suffix');
    await form.getByText('1 match').waitFor({ timeout: 10_000 });
    await form.getByRole('button', { name: 'Create', exact: true }).click();
    await form.waitFor({ state: 'hidden', timeout: 10_000 });
    let rules = await api(`/projects/${projectId}/rules`);
    requireValue(
      rules.some((entry) => entry.label === label),
      'the redaction rule was not created',
    );
    const row = page.locator('tr').filter({ hasText: label });
    await row.waitFor({ timeout: 10_000 });
    await row.getByRole('button', { name: 'Edit' }).click();
    const edit = await dialog();
    await edit.getByLabel('Label').fill(`${label}_E`);
    await edit.getByRole('button', { name: 'Save', exact: true }).click();
    await edit.waitFor({ state: 'hidden', timeout: 10_000 });
    rules = await api(`/projects/${projectId}/rules`);
    requireValue(
      rules.some((entry) => entry.label === `${label}_E`),
      'the rule was not updated',
    );
    const edited = page.locator('tr').filter({ hasText: `${label}_E` });
    await edited.waitFor({ timeout: 10_000 });
    await edited.getByRole('button', { name: 'Delete' }).click();
    await confirm('Delete');
    rules = await api(`/projects/${projectId}/rules`);
    requireValue(!rules.some((entry) => entry.label === `${label}_E`), 'the rule was not deleted');
  });
}

await step('user create and disable', async () => {
  const username = `ui${stamp}`;
  await page.goto(`${consoleBase}/users`, { waitUntil: 'domcontentloaded' });
  await waitForPage();
  await page.getByRole('button', { name: 'New user' }).click();
  const form = await dialog();
  await form.getByLabel('Username').fill(username);
  await form.getByRole('button', { name: 'Create', exact: true }).click();
  const secret = await dialog();
  await secret.getByRole('button', { name: 'Secret stored' }).click();
  let users = await api('/users');
  const created = users.find((entry) => entry.username === username);
  requireValue(created, 'the user was not created');
  userId = created.id;
  await page.getByText(username, { exact: true }).waitFor({ timeout: 10_000 });
  const actions = page.getByRole('button', { name: `Actions: ${username}` });
  await actions.click();
  await page.getByRole('menuitem', { name: 'Disable' }).click();
  await page.waitForTimeout(400);
  users = await api('/users');
  requireValue(
    users.find((entry) => entry.username === username)?.disabled === true,
    'the user was not disabled',
  );
  await actions.click();
  await page.getByRole('menuitem', { name: 'Enable' }).click();
  await page.waitForTimeout(400);
});

if (projectId && userId) {
  await step('member add and remove', async () => {
    await page.goto(`${consoleBase}/projects/${projectId}/members`, {
      waitUntil: 'domcontentloaded',
    });
    await waitForPage();
    await page.getByRole('button', { name: 'Add member' }).first().click();
    const form = await dialog();
    await pick(form, 'User', `ui${stamp}`);
    await form.getByRole('button', { name: 'Add member' }).click();
    await form.waitFor({ state: 'hidden', timeout: 10_000 });
    let members = await api(`/projects/${projectId}/members`);
    requireValue(
      members.some((entry) => entry.user_id === userId),
      'the member was not added',
    );
    const row = page.locator('tr').filter({ hasText: `ui${stamp}` });
    await row.waitFor({ timeout: 10_000 });
    await row.getByRole('button', { name: 'Remove' }).click();
    await confirm('Remove');
    members = await api(`/projects/${projectId}/members`);
    requireValue(!members.some((entry) => entry.user_id === userId), 'the member was not removed');
  });
}

await step('channel settings', async () => {
  const name = `UI Channel ${stamp}`;
  await page.goto(`${consoleBase}/channels`, { waitUntil: 'domcontentloaded' });
  await waitForPage();
  await page.getByRole('button', { name: 'New channel' }).click();
  const form = await dialog();
  await form.getByLabel('Name').fill(name);
  await form.getByRole('button', { name: 'Create', exact: true }).click();
  await page.waitForURL(/\/channels\/[^/]+$/, { timeout: 15_000 });
  await waitForPage();
  channelId = page.url().split('/').pop();
  await page.getByLabel('Default output limit').fill('321');
  await page.getByLabel('Forward client headers').fill('x-ui-test');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await page.waitForTimeout(700);
  let loaded = await api(`/channels/${channelId}`);
  requireValue(loaded.default_max_output_tokens === 321, 'the channel limit was not saved');
  requireValue(
    (loaded.forward_headers ?? []).includes('x-ui-test'),
    'the forwarded headers were not saved',
  );
  await page.getByRole('button', { name: 'Add key' }).click();
  const keyForm = await dialog();
  await keyForm.getByLabel('Label').fill('ui');
  await keyForm.getByLabel('API key').fill(`ui-secret-${stamp}`);
  await keyForm.getByRole('button', { name: 'Create', exact: true }).click();
  await keyForm.waitFor({ state: 'hidden', timeout: 10_000 });
  const keys = await api(`/channels/${channelId}/keys`);
  requireValue(
    keys.some((entry) => entry.label === 'ui'),
    'the channel key was not added',
  );
  await page.locator('label').filter({ hasText: 'Enabled' }).getByRole('switch').click();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await page.waitForTimeout(700);
  loaded = await api(`/channels/${channelId}`);
  requireValue(loaded.enabled === false, 'the channel was not disabled');
});

const calls = (await api(`/projects/${project}/calls?limit=100`).catch(() => ({ items: [] })))
  .items;
const highCalls = (
  await api(`/projects/${project}/calls?min_severity=high&limit=100`).catch(() => ({
    items: [],
  }))
).items;
const richCall =
  highCalls.find((call) => call.redaction_hits > 0) ??
  highCalls.find((call) => call.analysis_state === 'completed') ??
  highCalls[0] ??
  calls[0];

async function fetchCalls(query) {
  return (await api(`/projects/${project}/calls?${query}`).catch(() => ({ items: [] }))).items;
}

async function findMultiSession() {
  const keys = await api(`/projects/${project}/keys`).catch(() => []);
  for (const key of keys) {
    const items = await fetchCalls(`agent_key_id=${encodeURIComponent(key.id)}&limit=100`);
    const counts = new Map();
    for (const call of items) counts.set(call.session_id, (counts.get(call.session_id) ?? 0) + 1);
    for (const [id, count] of counts) if (count > 1) return id;
  }
  return null;
}

async function findProblemCall() {
  for (const state of ['blocked', 'rejected', 'failed']) {
    const items = await fetchCalls(`state=${state}&limit=1`);
    if (items[0]) return items[0];
  }
  return null;
}

const channels = await api('/channels').catch(() => []);
requireValue(channels.length > 0, 'the lab has no channel for the channel detail page');
const channel = channels[0];

const pages = [
  ['03-projects', '/projects'],
  ['04-overview', `/projects/${project}/overview`],
  ['05-calls', `/projects/${project}/calls`],
  ['06-conversations', `/projects/${project}/conversations`],
  ['07-risk', `/projects/${project}/risk`],
  ['08-activity', `/projects/${project}/activity`],
  ['09-policies', `/projects/${project}/policies`],
  ['10-redaction', `/projects/${project}/redaction`],
  ['11-keys', `/projects/${project}/keys`],
  ['12-routes', `/projects/${project}/routes`],
  ['13-members', `/projects/${project}/members`],
  ['14-settings', `/projects/${project}/settings`],
  ['15-channels', '/channels'],
  ['15b-channel', `/channels/${channel.id}`],
  ['16-users', '/users'],
  ['17-operations', '/operations'],
  ['18-password', '/password'],
];

for (const [name, path] of pages) {
  await step(`page ${path}`, async () => {
    const response = await page.goto(consoleBase + path, { waitUntil: 'domcontentloaded' });
    await waitForPage();
    if (!response || response.status() >= 400) throw new Error(`HTTP ${response?.status()}`);
    const heading = await page
      .locator('h1')
      .first()
      .textContent()
      .catch(() => null);
    if (!heading) throw new Error('no h1');
    const body = await page.locator('body').innerText();
    if (body.trim().length < 40) throw new Error('empty body');
    await shot(name);
  });
}

if (richCall) {
  await step('call detail tabs', async () => {
    await page.goto(`${consoleBase}/projects/${project}/calls?call=${richCall.id}`, {
      waitUntil: 'domcontentloaded',
    });
    await waitForPage();
    await shot('19-call-detail');
    for (const [tab, name] of [
      ['Transcript', '20-call-transcript'],
      ['Events', '21-call-events'],
      ['Attempts', '22-call-attempts'],
    ]) {
      const trigger = page.getByRole('tab', { name: new RegExp(tab, 'i') });
      requireValue((await trigger.count()) > 0, `no ${tab} tab`);
      await trigger.first().click();
      await page.waitForTimeout(300);
      await shot(name, false);
    }
  });
}

const session = await findMultiSession();
if (session) {
  await page.goto(`${consoleBase}/projects/${project}/conversations/${session}`, {
    waitUntil: 'domcontentloaded',
  });
  await waitForPage();
  await shot('23-conversation');
} else {
  failures.push('no multi-call conversation was available for the tree check');
}

const problemCall = await findProblemCall();
if (problemCall) {
  await page.goto(`${consoleBase}/projects/${project}/calls?call=${problemCall.id}`, {
    waitUntil: 'domcontentloaded',
  });
  await waitForPage();
  await shot('24-call-blocked');
  const body = await page.locator('body').innerText();
  if (!/blocked|rejected|failed/i.test(body)) {
    failures.push('the blocked call detail does not show its state');
  }
} else {
  failures.push('no blocked, rejected or failed call was available for the detail check');
}

await step('language and theme', async () => {
  await page.goto(`${consoleBase}/monitor`, { waitUntil: 'domcontentloaded' });
  await waitForPage();
  const language = page.getByRole('button', { name: /language|语言/i }).first();
  requireValue((await language.count()) > 0, 'language menu button is missing');
  await language.click();
  await shot('25-language-menu', false);
  const chinese = page.getByRole('menuitemradio', { name: '简体中文' });
  requireValue((await chinese.count()) > 0, 'language menu has no Chinese option');
  await chinese.click();
  await page.waitForTimeout(600);
  requireValue(
    (await page.locator('html').getAttribute('lang')) === 'zh-CN',
    'language switch did not set html lang',
  );
  await shot('26-monitor-zh');
  const theme = page.getByRole('button', { name: /theme|主题/i }).first();
  await theme.click();
  const dark = page.getByRole('menuitemradio', { name: /dark|深色/i });
  requireValue((await dark.count()) > 0, 'theme menu has no dark option');
  await dark.click();
  await page.waitForTimeout(500);
  await shot('27-monitor-dark');
  const lightButton = page.getByRole('button', { name: /theme|主题/i }).first();
  await lightButton.click();
  await page.getByRole('menuitemradio', { name: /light|浅色/i }).click();
});

const state = await context.storageState();
const mobile = await browser.newContext({
  viewport: { width: 390, height: 844 },
  locale: 'zh-CN',
  storageState: state,
});
const phone = await mobile.newPage();
phone.on('pageerror', (error) => failures.push(`mobile pageerror: ${error.message}`));
phone.on('response', (response) => {
  if (response.status() >= 400 && !response.url().includes('/favicon')) {
    failures.push(`mobile http ${response.status()}: ${response.url()}`);
  }
});
for (const [name, path] of pages) {
  await phone.goto(consoleBase + path, { waitUntil: 'domcontentloaded' });
  await phone.waitForTimeout(600);
  const overflow = await phone.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  if (overflow > 2) failures.push(`mobile ${path} overflows horizontally by ${overflow}px`);
  await phone.screenshot({ path: `${shots}/m-${name}.png`, fullPage: true });
}
await phone.goto(`${consoleBase}/projects/${project}/overview`, {
  waitUntil: 'domcontentloaded',
});
await phone.waitForTimeout(500);
const menu = phone
  .getByRole('button', { name: /open menu|打开菜单|open navigation|打开导航/i })
  .first();
if ((await menu.count()) > 0) {
  await menu.click();
  await phone.waitForTimeout(300);
  await phone.screenshot({ path: `${shots}/m-menu.png`, fullPage: false });
} else {
  failures.push('mobile navigation button is missing');
}

await step('cleanup', async () => {
  if (projectId) await api(`/projects/${projectId}`, 'DELETE');
  if (userId) await api(`/users/${userId}`, 'PUT', { disabled: true, administrator: false });
  if (channelId) {
    await api(`/channels/${channelId}`, 'PUT', {
      ...(await api(`/channels/${channelId}`)),
      enabled: false,
    }).catch(() => {});
  }
});

const report = {
  pages: pages.map(([name, path]) => ({ name, path })),
  projectId,
  richCall: richCall?.id ?? null,
  failures,
};
fs.writeFileSync(`${shots}/report.json`, JSON.stringify(report, null, 2));
await browser.close();
if (failures.length) {
  console.error(`FAILED\n${failures.join('\n')}`);
  process.exit(1);
}
console.log(`console acceptance passed (${pages.length} pages, desktop and mobile)`);
