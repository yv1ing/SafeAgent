import assert from 'node:assert/strict';
import test from 'node:test';
import { splitBySpanSets } from '../src/lib/highlight.ts';

test('ranges are UTF-8 byte offsets', () => {
  assert.deepEqual(splitBySpanSets('邮箱 a@b.c 结束', [[{ start: 7, end: 12 }]]), [
    { text: '邮箱 ', spans: [] },
    { text: 'a@b.c', spans: [{ set: 0, range: 0 }] },
    { text: ' 结束', spans: [] },
  ]);
});

test('overlapping sets report every active span', () => {
  assert.deepEqual(splitBySpanSets('abcdef', [[{ start: 1, end: 4 }], [{ start: 2, end: 5 }]]), [
    { text: 'a', spans: [] },
    { text: 'b', spans: [{ set: 0, range: 0 }] },
    {
      text: 'cd',
      spans: [
        { set: 0, range: 0 },
        { set: 1, range: 0 },
      ],
    },
    { text: 'e', spans: [{ set: 1, range: 0 }] },
    { text: 'f', spans: [] },
  ]);
});

test('out-of-range and inverted ranges are dropped', () => {
  assert.deepEqual(splitBySpanSets('abcdef', [[{ start: 9, end: 12 }], [{ start: 4, end: 2 }]]), [
    { text: 'abcdef', spans: [] },
  ]);
});

test('a range that splits a code point still decodes whole characters', () => {
  assert.deepEqual(splitBySpanSets('é', [[{ start: 1, end: 2 }]]), [
    { text: 'é', spans: [{ set: 0, range: 0 }] },
  ]);
});

test('empty text has no segments', () => {
  assert.deepEqual(splitBySpanSets('', [[{ start: 0, end: 1 }]]), []);
});
