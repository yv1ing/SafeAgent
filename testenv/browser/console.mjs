// Browser acceptance for the operator console.
//
// One browser session drives the current console against a live lab gateway:
//   - every page must render an <h1>, without a client error and without a failed API call;
//   - desktop flows create and remove a project, a provider graph, keys, policies, redaction rules,
//     users and members through the UI, and soft-delete/restore the graph again;
//   - traffic-dependent checks (call filters, call detail, conversations, trajectory, risk rows) are
//     skipped with a clear log line when the lab has no calls, so the suite runs with and without
//     upstream credentials;
//   - screenshots are written for every page and flow;
//   - page errors, console errors and failed API responses are aggregated into the exit status.
//
// Required environment:
//   SAFEAGENT_BASE_URL          gateway origin (default http://host.docker.internal:8080)
//   SAFEAGENT_ADMIN_PASSWORD    administrator password
//   SAFEAGENT_PROJECT           id of an existing lab project used for traffic checks and pages
// Optional environment:
//   SAFEAGENT_ADMIN_NEW_PASSWORD  new password used when the administrator must change the password
//                                 at sign-in; the suite completes the change and continues
//   SAFEAGENT_SCREENSHOTS         screenshot directory (default /screenshots)
//
// Every created name carries one timestamp stamp, so the run is deterministic and safe to repeat
// against a lab that already holds data from an earlier run.
import { chromium } from 'playwright';
import fs from 'node:fs';

const base = (process.env.SAFEAGENT_BASE_URL ?? 'http://host.docker.internal:8080').replace(
  /\/+$/,
  '',
);
const consoleBase = `${base}/console`;
const password = process.env.SAFEAGENT_ADMIN_PASSWORD;
const nextAdminPassword = process.env.SAFEAGENT_ADMIN_NEW_PASSWORD;
const labProjectId = process.env.SAFEAGENT_PROJECT;
const shots = process.env.SAFEAGENT_SCREENSHOTS ?? '/screenshots';
if (!password || !labProjectId) {
  console.error('SAFEAGENT_ADMIN_PASSWORD and SAFEAGENT_PROJECT are required');
  process.exit(2);
}
fs.mkdirSync(shots, { recursive: true });

// Console copy read from the English i18n dictionary. The suite runs in the English locale except
// for the language-switch check, so every label here exists in web/src/i18n/messages.ts.
const L = {
  acknowledge: 'I understand the consequences.',
  addAlias: 'Add alias',
  addMember: 'Add member',
  alias: 'Alias',
  apiKey: 'API key',
  auxiliaryRetention: 'Auxiliary retention (days)',
  baseUrl: 'Base URL',
  blocked: 'Blocked',
  calls: 'Calls',
  check: 'Check',
  clearFilters: 'Clear filters',
  closeInspector: 'Close inspector',
  completed: 'Completed',
  connect: 'Connect provider',
  connectAndDiscover: 'Connect and discover',
  connected: 'Connected',
  create: 'Create',
  createProjectRoute: 'Create a project route',
  currentPassword: 'Current password',
  customProvider: 'Custom provider',
  dark: 'Dark',
  defaultOutputLimit: 'Default output limit',
  defaultRoute: 'Default route',
  delete: 'Delete',
  deletion: 'Deletion',
  edit: 'Edit',
  endpointCatalog: 'Endpoint catalog',
  failures: 'Failures',
  finishSetup: 'Finish setup',
  flat: 'Flat',
  graphView: 'Graph view',
  highFindings: 'High-severity calls',
  highSeverity: 'High',
  label: 'Label',
  language: 'Language',
  lane: 'Lane',
  last24Hours: 'Last 24 hours',
  last7Days: 'Last 7 days',
  light: 'Light',
  loadFailed: 'The page could not be loaded.',
  main: 'Main',
  mainLaneNote: 'only count main-lane calls',
  minimumSeverity: 'Minimum severity',
  modelNames: 'Model names',
  name: 'Name',
  newKey: 'New key',
  newPassword: 'New password',
  newProject: 'New project',
  newRule: 'New rule',
  newUser: 'New user',
  next: 'Next',
  nextStep: 'Next step',
  noAuditEvents: 'No audit events were recorded.',
  noCalls: 'No calls recorded.',
  noConversations: 'No conversations recorded.',
  noData: 'No data in this window.',
  noFindings: 'No findings in this window.',
  noRedactedValues: 'No redacted values in this conversation.',
  noRedactions: 'This call introduced no redacted values.',
  noResults: 'No results match these filters.',
  openNavigation: 'Open navigation',
  operation: 'Operation',
  outcome: 'Outcome',
  password: 'Password',
  pattern: 'Pattern',
  policyActive: 'The policy is active.',
  policyDocument: 'Policy document',
  policyRestored: 'The earlier version is active again.',
  policyValid: 'The document is valid.',
  previousStep: 'Previous step',
  project: 'Project',
  purpose: 'Purpose',
  raw: 'Raw',
  redactedValues: 'Redacted values',
  regularExpression: 'Regular expression',
  remove: 'Remove',
  requestsMainLane: 'Requests (main lane)',
  restore: 'Restore',
  riskSources: 'Risk sources',
  ruleTest: 'Rule to test',
  sampleText: 'Sample text',
  save: 'Save',
  saveAndActivate: 'Save and activate',
  secretStored: 'Secret stored',
  selectAll: 'Select all',
  showAuxiliary: 'Show auxiliary calls',
  showDeleted: 'Show deleted',
  showInOriginal: 'Show in original',
  shown: 'Shown',
  signIn: 'Sign in',
  state: 'State',
  succeeded: 'Succeeded',
  theme: 'Theme',
  timeWindow: 'Time window',
  transcriptText: 'Transcript text',
  tree: 'Tree',
  type: 'Type',
  updatePassword: 'Update password',
  user: 'User',
  username: 'Username',
  view: 'View',
  changesSaved: 'Changes saved.',
};

const stamp = Date.now();
const failures = [];
let recording = false;
const allowedResponses = [];

const browser = await chromium.launch({ args: ['--no-sandbox'] });
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
  colorScheme: 'light',
  locale: 'en-US',
});
const page = await context.newPage();

