import assert from 'node:assert/strict';
import test from 'node:test';
import { en } from '../src/i18n/messages.ts';
import { zhCN } from '../src/i18n/zh-CN.ts';

function entries(value: object, prefix = ''): [string, unknown][] {
  return Object.entries(value).flatMap(([key, nested]) =>
    nested && typeof nested === 'object'
      ? entries(nested, `${prefix}${key}.`)
      : [[`${prefix}${key}`, nested] as [string, unknown]],
  );
}

test('translation catalogs have identical keys', () => {
  assert.deepEqual(
    entries(zhCN)
      .map(([key]) => key)
      .sort(),
    entries(en)
      .map(([key]) => key)
      .sort(),
  );
});

test('no translation is empty', () => {
  for (const [key, value] of entries(en)) {
    assert.equal(typeof value, 'string', key);
    assert.ok((value as string).trim().length > 0, key);
  }
  for (const [key, value] of entries(zhCN)) {
    assert.equal(typeof value, 'string', key);
    assert.ok((value as string).trim().length > 0, key);
  }
});

test('chinese catalog is actually translated', () => {
  const english = new Map(entries(en));
  const chinese = new Map(entries(zhCN));
  let translated = 0;
  for (const [key, value] of chinese) {
    if (value !== english.get(key)) translated += 1;
  }
  assert.ok(translated / chinese.size > 0.8, 'most keys must differ from English');
});
