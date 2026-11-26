import type {
  ConsoleRedactionIndexView,
  ConsoleRedactionLocationView,
  ConsoleRedactionValueView,
} from '../../api/generated/models';

export interface RevealTarget {
  call_id: string;
  section: string;
  ordinal: number;
  value_id?: string;
}

export interface RedactionSummary {
  values: number;
  total: number;
}

export function redactionSummary(
  index: Pick<ConsoleRedactionIndexView, 'values' | 'total'>,
): RedactionSummary {
  return { values: index.values.length, total: index.total };
}

export function itemNodeId(location: { call_id: string; section: string; ordinal: number }) {
  return `item:${location.call_id}:${location.section}:${location.ordinal}`;
}

export interface RedactionOccurrenceGroup {
  callId: string;
  createdAt: string;
  occurrences: ConsoleRedactionLocationView[];
}

// Occurrences arrive in conversation order, so grouping preserves both call order and position.
export function groupOccurrences(
  occurrences: ConsoleRedactionLocationView[],
): RedactionOccurrenceGroup[] {
  const groups: RedactionOccurrenceGroup[] = [];
  const byCall = new Map<string, RedactionOccurrenceGroup>();
  for (const occurrence of occurrences) {
    let group = byCall.get(occurrence.call_id);
    if (!group) {
      group = {
        callId: occurrence.call_id,
        createdAt: occurrence.call_created_at,
        occurrences: [],
      };
      byCall.set(occurrence.call_id, group);
      groups.push(group);
    }
    group.occurrences.push(occurrence);
  }
  return groups;
}

export function sortedValues(values: ConsoleRedactionValueView[]) {
  return [...values].sort(
    (left, right) => left.label.localeCompare(right.label) || right.count - left.count,
  );
}

export function filteredValues(values: ConsoleRedactionValueView[], onlyValueId: string | null) {
  if (!onlyValueId) return values;
  return values.filter((value) => value.value_id === onlyValueId);
}