function watch(target, prefix = '') {
  target.on('pageerror', (error) => failures.push(`${prefix}pageerror: ${error.message}`));
  target.on('console', (message) => {
    if (recording && message.type() === 'error') {
      failures.push(`${prefix}console: ${message.text()}`);
    }
  });
  target.on('response', (response) => {
    if (!recording) return;
    const status = response.status();
    const url = response.url();
    if (status < 400 || url.includes('/favicon')) return;
    const index = allowedResponses.findIndex(
      (entry) => entry.status === status && url.includes(entry.urlPart),
    );
    if (index >= 0) {
      allowedResponses.splice(index, 1);
      return;
    }
    failures.push(`${prefix}http ${status}: ${url}`);
  });
}

watch(page);

async function shot(name, fullPage = true) {
  await page.screenshot({ path: `${shots}/${name}.png`, fullPage });
}

async function bodyText() {
  return page.locator('body').innerText();
}

function requireValue(condition, message) {
  if (!condition) throw new Error(message);
}

async function waitForPage() {
  await page.waitForLoadState('domcontentloaded').catch(() => {});
  await page.waitForSelector('main h1', { timeout: 20_000 }).catch(() => {});
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
  await page.waitForTimeout(150);
  const text = await bodyText();
  if (text.includes(L.loadFailed) || text.includes('页面加载失败')) {
    throw new Error(`error boundary on ${page.url()}`);
  }
  if (text.trim().length < 40) throw new Error(`empty page at ${page.url()}`);
  const heading = await page
    .locator('main h1')
    .first()
    .textContent()
    .catch(() => null);
  if (!heading || !heading.trim()) throw new Error(`no h1 at ${page.url()}`);
}

async function goto(path) {
  const response = await page.goto(consoleBase + path, { waitUntil: 'domcontentloaded' });
  await waitForPage();
  if (response && response.status() >= 400)
    throw new Error(`HTTP ${response.status()} for ${path}`);
}

async function step(name, run) {
  try {
    await run();
  } catch (error) {
    failures.push(`${name}: ${error.message}`);
    await shot(`failure-${name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}`, false).catch(() => {});
  }
}

function allowResponse(status, urlPart) {
  allowedResponses.push({ status, urlPart });
}

async function api(path, method = 'GET', data, options = {}) {
  const url = `${consoleBase}/api${path}`;
  if (options.expectStatus) allowResponse(options.expectStatus, url);
  const response = await page.request.fetch(url, {
    method,
    headers: {
      'X-SafeAgent-Request': '1',
      Origin: base,
      'Content-Type': 'application/json',
    },
    data: data === undefined ? undefined : JSON.stringify(data),
  });
  const text = await response.text();
  const body = text === '' ? null : JSON.parse(text);
  if (options.expectStatus) {
    const stale = allowedResponses.findIndex((entry) => entry.status === options.expectStatus);
    if (stale >= 0) allowedResponses.splice(stale, 1);
    if (response.status() !== options.expectStatus) {
      throw new Error(
        `${method} ${path} -> HTTP ${response.status()} (expected ${options.expectStatus})`,
      );
    }
    return body;
  }
  if (!response.ok()) {
    throw new Error(`${method} ${path} -> HTTP ${response.status()} ${text.slice(0, 200)}`);
  }
  return body;
}

async function waitForApi(loader, predicate, description, attempts = 75) {
  let value;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    value = await loader();
    if (predicate(value)) return value;
    await page.waitForTimeout(200);
  }
  throw new Error(`timed out waiting for ${description}`);
}

async function openDialog() {
  const dialog = page.getByRole('dialog').last();
  await dialog.waitFor({ timeout: 10_000 });
  return dialog;
}

