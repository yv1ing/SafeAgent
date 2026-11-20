import { useState } from 'react';
import type { ConsoleCursorView } from '../api/generated/models';

const pageSize = 50;

export function useCursorPages(resetKey: string) {
  const [state, setState] = useState<{ key: string; stack: ConsoleCursorView[] }>({
    key: resetKey,
    stack: [],
  });
  const stack = state.key === resetKey ? state.stack : [];
  const cursor = stack.at(-1);
  return {
    params: { limit: pageSize, before: cursor?.before, before_id: cursor?.before_id },
    page: stack.length + 1,
    hasPrevious: stack.length > 0,
    next: (next: ConsoleCursorView) => setState({ key: resetKey, stack: [...stack, next] }),
    previous: () => setState({ key: resetKey, stack: stack.slice(0, -1) }),
  };
}
