export type CallState = 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted';
export type CallSeverity = 'info' | 'medium' | 'high';
export type CallLane = 'main' | 'side' | 'auxiliary';
export type CallOperation =
  | 'generation'
  | 'count_tokens'
  | 'models'
  | 'model'
  | 'responses_retrieve'
  | 'responses_input_items'
  | 'responses_cancel'
  | 'responses_delete'
  | 'embeddings'
  | 'moderations';
export type CallPurpose =
  | 'chat'
  | 'subagent'
  | 'compaction'
  | 'title'
  | 'memory'
  | 'count_tokens'
  | 'model_list'
  | 'embedding'
  | 'moderation'
  | 'unknown';
export type CallOutcome =
  | 'ok'
  | 'blocked'
  | 'rejected'
  | 'client_error'
  | 'upstream_error'
  | 'unsupported'
  | 'not_found'
  | 'rate_limited'
  | 'cancelled';

export interface CallsSearch {
  state?: CallState;
  min_severity?: CallSeverity;
  model?: string;
  agent_key_id?: string;
  session_id?: string;
  lane?: CallLane;
  operation?: CallOperation;
  purpose?: CallPurpose;
  outcome?: CallOutcome;
  include_auxiliary?: boolean;
  call?: string;
}

export type CallFilters = Omit<CallsSearch, 'call'>;

const states: CallState[] = ['running', 'completed', 'failed', 'cancelled', 'interrupted'];
const severities: CallSeverity[] = ['info', 'medium', 'high'];
const lanes: CallLane[] = ['main', 'side', 'auxiliary'];
const operations: CallOperation[] = [
  'generation',
  'count_tokens',
  'models',
  'model',
  'responses_retrieve',
  'responses_input_items',
  'responses_cancel',
  'responses_delete',
  'embeddings',
  'moderations',
];
const purposes: CallPurpose[] = [
  'chat',
  'subagent',
  'compaction',
  'title',
  'memory',
  'count_tokens',
  'model_list',
  'embedding',
  'moderation',
  'unknown',
];
const outcomes: CallOutcome[] = [
  'ok',
  'blocked',
  'rejected',
  'client_error',
  'upstream_error',
  'unsupported',
  'not_found',
  'rate_limited',
  'cancelled',
];

function text(value: unknown) {
  return typeof value === 'string' && value !== '' ? value : undefined;
}

function flag(value: unknown) {
  return value === true || value === 'true' ? true : undefined;
}

function oneOf<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
  return allowed.find((option) => option === value);
}

export function validateCallsSearch(search: Record<string, unknown>): CallsSearch {
  return {
    state: oneOf(search.state, states),
    min_severity: oneOf(search.min_severity, severities),
    model: text(search.model),
    agent_key_id: text(search.agent_key_id),
    session_id: text(search.session_id),
    lane: oneOf(search.lane, lanes),
    operation: oneOf(search.operation, operations),
    purpose: oneOf(search.purpose, purposes),
    outcome: oneOf(search.outcome, outcomes),
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

export function listParams(filters: CallFilters) {
  if (filters.lane) return { ...filters, include_auxiliary: undefined };
  return filters;
}