async function pick(scope, label, option) {
  await scope.getByLabel(label, { exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
  await page.waitForTimeout(150);
}

async function expectText(text, description = text) {
  await page
    .getByText(text, { exact: true })
    .first()
    .waitFor({ timeout: 15_000 })
    .catch(() => {
      throw new Error(`${description} did not appear`);
    });
}

async function confirmDialog(label) {
  const dialog = await openDialog();
  await dialog.getByRole('button', { name: label, exact: true }).click();
  await dialog.waitFor({ state: 'hidden', timeout: 20_000 });
}

async function confirmImpactDialog(label) {
  const dialog = await openDialog();
  const confirm = dialog.getByRole('button', { name: label, exact: true });
  const acknowledge = dialog.getByRole('switch', { name: L.acknowledge });
  await acknowledge.waitFor({ timeout: 10_000 });
  requireValue(
    await confirm.isDisabled(),
    'the impact confirmation is enabled before acknowledgement',
  );
  await acknowledge.click();
  await confirm.click();
  await dialog.waitFor({ state: 'hidden', timeout: 20_000 });
}

async function completePasswordChangeOn(target, current, next) {
  await target.getByLabel(L.currentPassword).fill(current);
  await target.getByLabel(L.newPassword, { exact: true }).fill(next);
  await target.getByLabel('Repeat new password').fill(next);
  await target.getByRole('button', { name: L.updatePassword, exact: true }).click();
  await target.waitForURL((url) => !url.pathname.endsWith('/password'), { timeout: 30_000 });
}

async function signIn() {
  await page.goto(`${consoleBase}/login`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('form', { timeout: 20_000 });
  await shot('01-login');
  await page.getByLabel(L.username).fill('admin');
  await page.getByLabel(L.password).fill(password);
  await page.getByRole('button', { name: L.signIn, exact: true }).click();
  await page.waitForURL((url) => !url.pathname.endsWith('/login'), { timeout: 30_000 });
  if (page.url().includes('/password')) {
    if (!nextAdminPassword) {
      throw new Error(
        'the administrator must change the password; set SAFEAGENT_ADMIN_NEW_PASSWORD',
      );
    }
    await completePasswordChangeOn(page, password, nextAdminPassword);
  }
  await waitForPage();
  await shot('02-monitor');
}

async function createProject() {
  await goto('/projects');
  await shot('03-projects');
  await page.getByRole('button', { name: L.newProject, exact: true }).click();
  const form = await openDialog();
  await form.getByLabel(L.name).fill(suiteProjectName);
  await form.getByRole('button', { name: L.create, exact: true }).click();
  await form.waitFor({ state: 'hidden', timeout: 20_000 });
  const projects = await waitForApi(
    () => api('/projects'),
    (list) => list.some((entry) => entry.name === suiteProjectName),
    'the created project to be listed',
  );
  projectId = projects.find((entry) => entry.name === suiteProjectName).id;
  const openProject = page.getByRole('link', { name: suiteProjectName, exact: true }).first();
  await openProject.waitFor({ timeout: 10_000 });
  await openProject.click();
  await page.waitForURL(new RegExp(`/projects/${projectId}/overview`), { timeout: 20_000 });
  await waitForPage();
}

async function switchProjects() {
  const lab = await api(`/projects/${labProjectId}`);
  await page.getByRole('button', { name: suiteProjectName, exact: true }).click();
  await page.getByRole('menuitem', { name: lab.name, exact: true }).click();
  await page.waitForURL(new RegExp(`/projects/${labProjectId}/overview`), { timeout: 20_000 });
  await waitForPage();
  await page.getByRole('button', { name: lab.name, exact: true }).click();
  await page.getByRole('menuitem', { name: suiteProjectName, exact: true }).click();
  await page.waitForURL(new RegExp(`/projects/${projectId}/overview`), { timeout: 20_000 });
  await waitForPage();
  await shot('04-project-switcher', false);
}

async function saveProjectSettings() {
  await goto(`/projects/${projectId}/settings`);
  await shot('05-project-settings');
  suiteProjectName = `${suiteProjectName} Renamed`;
  await page.getByLabel(L.name).fill(suiteProjectName);
  await page.getByLabel(L.auxiliaryRetention).fill('3');
  await page.getByRole('button', { name: L.save, exact: true }).first().click();
  await expectText(L.changesSaved);
  const saved = await waitForApi(
    () => api(`/projects/${projectId}`),
    (project) => project.name === suiteProjectName && project.auxiliary_retention_days === 3,
    'the project settings to persist',
  );
  requireValue(saved.auxiliary_retention_days === 3, 'the auxiliary retention was not saved');
}

async function connectProvider() {
  await goto('/providers');
  await page.getByRole('button', { name: L.connect, exact: true }).click();
  const wizard = await openDialog();
  await wizard.getByText('Connect a provider', { exact: true }).waitFor({ timeout: 10_000 });

  await wizard.getByRole('button', { name: L.customProvider, exact: true }).click();
  const providerStep = wizard.locator('section:not([hidden])');
  await providerStep.getByLabel(L.label, { exact: true }).fill(providerLabel);
  const catalog = {
    openai_chat: {
      Format: 'openai_chat',
      BaseURL: 'http://127.0.0.1:9',
      Path: '/v1/chat/completions',
    },
  };
  await providerStep
    .getByLabel(L.endpointCatalog, { exact: true })
    .fill(JSON.stringify(catalog, null, 2));
  await shot('10-provider-wizard-provider', false);
  await wizard.getByRole('button', { name: L.next, exact: true }).click();

  const channelStep = wizard.locator('section:not([hidden])');
  await channelStep.getByLabel(L.name, { exact: true }).fill(channelName);
  await channelStep.getByLabel(L.baseUrl, { exact: true }).fill('http://127.0.0.1:9');
  await channelStep.getByLabel(L.apiKey, { exact: true }).fill(`ui-secret-${stamp}`);
  await shot('11-provider-wizard-channel', false);
  await wizard.getByRole('button', { name: L.next, exact: true }).click();

  const discoveryStep = wizard.locator('section:not([hidden])');
  await discoveryStep.getByRole('button', { name: L.connectAndDiscover, exact: true }).click();
  await discoveryStep.getByText(L.connected, { exact: true }).waitFor({ timeout: 20_000 });
  await shot('12-provider-wizard-discovery', false);
  await wizard.getByRole('button', { name: L.next, exact: true }).click();

  const modelsStep = wizard.locator('section:not([hidden])');
  await modelsStep.getByLabel(L.modelNames, { exact: true }).fill(modelName);
  await modelsStep.getByRole('button', { name: L.selectAll, exact: true }).click();
  await shot('13-provider-wizard-models', false);
  await wizard.getByRole('button', { name: L.next, exact: true }).click();

  const routeStep = wizard.locator('section:not([hidden])');
  await routeStep
    .locator('label')
    .filter({ hasText: L.createProjectRoute })
    .getByRole('switch')
    .click();
  await pick(routeStep, L.project, suiteProjectName);
  await routeStep.getByLabel(L.name, { exact: true }).fill(routeName);
  await routeStep.getByLabel(L.defaultOutputLimit, { exact: true }).fill('111');
  await shot('14-provider-wizard-route', false);
  await wizard.getByRole('button', { name: L.finishSetup, exact: true }).click();
  await wizard.waitFor({ state: 'hidden', timeout: 30_000 });

  const providers = await waitForApi(
    () => api('/providers'),
    (list) => list.some((entry) => entry.label === providerLabel),
    'the custom provider to be listed',
  );
  providerId = providers.find((entry) => entry.label === providerLabel).id;
  const channels = await waitForApi(
    () => api('/channels'),
    (list) => list.some((entry) => entry.name === channelName),
    'the channel to be listed',
  );
  channelId = channels.find((entry) => entry.name === channelName).id;
  const models = await waitForApi(
    () => api('/models'),
    (list) => list.some((entry) => entry.upstream_name === modelName),
    'the model to be listed',
  );
  modelId = models.find((entry) => entry.upstream_name === modelName).id;
  const routes = await waitForApi(
    () => api(`/projects/${projectId}/routes`),
    (list) => list.some((entry) => entry.name === routeName),
    'the route to be listed',
  );
  const created = routes.find((entry) => entry.name === routeName);
  routeId = created.id;
  const target = created.targets?.[0];
  requireValue(target?.channel_id === channelId, 'the route target does not use the new channel');
  requireValue(target?.model_id === modelId, 'the route target does not use the new model');
  requireValue(target?.max_output_tokens === 111, 'the route target output limit was not saved');
  console.log(`provider graph: provider=${providerId} channel=${channelId} model=${modelId}`);
}

async function addRouteAlias() {
  await goto(`/projects/${projectId}/routes`);
  const row = page.locator('tr').filter({ hasText: routeName });
  await row.waitFor({ timeout: 10_000 });
  await shot('15-routes', false);
  await row.getByRole('button', { name: L.edit, exact: true }).click();
  const dialog = await openDialog();
  await dialog.getByRole('button', { name: L.addAlias, exact: true }).click();
  await dialog.getByLabel(L.alias, { exact: true }).fill(routeAlias);
  await dialog.getByRole('button', { name: L.save, exact: true }).click();
  await dialog.waitFor({ state: 'hidden', timeout: 20_000 });
  const route = await waitForApi(
    () => api(`/projects/${projectId}/routes`),
    (list) =>
      list
        .find((entry) => entry.id === routeId)
        ?.aliases.some((alias) => alias.alias === routeAlias && alias.source === 'manual') === true,
    'the route alias to be saved',
  );
  requireValue(Boolean(route.find((entry) => entry.id === routeId)), 'the route disappeared');
}

async function createAgentKey() {
  await goto(`/projects/${projectId}/keys`);
  await shot('16-keys', false);
  await page.getByRole('button', { name: L.newKey, exact: true }).click();
  const form = await openDialog();
  await form.getByLabel(L.label, { exact: true }).fill(keyLabel);
  await form.getByRole('button', { name: L.create, exact: true }).click();
  const secret = await openDialog();
  await secret.locator('input[readonly]').waitFor({ timeout: 10_000 });
  const value = await secret.locator('input[readonly]').inputValue();
  requireValue(value.length > 0, 'the new agent key has no secret');
  await page.screenshot({ path: `${shots}/17-key-secret.png` });
  await secret.getByRole('button', { name: L.secretStored, exact: true }).click();
  await secret.waitFor({ state: 'hidden', timeout: 10_000 });
  const keys = await waitForApi(
    () => api(`/projects/${projectId}/keys`),
    (list) => list.some((entry) => entry.label === keyLabel),
    'the agent key to be listed',
  );
  agentKeyId = keys.find((entry) => entry.label === keyLabel).id;
}

const projectPages = [
  ['project-overview', `/projects/${labProjectId}/overview`],
  ['project-calls', `/projects/${labProjectId}/calls`],
  ['project-conversations', `/projects/${labProjectId}/conversations`],
  ['project-risk', `/projects/${labProjectId}/risk`],
  ['project-activity', `/projects/${labProjectId}/activity`],
  ['project-policies', `/projects/${labProjectId}/policies`],
  ['project-redaction', `/projects/${labProjectId}/redaction`],
  ['project-keys', `/projects/${labProjectId}/keys`],
  ['project-routes', `/projects/${labProjectId}/routes`],
  ['project-members', `/projects/${labProjectId}/members`],
  ['project-settings', `/projects/${labProjectId}/settings`],
];

const globalPages = [
  ['monitor', '/monitor'],
  ['projects', '/projects'],
  ['providers', '/providers'],
  ['models', '/models'],
  ['channels', '/channels'],
  ['users', '/users'],
  ['operations', '/operations'],
  ['password', '/password'],
];

let pages = [];

async function sweepPages() {
  pages = [...projectPages, ...globalPages];
  if (channelId) pages.push(['channel-detail', `/channels/${channelId}`]);
  for (const [name, path] of pages) {
    await step(`page ${path}`, async () => {
      await goto(path);
      await shot(`page-${name}`);
    });
  }
}

async function deleteAndRestoreChannel() {
  await goto(`/channels/${channelId}`);
  const impact = await api(`/configuration/impact?channel_id=${channelId}`);
  requireValue(
    impact.routes.some((reference) => reference.route_id === routeId),
    'the channel impact does not name the referencing route',
  );
  const conflict = await api(`/channels/${channelId}`, 'DELETE', undefined, { expectStatus: 409 });
  requireValue(
    conflict?.code === 'impact_requires_acknowledge',
    'the channel delete without acknowledgement did not require an impact review',
  );
  const deleteButton = page
    .getByRole('button', { name: L.delete, exact: true })
    .filter({ has: page.locator('svg') });
  await deleteButton.click();
  await page.waitForTimeout(250);
  await shot('20-channel-impact', false);
  await confirmImpactDialog(L.delete);
  await page.waitForURL(/\/channels$/, { timeout: 20_000 });
  await waitForPage();
  const deleted = await waitForApi(
    () => api('/channels?include_deleted=true'),
    (list) => list.some((entry) => entry.id === channelId && entry.deleted_at),
    'the channel to be soft deleted',
  );
  requireValue(
    Boolean(deleted.find((entry) => entry.id === channelId).deleted_at),
    'no deleted_at',
  );
  const degraded = await api(`/projects/${projectId}/routes`);
  const target = degraded.find((entry) => entry.id === routeId).targets[0];
  requireValue(target.enabled === false, 'the route target was not disabled with the channel');

  await goto('/channels');
  await pick(page, L.deletion, L.showDeleted);
  const row = page.locator('tr').filter({ hasText: channelName });
  await row.waitFor({ timeout: 10_000 });
  await shot('21-channel-deleted', false);
  await row.getByRole('button', { name: L.restore, exact: true }).click();
  await waitForApi(
    () => api('/channels'),
    (list) => list.some((entry) => entry.id === channelId),
    'the channel to be restored',
  );
  const restoredRoute = await waitForApi(
    () => api(`/projects/${projectId}/routes`),
    (list) => list.find((entry) => entry.id === routeId)?.targets[0]?.enabled === true,
    'the route target to recover after the channel restore',
  );
  requireValue(
    Boolean(restoredRoute.find((entry) => entry.id === routeId)),
    'the route disappeared',
  );
}

async function deleteAndRestoreModel() {
  await goto('/models');
  const impact = await api(`/configuration/impact?model_id=${modelId}`);
  requireValue(
    impact.routes.some((reference) => reference.route_id === routeId),
    'the model impact does not name the referencing route',
  );
  const conflict = await api(`/models/${modelId}`, 'DELETE', undefined, { expectStatus: 409 });
  requireValue(
    conflict?.code === 'impact_requires_acknowledge',
    'the model delete without acknowledgement did not require an impact review',
  );
  const row = page.locator('tr').filter({ hasText: modelName });
  await row.waitFor({ timeout: 10_000 });
  await row.getByRole('button', { name: L.delete, exact: true }).click();
  await page.waitForTimeout(250);
  await shot('22-model-impact', false);
  await confirmImpactDialog(L.delete);
  await waitForApi(
    () => api('/models?include_deleted=true'),
    (list) => list.some((entry) => entry.id === modelId && entry.deleted_at),
    'the model to be soft deleted',
  );
  const degraded = await api(`/projects/${projectId}/routes`);
  requireValue(
    degraded.find((entry) => entry.id === routeId).targets[0].enabled === false,
    'the route target was not disabled with the model',
  );

  await pick(page, L.deletion, L.showDeleted);
  await row.waitFor({ timeout: 10_000 });
  await shot('23-model-deleted', false);
  await row.getByRole('button', { name: L.restore, exact: true }).click();
  await waitForApi(
    () => api('/models'),
    (list) => list.some((entry) => entry.id === modelId),
    'the model to be restored',
  );
  await waitForApi(
    () => api(`/projects/${projectId}/routes`),
    (list) => list.find((entry) => entry.id === routeId)?.targets[0]?.enabled === true,
    'the route target to recover after the model restore',
  );
}

async function deleteAndRestoreRoute() {
  await goto(`/projects/${projectId}/routes`);
  const row = page.locator('tr').filter({ hasText: routeName });
  await row.waitFor({ timeout: 10_000 });
  await row.getByRole('button', { name: L.delete, exact: true }).click();
  await shot('24-route-delete', false);
  await confirmDialog(L.delete);
  await waitForApi(
    () => api(`/projects/${projectId}/routes?include_deleted=true`),
    (list) => list.some((entry) => entry.id === routeId && entry.deleted_at),
    'the route to be soft deleted',
  );
  await pick(page, L.deletion, L.showDeleted);
  await row.waitFor({ timeout: 10_000 });
  await shot('25-route-deleted', false);
  await row.getByRole('button', { name: L.restore, exact: true }).click();
  await waitForApi(
    () => api(`/projects/${projectId}/routes`),
    (list) => list.some((entry) => entry.id === routeId && !entry.deleted_at),
    'the route to be restored',
  );
}

async function savePolicy() {
  await goto(`/projects/${projectId}/policies`);
  await shot('30-policies', false);
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
  await page.getByLabel(L.policyDocument).fill(JSON.stringify(document, null, 2));
  await page.getByRole('button', { name: L.check, exact: true }).click();
  await expectText(L.policyValid);
  await page.getByRole('button', { name: L.saveAndActivate, exact: true }).click();
  await expectText(L.policyActive);
  const active = await waitForApi(
    () => api(`/projects/${projectId}/policies/active`),
    (version) => (version.document.rules ?? []).length === 1,
    'the saved policy to become active',
  );
  await shot('31-policy-saved', false);
  const first = page.locator('li').filter({ has: page.getByText('v1', { exact: true }) });
  await first.getByRole('button', { name: L.restore, exact: true }).click();
  await expectText(L.policyRestored);
  await waitForApi(
    () => api(`/projects/${projectId}/policies/active`),
    (version) => (version.document.rules ?? []).length === 0,
    'the earlier policy version to become active',
  );
  requireValue(active.version > 1, 'the saved policy did not create a new version');
}

async function createRedactionRule() {
  await goto(`/projects/${projectId}/redaction`);
  await shot('32-redaction', false);
  await page.getByRole('button', { name: L.newRule, exact: true }).click();
  const form = await openDialog();
  await pick(form, L.type, L.regularExpression);
  await form.getByLabel(L.pattern, { exact: true }).fill('ui-[0-9]+');
  await form.getByLabel(L.label, { exact: true }).fill(ruleLabel);
  await form.getByLabel(L.sampleText, { exact: true }).fill('prefix ui-123 suffix');
  await form.getByText('1 match', { exact: true }).waitFor({ timeout: 10_000 });
  await shot('33-rule-dialog', false);
  await form.getByRole('button', { name: L.create, exact: true }).click();
  await form.waitFor({ state: 'hidden', timeout: 20_000 });
  const rules = await waitForApi(
    () => api(`/projects/${projectId}/rules`),
    (list) => list.some((entry) => entry.label === ruleLabel),
    'the redaction rule to be listed',
  );
  ruleId = rules.find((entry) => entry.label === ruleLabel).id;

  await pick(page, L.ruleTest, ruleLabel);
  await page.getByLabel(L.sampleText, { exact: true }).fill('prefix ui-777 suffix');
  await page.getByText('1 match', { exact: true }).waitFor({ timeout: 10_000 });
  await shot('34-rule-tested', false);

  const editedLabel = `UI_ED_${stamp}`;
  const row = page.locator('tr').filter({ hasText: ruleLabel });
  await row.getByRole('button', { name: L.edit, exact: true }).click();
  const edit = await openDialog();
  await edit.getByLabel(L.label, { exact: true }).fill(editedLabel);
  await edit.getByRole('button', { name: L.save, exact: true }).click();
  await edit.waitFor({ state: 'hidden', timeout: 20_000 });
  await waitForApi(
    () => api(`/projects/${projectId}/rules`),
    (list) => list.some((entry) => entry.label === editedLabel),
    'the edited redaction rule to be listed',
  );
  const edited = page.locator('tr').filter({ hasText: editedLabel });
  await edited.getByRole('button', { name: L.delete, exact: true }).click();
  await confirmDialog(L.delete);
  await waitForApi(
    () => api(`/projects/${projectId}/rules`),
    (list) => !list.some((entry) => entry.id === ruleId),
    'the redaction rule to be deleted',
  );
}

async function createUser() {
  username = `ui${stamp}`;
  await goto('/users');
  await shot('40-users', false);
  await page.getByRole('button', { name: L.newUser, exact: true }).click();
  const form = await openDialog();
  await form.getByLabel(L.username, { exact: true }).fill(username);
  await form.getByRole('button', { name: L.create, exact: true }).click();
  const secret = await openDialog();
  await secret.locator('input[readonly]').waitFor({ timeout: 10_000 });
  temporaryPassword = await secret.locator('input[readonly]').inputValue();
  requireValue(temporaryPassword.length > 0, 'the new user has no temporary password');
  await page.screenshot({ path: `${shots}/41-user-secret.png` });
  await secret.getByRole('button', { name: L.secretStored, exact: true }).click();
  await secret.waitFor({ state: 'hidden', timeout: 10_000 });
  const users = await waitForApi(
    () => api('/users'),
    (list) => list.some((entry) => entry.username === username),
    'the new user to be listed',
  );
  const created = users.find((entry) => entry.username === username);
  userId = created.id;
  requireValue(
    created.must_change_password === true,
    'the new user does not need a password change',
  );

  const actions = page.getByRole('button', { name: `Actions: ${username}`, exact: true });
  await actions.click();
  await page.getByRole('menuitem', { name: 'Disable', exact: true }).click();
  await waitForApi(
    () => api('/users'),
    (list) => list.find((entry) => entry.id === userId)?.disabled === true,
    'the user to be disabled',
  );
  await actions.click();
  await page.getByRole('menuitem', { name: 'Enable', exact: true }).click();
  await waitForApi(
    () => api('/users'),
    (list) => list.find((entry) => entry.id === userId)?.disabled === false,
    'the user to be enabled',
  );
}

async function forcedPasswordChange() {
  const forcedContext = await browser.newContext({
    viewport: { width: 1280, height: 900 },
    colorScheme: 'light',
    locale: 'en-US',
  });
  const forcedPage = await forcedContext.newPage();
  watch(forcedPage, 'forced-change ');
  const forcedPassword = `UiPass${stamp}X9`;
  await forcedPage.goto(`${consoleBase}/login`, { waitUntil: 'domcontentloaded' });
  await forcedPage.getByLabel(L.username).fill(username);
  await forcedPage.getByLabel(L.password).fill(temporaryPassword);
  await forcedPage.getByRole('button', { name: L.signIn, exact: true }).click();
  await forcedPage.waitForURL((url) => url.pathname.endsWith('/password'), { timeout: 30_000 });
  await completePasswordChangeOn(forcedPage, temporaryPassword, forcedPassword);
  await forcedPage.waitForSelector('main h1', { timeout: 20_000 });
  await forcedPage.screenshot({ path: `${shots}/42-forced-password-change.png` });
  await forcedContext.close();
}

async function members() {
  await goto(`/projects/${projectId}/members`);
  await shot('43-members', false);
  await page.getByRole('button', { name: L.addMember, exact: true }).first().click();
  const form = await openDialog();
  await pick(form, L.user, username);
  await form.getByRole('button', { name: L.addMember, exact: true }).click();
  await form.waitFor({ state: 'hidden', timeout: 20_000 });
  await waitForApi(
    () => api(`/projects/${projectId}/members`),
    (list) => list.some((entry) => entry.user_id === userId),
    'the member to be added',
  );
  const row = page.locator('tr').filter({ hasText: username });
  await row.waitFor({ timeout: 10_000 });
  await row.getByRole('button', { name: L.remove, exact: true }).click();
  await confirmDialog(L.remove);
  await waitForApi(
    () => api(`/projects/${projectId}/members`),
    (list) => !list.some((entry) => entry.user_id === userId),
    'the member to be removed',
  );
}

async function saveDefaultRoute() {
  await goto(`/projects/${projectId}/settings`);
  await pick(page, L.defaultRoute, routeName);
  await page.getByRole('button', { name: L.save, exact: true }).first().click();
  await expectText(L.changesSaved);
  await waitForApi(
    () => api(`/projects/${projectId}`),
    (project) => project.default_route_id === routeId,
    'the default route to persist',
  );
}

async function callFilters() {
  await goto(`/projects/${labProjectId}/calls`);
  await shot('50-calls', false);
  const filters = [
    [L.state, L.completed, 'state', 'completed'],
    [L.minimumSeverity, L.highSeverity, 'min_severity', 'high'],
    [L.lane, L.main, 'lane', 'main'],
    [L.operation, 'Generation', 'operation', 'generation'],
    [L.purpose, 'Chat', 'purpose', 'chat'],
    [L.outcome, L.succeeded, 'outcome', 'ok'],
  ];
  for (const [label, option, parameter, expected] of filters) {
    await pick(page, label, option);
    await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
    const value = new URL(page.url()).searchParams.get(parameter);
    requireValue(value === expected, `the ${label} filter did not reach the URL`);
  }
  const list = page.getByRole('list', { name: 'Call list' });
  const filtered = (await list.count()) > 0 || (await bodyText()).includes(L.noResults);
  requireValue(filtered, 'the filtered call list shows neither calls nor the empty result');
  await shot('51-call-filters', false);
  await page.getByRole('button', { name: L.clearFilters, exact: true }).click();
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
  requireValue(
    !new URL(page.url()).searchParams.has('state'),
    'clear filters kept the state filter',
  );

  const auxiliary = page.getByRole('switch', { name: L.showAuxiliary });
  await auxiliary.click();
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
  requireValue(
    new URL(page.url()).searchParams.get('include_auxiliary') === 'true',
    'the auxiliary toggle did not reach the URL',
  );
  await auxiliary.click();
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
  requireValue(
    !new URL(page.url()).searchParams.has('include_auxiliary'),
    'the auxiliary toggle did not clear',
  );
}

async function callDetail() {
  let detail = null;
  for (const call of labCalls.slice(0, 12)) {
    const loaded = await api(`/projects/${labProjectId}/calls/${call.id}`);
    if (!detail) detail = loaded;
    if (loaded.call.session_id && loaded.hits.length > 0) {
      detail = loaded;
      break;
    }
  }
  requireValue(detail !== null, 'no call detail could be loaded');
  await goto(`/projects/${labProjectId}/calls?call=${detail.call.id}`);
  const tab = (name) => page.getByRole('tab', { name });
  await shot('52-call-detail', false);

  await tab(/^Transcript$/).click();
  await page.waitForTimeout(200);
  const view = page.getByRole('group', { name: L.transcriptText });
  await view.getByRole('button', { name: L.raw, exact: true }).click();
  requireValue(
    (await view.getByRole('button', { name: L.raw, exact: true }).getAttribute('aria-pressed')) ===
      'true',
    'the raw transcript view was not selected',
  );
  await shot('53-call-transcript', false);
  await view.getByRole('button', { name: L.shown, exact: true }).click();

  await tab(/^Upstream attempts$/).click();
  await page.waitForTimeout(200);
  if (detail.attempts.length > 0) {
    await page.getByText('Attempt 1', { exact: true }).first().waitFor({ timeout: 10_000 });
  } else {
    await expectText('No upstream attempt was recorded.');
  }
  await shot('54-call-attempts', false);

  await tab(/^Redacted values/).click();
  await page.waitForTimeout(200);
  if (detail.hits.length > 0) {
    await expectText(detail.hits[0].label);
  } else {
    await expectText(L.noRedactions);
  }
  await shot('55-call-redactions', false);
}

async function conversations() {
  const conversationPage = await api(`/projects/${labProjectId}/conversations?limit=100`);
  const conversation =
    conversationPage.items.find((entry) => entry.calls > 1) ?? conversationPage.items[0];
  if (!conversation) {
    console.log(
      'traffic: calls exist but no conversation was linked; skipping the trajectory check',
    );
    await goto(`/projects/${labProjectId}/conversations`);
    await expectText(L.noConversations);
    await shot('56-conversations-empty');
    return;
  }
  const trajectory = await api(
    `/projects/${labProjectId}/conversations/${conversation.id}/trajectory`,
  );
  const redactionIndex = await api(
    `/projects/${labProjectId}/conversations/${conversation.id}/redactions`,
  );
  await goto(`/projects/${labProjectId}/conversations/${conversation.id}`);
  await page.getByRole('group', { name: L.graphView }).waitFor({ timeout: 15_000 });
  await page.waitForTimeout(300);
  await shot('57-trajectory-flow');

  await page
    .getByRole('group', { name: L.view })
    .getByRole('button', { name: L.tree, exact: true })
    .click();
  await page.locator('ul li').first().waitFor({ timeout: 10_000 });
  await shot('58-trajectory-tree');

  await page
    .getByRole('group', { name: L.view })
    .getByRole('button', { name: L.flat, exact: true })
    .click();
  const firstNode = page.locator('ol > li > button').first();
  await firstNode.waitFor({ timeout: 10_000 });
  await firstNode.click();
  await page.waitForTimeout(300);
  requireValue(
    new URL(page.url()).searchParams.get('node') !== null,
    'selecting a trajectory node did not set the node deep link',
  );
  await page.getByRole('button', { name: L.closeInspector }).waitFor({ timeout: 10_000 });
  await shot('59-trajectory-inspector', false);

  const deepLink = page.url();
  await page.goto(deepLink, { waitUntil: 'domcontentloaded' });
  await waitForPage();
  await page.getByRole('button', { name: L.closeInspector }).waitFor({ timeout: 10_000 });

  if (trajectory.steps.length > 0) {
    await page.getByRole('button', { name: L.nextStep }).click();
    await page.getByText(/^Step 1 of \d+$/).waitFor({ timeout: 10_000 });
    await shot('60-trajectory-replay', false);
    await page.getByRole('button', { name: L.previousStep }).click();
  } else {
    await expectText(L.noAuditEvents);
  }

  const chip = page.getByRole('button', { name: /values? · \d+ occurrences/ }).first();
  await chip.click();
  const panel = await openDialog();
  await panel.getByText(L.redactedValues, { exact: true }).first().waitFor({ timeout: 10_000 });
  await shot('61-trajectory-redactions', false);
  if (redactionIndex.values.length > 0) {
    await panel.locator('summary').first().click();
    await panel.getByRole('button', { name: L.showInOriginal, exact: true }).first().click();
    await page.waitForURL((url) => (url.searchParams.get('node') ?? '').startsWith('call:'), {
      timeout: 15_000,
    });
    await expectText('Metrics');
  } else {
    console.log('traffic: the conversation has no redacted values; skipping show-in-original');
    await expectText(L.noRedactedValues);
  }
}

async function riskAndOverview() {
  await goto(`/projects/${labProjectId}/risk`);
  const riskText = await bodyText();
  requireValue(
    riskText.includes(L.riskSources) || riskText.includes(L.noFindings),
    'the risk page shows neither findings nor an empty state',
  );
  await shot('62-risk', false);
  await goto(`/projects/${labProjectId}/overview`);
  const overviewText = await bodyText();
  requireValue(
    overviewText.includes(L.requestsMainLane) || overviewText.includes(L.noData),
    'the overview shows neither counters nor an empty state',
  );
  await shot('63-overview', false);
  await goto('/monitor');
  const monitorText = await bodyText();
  for (const label of [L.calls, L.failures, L.blocked, L.highFindings]) {
    requireValue(monitorText.includes(label), `the monitor does not show the ${label} counter`);
  }
  requireValue(monitorText.includes(L.mainLaneNote), 'the monitor does not explain the main lane');
  await page
    .getByRole('group', { name: L.timeWindow })
    .getByRole('button', { name: L.last7Days, exact: true })
    .click();
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
  await page
    .getByRole('group', { name: L.timeWindow })
    .getByRole('button', { name: L.last24Hours, exact: true })
    .click();
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
  await shot('64-monitor', false);
}

async function trafficChecks() {
  const callPage = await api(`/projects/${labProjectId}/calls?limit=100`);
  labCalls = callPage.items;
  if (labCalls.length === 0) {
    console.log(
      'traffic: the lab has no calls (no upstream configured); asserting empty states and skipping call, conversation, trajectory and risk checks',
    );
    await step('calls empty state', async () => {
      await goto(`/projects/${labProjectId}/calls`);
      await expectText(L.noCalls);
      await shot('50-calls-empty');
    });
    await step('conversations empty state', async () => {
      await goto(`/projects/${labProjectId}/conversations`);
      await expectText(L.noConversations);
      await shot('51-conversations-empty');
    });
    await step('risk empty state', async () => {
      await goto(`/projects/${labProjectId}/risk`);
      await expectText(L.noFindings);
      await shot('52-risk-empty');
    });
    await step('overview empty state', async () => {
      await goto(`/projects/${labProjectId}/overview`);
      await expectText(L.noData);
      await shot('53-overview-empty');
    });
    await step('monitor counters', async () => {
      await goto('/monitor');
      const text = await bodyText();
      for (const label of [L.calls, L.failures, L.blocked, L.highFindings]) {
        requireValue(text.includes(label), `the monitor does not show the ${label} counter`);
      }
      requireValue(text.includes(L.mainLaneNote), 'the monitor does not explain the main lane');
      await shot('54-monitor');
    });
    return;
  }
  console.log(`traffic: ${labCalls.length} calls are available; running the data-dependent checks`);
  await step('call classification filters', callFilters);
  await step('call detail attempts, redactions and transcript', callDetail);
  await step('conversation trajectory views, replay and redactions', conversations);
  await step('risk, overview and monitor data', riskAndOverview);
}

async function languageAndTheme() {
  await goto('/monitor');
  await page.getByRole('button', { name: L.theme, exact: true }).click();
  await page.getByRole('menuitemradio', { name: L.dark, exact: true }).click();
  await page.waitForTimeout(300);
  requireValue(
    (await page.locator('html').getAttribute('data-theme')) === 'dark',
    'the theme switch did not set the dark theme',
  );
  await shot('70-monitor-dark');
  await page.getByRole('button', { name: L.theme, exact: true }).click();
  await page.getByRole('menuitemradio', { name: L.light, exact: true }).click();
  await page.getByRole('button', { name: L.language, exact: true }).click();
  await shot('71-language-menu', false);
  await page.getByRole('menuitemradio', { name: '简体中文', exact: true }).click();
  await page.waitForTimeout(400);
  requireValue(
    (await page.locator('html').getAttribute('lang')) === 'zh-CN',
    'the language switch did not set the page language',
  );
  await shot('72-monitor-zh');
  await page.getByRole('button', { name: '语言', exact: true }).click();
  await page.getByRole('menuitemradio', { name: 'English', exact: true }).click();
  await page.waitForTimeout(300);
  requireValue(
    (await page.locator('html').getAttribute('lang')) === 'en',
    'the language switch back to English failed',
  );
}

async function mobileCheck() {
  const state = await context.storageState();
  const mobile = await browser.newContext({
    viewport: { width: 390, height: 844 },
    locale: 'zh-CN',
    colorScheme: 'light',
    storageState: state,
  });
  const phone = await mobile.newPage();
  watch(phone, 'mobile ');
  for (const [name, path] of pages) {
    await step(`mobile page ${path}`, async () => {
      const response = await phone.goto(consoleBase + path, { waitUntil: 'domcontentloaded' });
      await phone.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
      await phone.waitForTimeout(150);
      if (!response || response.status() >= 400) throw new Error(`HTTP ${response?.status()}`);
      const overflow = await phone.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      if (overflow > 2) throw new Error(`overflows horizontally by ${overflow}px`);
      await phone.screenshot({ path: `${shots}/m-${name}.png`, fullPage: true });
    });
  }
  await step('mobile navigation', async () => {
    await phone.goto(`${consoleBase}/projects/${labProjectId}/overview`, {
      waitUntil: 'domcontentloaded',
    });
    await phone.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {});
    const menu = phone.getByRole('button', { name: L.openNavigation, exact: true });
    requireValue((await menu.count()) > 0, 'the mobile navigation button is missing');
    await menu.click();
    await phone.waitForTimeout(300);
    await phone.getByRole('navigation', { name: 'Main navigation' }).waitFor({ timeout: 10_000 });
    await phone.screenshot({ path: `${shots}/m-menu.png` });
  });
  await mobile.close();
}

async function cleanup() {
  const attempt = async (description, run) => {
    try {
      await run();
    } catch (error) {
      console.warn(`cleanup skipped ${description}: ${error.message}`);
    }
  };
  if (!providerId || !channelId || !modelId) {
    await attempt('resolve created records', async () => {
      const providers = await api('/providers');
      providerId = providerId || providers.find((entry) => entry.label === providerLabel)?.id || '';
      const channels = await api('/channels');
      channelId = channelId || channels.find((entry) => entry.name === channelName)?.id || '';
      const models = await api('/models');
      modelId = modelId || models.find((entry) => entry.upstream_name === modelName)?.id || '';
    });
  }
  await attempt('project', () => (projectId ? api(`/projects/${projectId}`, 'DELETE') : null));
  await attempt('user', () =>
    userId ? api(`/users/${userId}`, 'PUT', { disabled: true, administrator: false }) : null,
  );
  await attempt('channel', () =>
    channelId ? api(`/channels/${channelId}?acknowledge=true&detach_targets=true`, 'DELETE') : null,
  );
  await attempt('model', () =>
    modelId ? api(`/models/${modelId}?acknowledge=true&detach_targets=true`, 'DELETE') : null,
  );
  await attempt('provider', () => (providerId ? api(`/providers/${providerId}`, 'DELETE') : null));
}

let suiteProjectName = `UI Acceptance ${stamp}`;
let projectId = '';
let providerId = '';
let providerLabel = `UI Provider ${stamp}`;
let channelId = '';
let channelName = `UI Channel ${stamp}`;
let modelId = '';
let modelName = `ui-model-${stamp}`;
let routeId = '';
let routeName = `ui-route-${stamp}`;
let routeAlias = `ui-alias-${stamp}`;
let keyLabel = `ui-key-${stamp}`;
let agentKeyId = '';
let ruleLabel = `UI_RULE_${stamp}`;
let ruleId = '';
let username = '';
let userId = '';
let temporaryPassword = '';
let labCalls = [];

recording = true;
try {
  await step('sign in', signIn);
  if (failures.length > 0) throw new Error('sign-in failed; the remaining flows need a session');
  await step('project create', createProject);
  if (!projectId) throw new Error('project creation failed; the remaining flows need a project');
  await step('project switching', switchProjects);
  await step('project settings', saveProjectSettings);
  await step('provider connect wizard', connectProvider);
  if (!channelId || !modelId || !routeId) {
    throw new Error('provider connect failed; the remaining flows need the provider graph');
  }
  await step('route alias', addRouteAlias);
  await step('agent key', createAgentKey);
  await step('page sweep', sweepPages);
  await step('channel soft delete and restore', deleteAndRestoreChannel);
  await step('model soft delete and restore', deleteAndRestoreModel);
  await step('route soft delete and restore', deleteAndRestoreRoute);
  await step('policy save and rollback', savePolicy);
  await step('redaction rule create, tester, edit and delete', createRedactionRule);
  await step('user create, disable and enable', createUser);
  await step('forced password change', forcedPasswordChange);
  await step('member add and remove', members);
  await step('default route', saveDefaultRoute);
  await step('traffic-dependent checks', trafficChecks);
  await step('language and theme', languageAndTheme);
  await step('mobile 390px', mobileCheck);
} catch (error) {
  failures.push(`run: ${error.message}`);
} finally {
  recording = false;
  await cleanup();
  const report = {
    stamp,
    base,
    labProject: labProjectId,
    projectId,
    providerId,
    channelId,
    modelId,
    routeId,
    agentKeyId,
    ruleId,
    userId,
    traffic: labCalls.length,
    pages: pages.map(([name, path]) => ({ name, path })),
    failures,
  };
  fs.writeFileSync(`${shots}/report.json`, JSON.stringify(report, null, 2));
  await browser.close();
}

if (failures.length > 0) {
  console.error(`FAILED (${failures.length})\n${failures.join('\n')}`);
  process.exit(1);
}
console.log(
  `console acceptance passed (${pages.length} pages at desktop and mobile width, ${labCalls.length} traffic calls)`,
);
