import assert from 'node:assert/strict';
import test from 'node:test';
import { filtersOf, validateCallsSearch } from '../src/features/calls/search.ts';
import { shortId } from '../src/lib/format.ts';

test('call filters accept only known values and keep selectors', () => {
  const parsed = validateCallsSearch({
    state: 'completed',
    min_severity: 'high',
    lane: 'main',
    operation: 'generation',
    purpose: 'chat',
    outcome: 'ok',
    model: 'gpt-5',
    agent_key_id: 'key_1',
    call: 'call_1',
  });
  assert.equal(parsed.state, 'completed');
  assert.equal(parsed.min_severity, 'high');
  assert.equal(parsed.lane, 'main');
  assert.equal(parsed.operation, 'generation');
  assert.equal(parsed.purpose, 'chat');
  assert.equal(parsed.outcome, 'ok');
  assert.equal(parsed.model, 'gpt-5');
  assert.equal(parsed.agent_key_id, 'key_1');
  assert.equal(parsed.call, 'call_1');
  const invalid = validateCallsSearch({ state: 'not-a-state', purpose: 'not-a-purpose' });
  assert.equal(invalid.state, undefined);
  assert.equal(invalid.purpose, undefined);
});

test('the selected call is not part of the list filters', () => {
  assert.deepEqual(filtersOf({ state: 'failed', call: 'call_1' }), { state: 'failed' });
});

test('short identifiers are bounded', () => {
  assert.equal(shortId('short'), 'short');
  assert.ok(shortId('trj_abcdefghijklmnopqrstuvwxyz').includes('…'));
});
