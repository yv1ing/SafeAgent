import { ListCallsMinSeverity, ListCallsState } from '../../api/generated/models';

export interface CallsSearch {
  state?: ListCallsState;
  min_severity?: ListCallsMinSeverity;
  model?: string;
  agent_key_id?: string;
  session_id?: string;
  call?: string;
}

export type CallFilters = Omit<CallsSearch, 'call'>;

function text(value: unknown) {
  return typeof value === 'string' && value !== '' ? value : undefined;
}

function oneOf<T extends string>(value: unknown, allowed: Record<string, T>) {
  return Object.values(allowed).find((option) => option === value);
}

export function validateCallsSearch(search: Record<string, unknown>): CallsSearch {
  return {
    state: oneOf(search.state, ListCallsState),
    min_severity: oneOf(search.min_severity, ListCallsMinSeverity),
    model: text(search.model),
    agent_key_id: text(search.agent_key_id),
    session_id: text(search.session_id),
    call: text(search.call),
  };
}

export function filtersOf({ call: _selected, ...filters }: CallsSearch): CallFilters {
  return filters;
}

export function hasFilters(filters: CallFilters) {
  return Object.values(filters).some((value) => value !== undefined);
}
