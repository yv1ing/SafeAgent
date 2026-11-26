export interface ByteRange {
  start: number;
  end: number;
}

export interface SpanRef {
  set: number;
  range: number;
}

export interface SpanSegment {
  text: string;
  spans: SpanRef[];
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function isContinuationByte(byte: number) {
  return (byte & 0xc0) === 0x80;
}

// Gateway offsets are UTF-8 byte positions (Go strings), not UTF-16 code unit indexes. Boundaries
// that fall inside a multi-byte code point are pulled back to the previous code point so that no
// slice is decoded into replacement characters.
function snapBoundary(bytes: Uint8Array, offset: number) {
  let boundary = Math.min(Math.max(offset, 0), bytes.length);
  while (boundary > 0 && boundary < bytes.length && isContinuationByte(bytes[boundary]!)) {
    boundary -= 1;
  }
  return boundary;
}

export function splitBySpanSets(text: string, sets: ByteRange[][]): SpanSegment[] {
  const bytes = encoder.encode(text);
  if (bytes.length === 0) return [];
  const spans: { set: number; range: number; start: number; end: number }[] = [];
  sets.forEach((ranges, set) => {
    ranges.forEach((range, index) => {
      const start = snapBoundary(bytes, range.start);
      const end = snapBoundary(bytes, range.end);
      if (start < end) spans.push({ set, range: index, start, end });
    });
  });
  const boundaries = new Set<number>([0, bytes.length]);
  for (const span of spans) {
    boundaries.add(span.start);
    boundaries.add(span.end);
  }
  const ordered = [...boundaries].sort((left, right) => left - right);
  const segments: SpanSegment[] = [];
  for (let index = 0; index < ordered.length - 1; index += 1) {
    const start = ordered[index]!;
    const end = ordered[index + 1]!;
    if (start >= end) continue;
    const active = spans
      .filter((span) => span.start <= start && span.end >= end)
      .map(({ set, range }) => ({ set, range }));
    segments.push({ text: decoder.decode(bytes.slice(start, end)), spans: active });
  }
  return segments;
}
