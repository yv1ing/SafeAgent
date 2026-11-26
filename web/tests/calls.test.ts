import assert from 'node:assert/strict';
import test from 'node:test';
import {
  filtersOf,
  hasFilters,
  listParams,
  validateCallsSearch,
} from '../src/features/calls/search.ts';
import { validateTrajectorySearch } from '../src/features/conversations/search.ts';

test('call search drops unknown enum values and empty strings', () => {
  assert.deepEqual(
    validateCallsSearch({
      state: 'completed',
      lane: 'side',
      operation: 'generation',
      purpose: 'subtitle',
      outcome: 'nope',
      model: '',
      call: 'call-1',
    }),
    {
      state: 'completed',
      min_severity: undefined,
      model: undefined,
      agent_key_id: undefined,
      session_id: undefined,
      lane: 'side',
      operation: 'generation',
      purpose: undefined,
      outcome: undefined,
      include_auxiliary: undefined,
      call: 'call-1',
    },
  );
});

test('call search accepts the auxiliary flag from a URL', () => {
  assert.equal(validateCallsSearch({ include_auxiliary: 'true' }).include_auxiliary, true);
  assert.equal(validateCallsSearch({ include_auxiliary: 'false' }).include_auxiliary, undefined);
  assert.equal(validateCallsSearch({ include_auxiliary: true }).include_auxiliary, true);
});

test('filters split the selected call from the list query', () => {
  const filters = filtersOf({ state: 'failed', call: 'call-1' });
  assert.deepEqual(filters, { state: 'failed' });
  assert.equal(hasFilters(filters), true);
  assert.equal(hasFilters({}), false);
});

test('an explicit lane wins over the auxiliary toggle', () => {
  assert.deepEqual(listParams({ lane: 'auxiliary', include_auxiliary: true, state: 'completed' }), {
    lane: 'auxiliary',
    include_auxiliary: undefined,
    state: 'completed',
  });
  assert.deepEqual(listParams({ include_auxiliary: true }), { include_auxiliary: true });
});

test('trajectory search keeps only a non-empty node id', () => {
  assert.deepEqual(validateTrajectorySearch({ node: 'call:a' }), { node: 'call:a' });
  assert.deepEqual(validateTrajectorySearch({ node: '' }), { node: undefined });
  assert.deepEqual(validateTrajectorySearch({}), { node: undefined });
});
