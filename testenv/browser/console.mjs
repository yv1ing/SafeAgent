// Browser acceptance for the operator console: every page must render without a client error or a
// failed API call, and the primary interactions must work at desktop and mobile widths.
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
  await page.waitForTimeout(900);
  const errorBoundary = await page.getByText(/something went wrong|出了点问题|加载失败/i).count();
  if (errorBoundary) failures.push(`error boundary on ${page.url()}`);
}

recording = true;
await page.goto(`${consoleBase}/login`, { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(500);
await shot('01-login');
await page.locator('input[autocomplete="username"]').fill('admin');
await page.locator('input[autocomplete="current-password"]').fill(password);
await page.locator('button[type="submit"]').click();
await page.waitForURL((url) => !url.pathname.endsWith('/login'), {
  timeout: 30_000,
});
await waitForPage();
await shot('02-monitor');

async function fetchCalls(query) {
  const response = await page.request.get(`${consoleBase}/api/projects/${project}/calls?${query}`);
  return response.ok() ? (await response.json()).items : [];
}

async function findMultiSession() {
  const keysResponse = await page.request.get(`${consoleBase}/api/projects/${project}/keys`);
  const keys = keysResponse.ok() ? await keysResponse.json() : [];
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

const calls = await fetchCalls('limit=100');
const highCalls = await fetchCalls('min_severity=high&limit=100');
const richCall =
  highCalls.find((call) => call.redaction_hits > 0) ??
  highCalls.find((call) => call.analysis_state === 'completed') ??
  highCalls[0] ??
  calls[0];
const channelsResponse = await page.request.get(`${consoleBase}/api/channels`);
const channels = channelsResponse.ok() ? await channelsResponse.json() : [];
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
  ['16-users', '/users'],
  ['17-operations', '/operations'],
];
if (channel) {
  pages.splice(14, 0, ['15-channels', '/channels'], ['15b-channel', `/channels/${channel.id}`]);
}

for (const [name, path] of pages) {
  const response = await page.goto(consoleBase + path, {
    waitUntil: 'domcontentloaded',
  });
  await waitForPage();
  if (!response || response.status() >= 400) failures.push(`${path}: HTTP ${response?.status()}`);
  const heading = await page
    .locator('h1')
    .first()
    .textContent()
    .catch(() => null);
  if (!heading) failures.push(`${path}: no h1`);
  const body = await page.locator('body').innerText();
  if (body.trim().length < 40) failures.push(`${path}: empty body`);
  await shot(name);
}

await page.goto(`${consoleBase}/projects`, { waitUntil: 'domcontentloaded' });
await waitForPage();
const addProject = page.getByRole('button', { name: /new project/i }).first();
if (await addProject.count()) {
  const name = `UI Acceptance ${Date.now()}`;
  await addProject.click();
  const dialog = page.getByRole('dialog');
  await dialog.locator('input').first().fill(name);
  await dialog.getByRole('button', { name: /^create$/i }).click();
  await page.getByText(name, { exact: true }).waitFor({ timeout: 10_000 });
  await shot('28-project-created');
  const projects = await page.request.get(`${consoleBase}/api/projects`);
  const created = projects.ok()
    ? (await projects.json()).find((entry) => entry.name === name)
    : null;
  if (created) {
    await page.request.delete(`${consoleBase}/api/projects/${created.id}`, {
      headers: { 'X-SafeAgent-Request': '1', Origin: base },
    });
  } else {
    failures.push('the project created through the console was not listed by the API');
  }
} else {
  failures.push('the new project button is missing');
}

if (richCall) {
  await page.goto(`${consoleBase}/projects/${project}/calls?call=${richCall.id}`, {
    waitUntil: 'domcontentloaded',
  });
  await waitForPage();
  await page.waitForTimeout(600);
  await shot('18-call-detail');
  for (const [tab, name] of [
    ['Transcript', '19-call-transcript'],
    ['Events', '20-call-events'],
    ['Attempts', '21-call-attempts'],
  ]) {
    const trigger = page.getByRole('tab', { name: new RegExp(tab, 'i') });
    if (await trigger.count()) {
      await trigger.first().click();
      await page.waitForTimeout(350);
      await shot(name, false);
    } else {
      failures.push(`call detail: no ${tab} tab`);
    }
  }
}

const session = await findMultiSession();
if (session) {
  await page.goto(`${consoleBase}/projects/${project}/conversations/${session}`, {
    waitUntil: 'domcontentloaded',
  });
  await waitForPage();
  await page.waitForTimeout(600);
  await shot('30-conversation');
} else {
  failures.push('no multi-call conversation was available for the tree check');
}

const problemCall = await findProblemCall();
if (problemCall) {
  await page.goto(`${consoleBase}/projects/${project}/calls?call=${problemCall.id}`, {
    waitUntil: 'domcontentloaded',
  });
  await waitForPage();
  await page.waitForTimeout(600);
  await shot('29-call-blocked');
  const body = await page.locator('body').innerText();
  if (!/blocked|rejected|failed/i.test(body)) {
    failures.push('the blocked call detail does not show its state');
  }
} else {
  failures.push('no blocked, rejected or failed call was available for the detail check');
}

await page.goto(`${consoleBase}/monitor`, { waitUntil: 'domcontentloaded' });
await waitForPage();
const language = page.getByRole('button', { name: /language|语言/i }).first();
if (await language.count()) {
  await language.click();
  await page.waitForTimeout(250);
  await shot('22-language-menu', false);
  const chinese = page.getByRole('menuitemradio', { name: '简体中文' });
  if (await chinese.count()) {
    await chinese.click();
    await page.waitForTimeout(700);
    if ((await page.locator('html').getAttribute('lang')) !== 'zh-CN')
      failures.push('language switch did not set html lang');
    await shot('23-monitor-zh');
  } else {
    failures.push('language menu has no Chinese option');
  }
} else {
  failures.push('language menu button is missing');
}

const theme = page.getByRole('button', { name: /theme|主题/i }).first();
if (await theme.count()) {
  await theme.click();
  await page.waitForTimeout(200);
  const dark = page.getByRole('menuitemradio', { name: /dark|深色/i });
  if (await dark.count()) {
    await dark.click();
    await page.waitForTimeout(600);
    await shot('24-monitor-dark');
    const light = page.getByRole('button', { name: /theme|主题/i }).first();
    await light.click();
    await page.waitForTimeout(150);
    const lightOption = page.getByRole('menuitemradio', {
      name: /light|浅色/i,
    });
    if (await lightOption.count()) await lightOption.click();
  } else {
    failures.push('theme menu has no dark option');
  }
}

const state = await context.storageState();
const mobile = await browser.newContext({
  viewport: { width: 390, height: 844 },
  locale: 'zh-CN',
  storageState: state,
});
const phone = await mobile.newPage();
phone.on('pageerror', (error) => failures.push(`mobile pageerror: ${error.message}`));
for (const [name, path] of pages) {
  await phone.goto(consoleBase + path, { waitUntil: 'domcontentloaded' });
  await phone.waitForTimeout(900);
  const overflow = await phone.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  if (overflow > 2) failures.push(`mobile ${path} overflows horizontally by ${overflow}px`);
  await phone.screenshot({ path: `${shots}/m-${name}.png`, fullPage: true });
}
const menu = phone
  .getByRole('button', { name: /open menu|打开菜单|open navigation|打开导航/i })
  .first();
if (await menu.count()) {
  await phone.click('body', { position: { x: 5, y: 5 } }).catch(() => {});
  await phone.goto(`${consoleBase}/projects/${project}/overview`, {
    waitUntil: 'domcontentloaded',
  });
  await phone.waitForTimeout(700);
  await menu.click();
  await phone.waitForTimeout(400);
  await phone.screenshot({
    path: `${shots}/26-mobile-menu.png`,
    fullPage: false,
  });
} else {
  failures.push('mobile navigation button is missing');
}

const report = {
  pages: pages.map(([name, path]) => ({ name, path })),
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
