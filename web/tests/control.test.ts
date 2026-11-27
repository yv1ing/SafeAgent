import assert from 'node:assert/strict';
import test from 'node:test';
import type {
  ConsoleChannelOptionView,
  ConsoleChannelView,
  ConsoleImpactView,
  ConsoleRouteTargetView,
  ConsoleRouteView,
} from '../src/api/generated/models/index.ts';
import { channelInput } from '../src/features/channels/channel.ts';
import { groupModelsByProvider, modelBatchInputs } from '../src/features/models/models.ts';
import {
  catalogNeedsHost,
  discoveryFormats,
  endpointHosts,
  formatsForKind,
  parseEndpointCatalog,
  providerFormats,
  validateEndpointCatalog,
  type EndpointCatalog,
} from '../src/features/providers/endpoints.ts';
import {
  impactReport,
  projectNameMap,
  replacementOptions,
} from '../src/features/providers/impact.ts';
import {
  manualAliases,
  rewireTargets,
  routeTargetsInput,
  validateTargets,
  type DraftTarget,
} from '../src/features/routes/targets.ts';

const catalog: EndpointCatalog = {
  anthropic_messages: {
    Format: 'anthropic_messages',
    BaseURL: 'https://api.anthropic.com',
    Path: '/v1/messages',
    Discovery: { Path: '/v1/models', Shape: 'anthropic' },
    Auxiliary: [{ Kind: 'moderation', Path: '/v1/moderations' }],
  },
  openai_chat: {
    Format: 'openai_chat',
    Path: '/chat/completions',
    Auth: 'bearer',
    Discovery: { Path: '/models', Shape: 'openai' },
    Auxiliary: [{ Kind: 'embedding', Path: '/embeddings' }],
  },
  openai_responses: {
    Format: 'openai_responses',
    Path: '/responses',
  },
};

test('the endpoint catalog round-trips from an object or its JSON text', () => {
  assert.deepEqual(parseEndpointCatalog(catalog), catalog);
  assert.deepEqual(parseEndpointCatalog(JSON.stringify(catalog)), catalog);
  assert.equal(parseEndpointCatalog('not json'), null);
  assert.equal(parseEndpointCatalog([]), null);
  assert.equal(parseEndpointCatalog({}), null);
  assert.equal(parseEndpointCatalog({ openai_chat: 'nope' }), null);
});

test('provider formats keep the canonical protocol order', () => {
  assert.deepEqual(providerFormats(catalog), [
    'openai_chat',
    'openai_responses',
    'anthropic_messages',
  ]);
});

test('model kinds derive formats from the declared auxiliary endpoints', () => {
  assert.deepEqual(formatsForKind(catalog, 'generation'), [
    'openai_chat',
    'openai_responses',
    'anthropic_messages',
  ]);
  assert.deepEqual(formatsForKind(catalog, 'embedding'), ['openai_chat']);
  assert.deepEqual(formatsForKind(catalog, 'moderation'), ['anthropic_messages']);
  assert.deepEqual(formatsForKind(catalog, 'unknown'), []);
});

test('only endpoints with a discovery path are discoverable', () => {
  assert.deepEqual(discoveryFormats(catalog), ['openai_chat', 'anthropic_messages']);
});

test('a catalog needs a channel host when any endpoint declares none', () => {
  assert.equal(catalogNeedsHost(catalog), true);
  assert.equal(
    catalogNeedsHost({ openai_chat: { BaseURL: 'https://api.example.com', Path: '/v1' } }),
    false,
  );
  assert.deepEqual(endpointHosts(catalog), ['https://api.anthropic.com']);
});

test('the endpoint editor rejects a malformed catalog', () => {
  assert.deepEqual(validateEndpointCatalog('{'), { problem: 'json' });
  assert.deepEqual(validateEndpointCatalog('[]'), { problem: 'catalog' });
  assert.deepEqual(validateEndpointCatalog('{}'), { problem: 'empty' });
  assert.deepEqual(validateEndpointCatalog('{" ": {}}'), { problem: 'format' });
  assert.deepEqual(validateEndpointCatalog('{"openai_chat": "x"}'), { problem: 'endpoint' });
  assert.deepEqual(
    validateEndpointCatalog('{"openai_chat":{"Format":"openai_responses","Path":"/chat"}}'),
    { problem: 'format' },
  );
  assert.deepEqual(
    validateEndpointCatalog('{"openai_chat":{"BaseURL":"ftp://example.com","Path":"/chat"}}'),
    { problem: 'endpoint' },
  );
  assert.deepEqual(
    validateEndpointCatalog('{"openai_chat":{"BaseURL":"https://user@example.com"}}'),
    { problem: 'endpoint' },
  );
  assert.deepEqual(validateEndpointCatalog('{"openai_chat":{"Path":"chat"}}'), {
    problem: 'path',
  });
  assert.deepEqual(validateEndpointCatalog(JSON.stringify(catalog)), { catalog });
});

