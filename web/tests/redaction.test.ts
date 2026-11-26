import assert from 'node:assert/strict';
import test from 'node:test';
import type {
  ConsoleRedactionLocationView,
  ConsoleRedactionValueView,
} from '../src/api/generated/models/index.ts';
import {
  filteredValues,
  groupOccurrences,
  itemNodeId,
  redactionSummary,
  sortedValues,
} from '../src/features/redaction/redaction.ts';

const location = (callId: string, ordinal: number) =>
  ({
    call_id: callId,
    call_created_at: '2026-01-01T00:00:00Z',
    section: 'request',
    ordinal,
    path: `messages[${ordinal}]`,
    start: 0,
    end: 5,
    shown_start: 0,
    shown_end: 6,
  }) as ConsoleRedactionLocationView;

const value = (id: string, label: string, count: number) =>
  ({
    value_id: id,
    label,
    action: 'replace',
    detector: '',
    secret: false,
    count,
    first_call_id: 'a',
    last_call_id: 'a',
    occurrences: [],
  }) as ConsoleRedactionValueView;

test('summary counts distinct values and occurrences separately', () => {
  assert.deepEqual(redactionSummary({ values: [value('1', 'EMAIL', 2)], total: 5 }), {
    values: 1,
    total: 5,
  });
  assert.deepEqual(redactionSummary({ values: [], total: 0 }), { values: 0, total: 0 });
});

test('occurrence anchors reuse the trajectory item identity', () => {
  assert.equal(
    itemNodeId({ call_id: 'call-a', section: 'response', ordinal: 3 }),
    'item:call-a:response:3',
  );
});

test('occurrences group by call without reordering', () => {
  const groups = groupOccurrences([
    location('a', 0),
    location('a', 2),
    location('b', 1),
    location('a', 4),
  ]);
  assert.deepEqual(
    groups.map((group) => [group.callId, group.occurrences.map((entry) => entry.ordinal)]),
    [
      ['a', [0, 2, 4]],
      ['b', [1]],
    ],
  );
});

test('values sort by label then frequency and filter by value id', () => {
  const values = [value('2', 'PHONE', 1), value('1', 'EMAIL', 3), value('3', 'EMAIL', 5)];
  assert.deepEqual(
    sortedValues(values).map((entry) => entry.value_id),
    ['3', '1', '2'],
  );
  assert.deepEqual(
    filteredValues(values, '1').map((entry) => entry.value_id),
    ['1'],
  );
  assert.equal(filteredValues(values, null).length, 3);
});
