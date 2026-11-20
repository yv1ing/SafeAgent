import assert from 'node:assert/strict';
import test from 'node:test';
import {
  formatCost,
  formatDuration,
  formatLimit,
  formatPercent,
  formatTokens,
  severityTone,
  shortId,
  stateTone,
} from '../src/lib/format.ts';

test('tokens are compacted', () => {
  assert.equal(formatTokens(999), '999');
  assert.equal(formatTokens(1500), '1.5k');
  assert.equal(formatTokens(2_500_000), '2.5M');
});

test('costs use the channel currency', () => {
  assert.equal(formatCost(15_000), '$0.0150');
  assert.equal(formatCost(15_000, ''), '$0.0150');
  assert.equal(formatCost(15_000, 'CNY'), '¥0.0150');
  assert.equal(formatCost(15_000, 'GBP'), '£0.0150');
  assert.equal(formatCost(15_000, 'CHF'), '0.0150 CHF');
  assert.equal(formatCost(null), '—');
});

test('limits treat zero as unlimited', () => {
  assert.equal(formatLimit(0, 'Unlimited'), 'Unlimited');
  assert.equal(formatLimit(undefined, 'Unlimited'), 'Unlimited');
  assert.equal(formatLimit(1200, 'Unlimited'), '1,200');
});

test('durations switch to seconds', () => {
  assert.equal(formatDuration(420), '420 ms');
  assert.equal(formatDuration(2500), '2.5 s');
  assert.equal(formatDuration(0), '—');
});

test('percentages guard against an empty whole', () => {
  assert.equal(formatPercent(1, 4), '25.0%');
  assert.equal(formatPercent(1, 0), '—');
});

test('long identifiers are shortened', () => {
  assert.equal(shortId('abc'), 'abc');
  assert.equal(shortId('0123456789abcdef'), '0123456789ab…');
});

test('states and severities map to badge tones', () => {
  assert.equal(stateTone('completed'), 'success');
  assert.equal(stateTone('blocked'), 'warning');
  assert.equal(stateTone('failed'), 'danger');
  assert.equal(stateTone('rejected'), 'danger');
  assert.equal(stateTone(undefined), 'neutral');
  assert.equal(severityTone('high'), 'danger');
  assert.equal(severityTone(''), 'neutral');
});
