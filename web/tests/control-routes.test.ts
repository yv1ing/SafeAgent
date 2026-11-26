import assert from 'node:assert/strict';
import test from 'node:test';
import type {
  ConsoleChannelOptionView,
  ConsoleChannelView,
  ConsoleRouteTargetView,
  ConsoleRouteView,
} from '../src/api/generated/models/index.ts';
import { channelInput } from '../src/features/channels/channel.ts';
import { groupModelsByProvider, modelBatchInputs } from '../src/features/models/models.ts';
import {
  manualAliases,
  rewireTargets,
  routeTargetsInput,
  validateTargets,
  type DraftTarget,
} from '../src/features/routes/targets.ts';

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
