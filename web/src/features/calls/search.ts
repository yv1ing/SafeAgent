import { ListCallsLane } from '../../api/generated/models/listCallsLane.ts';
import { ListCallsMinSeverity } from '../../api/generated/models/listCallsMinSeverity.ts';
import { ListCallsOperation } from '../../api/generated/models/listCallsOperation.ts';
import { ListCallsOutcome } from '../../api/generated/models/listCallsOutcome.ts';
import { ListCallsPurpose } from '../../api/generated/models/listCallsPurpose.ts';
import { ListCallsState } from '../../api/generated/models/listCallsState.ts';

export interface CallsSearch {
  state?: ListCallsState;
  min_severity?: ListCallsMinSeverity;
  model?: string;
  agent_key_id?: string;
  session_id?: string;
  lane?: ListCallsLane;
  operation?: ListCallsOperation;
  purpose?: ListCallsPurpose;
  outcome?: ListCallsOutcome;
  include_auxiliary?: boolean;
  call?: string;
}

export type CallFilters = Omit<CallsSearch, 'call'>;

function text(value: unknown) {
  return typeof value === 'string' && value !== '' ? value : undefined;
}

function flag(value: unknown) {
  return value === true || value === 'true' ? true : undefined;
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
    lane: oneOf(search.lane, ListCallsLane),
    operation: oneOf(search.operation, ListCallsOperation),
    purpose: oneOf(search.purpose, ListCallsPurpose),
    outcome: oneOf(search.outcome, ListCallsOutcome),
    include_auxiliary: flag(search.include_auxiliary),
    call: text(search.call),
  };
}

export function filtersOf({ call: _selected, ...filters }: CallsSearch): CallFilters {
  return filters;
}

export function hasFilters(filters: CallFilters) {
  return Object.values(filters).some((value) => value !== undefined);
}

// An explicit lane wins over the auxiliary toggle, matching the API contract.
export function listParams(filters: CallFilters) {
  if (filters.lane) return { ...filters, include_auxiliary: undefined };
  return filters;
}