const impact: ConsoleImpactView = {
  routes: [
    {
      route_id: 'route-1',
      route_name: 'gpt-5',
      project_id: 'project-1',
      target_count: 2,
    },
    {
      route_id: 'route-2',
      route_name: 'claude',
      project_id: 'project-missing',
      target_count: 1,
    },
  ],
  projects: ['project-1', 'project-missing'],
};

test('an impact report names routes and projects, falling back to the id', () => {
  const report = impactReport(impact, projectNameMap([{ id: 'project-1', name: 'Assistants' }]), [
    { id: 'channel-2', label: 'backup' },
  ]);
  assert.deepEqual(report.routes, ['gpt-5 (Assistants)', 'claude (project-missing)']);
  assert.deepEqual(report.projects, ['Assistants', 'project-missing']);
  assert.deepEqual(report.replacements, ['backup']);
  assert.equal(report.liveTraffic, 0);
});

test('replacement candidates exclude the record being removed', () => {
  const items = [
    { id: 'channel-1', name: 'primary' },
    { id: 'channel-2', name: 'backup' },
  ];
  assert.deepEqual(
    replacementOptions(items, 'channel-1', (item) => item.name),
    [{ id: 'channel-2', label: 'backup' }],
  );
});

const channels: ConsoleChannelOptionView[] = [
  {
    id: 'channel-1',
    name: 'primary',
    provider_id: 'openai',
    provider_label: 'OpenAI',
    formats: ['openai_chat'],
    models: [
      {
        id: 'model-1',
        upstream_name: 'gpt-5',
        display_name: 'GPT-5',
        kind: 'generation',
        formats: ['openai_chat', 'openai_responses'],
      },
      {
        id: 'model-embed',
        upstream_name: 'text-embedding',
        kind: 'embedding',
        formats: ['openai_chat'],
      },
    ],
  },
  {
    id: 'channel-2',
    name: 'backup',
    provider_id: 'openai',
    provider_label: 'OpenAI',
    formats: ['openai_chat'],
    models: [
      {
        id: 'model-1',
        upstream_name: 'gpt-5',
        display_name: 'GPT-5',
        kind: 'generation',
        formats: ['openai_chat'],
      },
    ],
  },
];

const target = (
  channelId: string,
  modelId: string,
  patch: Partial<ConsoleRouteTargetView> = {},
): ConsoleRouteTargetView => ({
  channel_id: channelId,
  model_id: modelId,
  priority: 0,
  weight: 1,
  enabled: true,
  ...patch,
});

test('target rows require one kind, valid formats and no duplicate', () => {
  const draft: DraftTarget[] = [
    { channel_id: 'channel-1', model_id: 'model-1', priority: 0, weight: 1 },
  ];
  assert.equal(validateTargets(draft, channels), null);
  assert.equal(validateTargets([], channels), 'empty');
  assert.equal(
    validateTargets([{ channel_id: '', model_id: '', priority: 0, weight: 1 }], channels),
    'incomplete',
  );
  assert.equal(
    validateTargets(
      [{ channel_id: 'channel-missing', model_id: 'model-1', priority: 0, weight: 1 }],
      channels,
    ),
    'provider_mismatch',
  );
  assert.equal(
    validateTargets(
      [
        { channel_id: 'channel-1', model_id: 'model-1', priority: 0, weight: 1 },
        { channel_id: 'channel-1', model_id: 'model-1', priority: 1, weight: 1 },
      ],
      channels,
    ),
    'duplicate',
  );
  assert.equal(
    validateTargets(
      [
        { channel_id: 'channel-1', model_id: 'model-1', priority: 0, weight: 1 },
        { channel_id: 'channel-1', model_id: 'model-embed', priority: 1, weight: 1 },
      ],
      channels,
    ),
    'kind_mismatch',
  );
  assert.equal(
    validateTargets(
      [
        {
          channel_id: 'channel-1',
          model_id: 'model-embed',
          format: 'openai_responses',
          priority: 0,
          weight: 1,
        },
      ],
      channels,
    ),
    'format_mismatch',
  );
});

