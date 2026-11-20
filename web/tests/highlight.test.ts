import assert from 'node:assert/strict';
import test from 'node:test';
import { splitByByteRanges } from '../src/lib/highlight.ts';

test('ranges are UTF-8 byte offsets', () => {
  assert.deepEqual(splitByByteRanges('邮箱 a@b.c 结束', [{ start: 7, end: 12 }]), [
    { text: '邮箱 ' },
    { text: 'a@b.c', range: 0 },
    { text: ' 结束' },
  ]);
});

test('overlapping and out-of-range ranges are clipped', () => {
  assert.deepEqual(
    splitByByteRanges('abcdef', [
      { start: 1, end: 4 },
      { start: 2, end: 5 },
      { start: 9, end: 12 },
    ]),
    [{ text: 'a' }, { text: 'bcd', range: 0 }, { text: 'e', range: 1 }, { text: 'f' }],
  );
});
