import assert from 'node:assert/strict';
import test from 'node:test';
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