test('rewiring a channel replaces it and drops a target that becomes a duplicate', () => {
  const targets = [
    target('channel-1', 'model-1', { format: 'openai_chat' }),
    target('channel-2', 'model-1', { format: 'openai_chat' }),
  ];
  const rewired = rewireTargets(targets, 'channel_id', 'channel-1', 'channel-2');
  assert.deepEqual(rewired, [
    {
      channel_id: 'channel-2',
      model_id: 'model-1',
      format: 'openai_chat',
      priority: 0,
      weight: 1,
      max_output_tokens: undefined,
      enabled: true,
    },
  ]);
});

test('rewiring a model keeps the channel and the output limit', () => {
  const rewired = rewireTargets(
    [target('channel-1', 'model-1', { max_output_tokens: 256 })],
    'model_id',
    'model-1',
    'model-2',
  );
  assert.equal(rewired[0]?.model_id, 'model-2');
  assert.equal(rewired[0]?.channel_id, 'channel-1');
  assert.equal(rewired[0]?.max_output_tokens, 256);
});

test('manual aliases omit renamed ones', () => {
  const route = {
    aliases: [
      { alias: 'gpt-5', source: 'renamed' },
      { alias: 'latest', source: 'manual' },
    ],
    targets: [target('channel-1', 'model-1')],
  } as ConsoleRouteView;
  assert.deepEqual(manualAliases(route), ['latest']);
  assert.deepEqual(routeTargetsInput(route), [
    {
      channel_id: 'channel-1',
      model_id: 'model-1',
      format: undefined,
      priority: 0,
      weight: 1,
      max_output_tokens: undefined,
      enabled: true,
    },
  ]);
});

test('a channel round trip keeps the version and echoes masked headers', () => {
  const view = {
    id: 'channel-1',
    name: 'primary',
    provider_id: 'openai',
    provider_label: 'OpenAI',
    headers: { Authorization: '****' },
    forward_headers: ['x-trace'],
    default_max_output_tokens: 128,
    timeout_seconds: 60,
    first_byte_timeout_seconds: 30,
    allow_private_network: true,
    enabled: false,
    route_count: 2,
    version: 7,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  } as ConsoleChannelView;
  assert.deepEqual(channelInput(view), {
    name: 'primary',
    provider_id: 'openai',
    base_url: '',
    headers: { Authorization: '****' },
    forward_headers: ['x-trace'],
    default_max_output_tokens: 128,
    timeout_seconds: 60,
    first_byte_timeout_seconds: 30,
    allow_private_network: true,
    enabled: false,
    version: 7,
  });
});

test('models group by provider label and fall back to the provider id', () => {
  const groups = groupModelsByProvider(
    [
      {
        id: 'model-1',
        provider_id: 'openai',
        upstream_name: 'gpt-5',
        kind: 'generation',
        currency: 'USD',
        formats: ['openai_chat'],
        version: 1,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
      {
        id: 'model-2',
        provider_id: 'missing',
        upstream_name: 'local',
        kind: 'generation',
        currency: 'USD',
        formats: [],
        version: 1,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
    ],
    new Map([['openai', 'OpenAI']]),
  );
  assert.deepEqual(
    groups.map((group) => [group.providerId, group.label, group.models.length]),
    [
      ['missing', 'missing', 1],
      ['openai', 'OpenAI', 1],
    ],
  );
});

test('batch model inputs share the selected kind and currency', () => {
  assert.deepEqual(
    modelBatchInputs('openai', ['a', 'b'], {
      kind: 'embedding',
      currency: 'CNY',
      enabled: false,
    }),
    [
      {
        provider_id: 'openai',
        upstream_name: 'a',
        kind: 'embedding',
        currency: 'CNY',
        enabled: false,
      },
      {
        provider_id: 'openai',
        upstream_name: 'b',
        kind: 'embedding',
        currency: 'CNY',
        enabled: false,
      },
    ],
  );
});
