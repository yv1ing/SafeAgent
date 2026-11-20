export interface ByteRange {
  start: number;
  end: number;
}

export interface Segment {
  text: string;
  range?: number;
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

// Gateway offsets are UTF-8 byte positions (Go strings), not UTF-16 code unit indexes.
export function splitByByteRanges(text: string, ranges: ByteRange[]): Segment[] {
  const bytes = encoder.encode(text);
  const ordered = ranges
    .map((range, index) => ({ ...range, index }))
    .filter((range) => range.start < range.end && range.start < bytes.length)
    .sort((left, right) => left.start - right.start);
  const segments: Segment[] = [];
  let cursor = 0;
  for (const range of ordered) {
    const start = Math.max(range.start, cursor);
    const end = Math.min(range.end, bytes.length);
    if (start >= end) continue;
    if (start > cursor) segments.push({ text: decoder.decode(bytes.slice(cursor, start)) });
    segments.push({ text: decoder.decode(bytes.slice(start, end)), range: range.index });
    cursor = end;
  }
  if (cursor < bytes.length) segments.push({ text: decoder.decode(bytes.slice(cursor)) });
  return segments;
}
