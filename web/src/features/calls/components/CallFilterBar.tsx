import { useEffect, useState } from 'react';
import { useListKeys } from '../../../api/generated/keys/keys';
import { FilterBar } from '../../../components/data/FilterBar';
import { Badge } from '../../../components/ui/badge';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { shortId } from '../../../lib/format';
import type {
  CallFilters,
  CallLane,
  CallOperation,
  CallOutcome,
  CallPurpose,
  CallSeverity,
  CallState,
} from '../search';

const debounceMilliseconds = 400;
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

function options<T extends string>(values: readonly T[], labels: Record<string, string>) {
  return values.map((value) => ({ value, label: labels[value] ?? value }));
}

export function CallFilterBar({
  projectId,
  filters,
  onChange,
  auxiliary,
  onAuxiliaryChange,
}: {
  projectId: string;
  filters: CallFilters;
  onChange: (filters: CallFilters) => void;
  auxiliary: boolean;
  onAuxiliaryChange: (enabled: boolean) => void;
}) {
  const { t } = useLocale();
  const keys = useListKeys(projectId);
  const [model, setModel] = useState(filters.model ?? '');
  useEffect(() => setModel(filters.model ?? ''), [filters.model]);
  useEffect(() => {
    const next = model.trim() || undefined;
    if (next === filters.model) return;
    const timer = setTimeout(() => onChange({ ...filters, model: next }), debounceMilliseconds);
    return () => clearTimeout(timer);
  }, [model, filters, onChange]);
  const chips = filters.session_id
    ? [
        {
          key: 'session',
          label: (
            <>
              {t.calls.session}: <span className="font-mono">{shortId(filters.session_id)}</span>
            </>
          ),
          removeLabel: `${t.common.clearFilters}: ${t.calls.session}`,
          onRemove: () => onChange({ ...filters, session_id: undefined }),
        },
      ]
    : [];
  return (
    <FilterBar
      searchLabel={t.calls.filters}
      searchPlaceholder={t.calls.modelPlaceholder}
      search={model}
      onSearchChange={setModel}
      searchClassName="font-mono"
      selects={[
        {
          key: 'state',
          label: t.calls.state,
          value: filters.state ?? '',
          width: 'w-36',
          options: [{ value: '', label: t.calls.anyState }, ...options(states, t.states)],
          onChange: (state) =>
            onChange({ ...filters, state: (state || undefined) as CallState | undefined }),
        },
        {
          key: 'severity',
          label: t.calls.minSeverity,
          value: filters.min_severity ?? '',
          width: 'w-36',
          options: [
            { value: '', label: t.calls.anySeverity },
            ...options(severities, t.severities),
          ],
          onChange: (severity) =>
            onChange({
              ...filters,
              min_severity: (severity || undefined) as CallSeverity | undefined,
            }),
        },
        {
          key: 'lane',
          label: t.calls.lane,
          value: filters.lane ?? '',
          width: 'w-32',
          options: [{ value: '', label: t.calls.anyLane }, ...options(lanes, t.lanes)],
          onChange: (lane) =>
            onChange({ ...filters, lane: (lane || undefined) as CallLane | undefined }),
        },
        {
          key: 'operation',
          label: t.calls.operation,
          value: filters.operation ?? '',
          width: 'w-40',
          options: [
            { value: '', label: t.calls.anyOperation },
            ...options(operations, t.classify.operations),
          ],
          onChange: (operation) =>
            onChange({
              ...filters,
              operation: (operation || undefined) as CallOperation | undefined,
            }),
        },
        {
          key: 'purpose',
          label: t.calls.purpose,
          value: filters.purpose ?? '',
          width: 'w-40',
          options: [
            { value: '', label: t.calls.anyPurpose },
            ...options(purposes, t.classify.purposes),
          ],
          onChange: (purpose) =>
            onChange({ ...filters, purpose: (purpose || undefined) as CallPurpose | undefined }),
        },
        {
          key: 'outcome',
          label: t.calls.outcome,
          value: filters.outcome ?? '',
          width: 'w-40',
          options: [{ value: '', label: t.calls.anyOutcome }, ...options(outcomes, t.outcomes)],
          onChange: (outcome) =>
            onChange({ ...filters, outcome: (outcome || undefined) as CallOutcome | undefined }),
        },
        {
          key: 'key',
          label: t.calls.agentKey,
          value: filters.agent_key_id ?? '',
          width: 'w-40',
          options: [
            { value: '', label: t.calls.anyKey },
            ...(keys.data ?? []).map((key) => ({ value: key.id, label: key.label ?? key.id })),
          ],
          onChange: (agentKeyId) => onChange({ ...filters, agent_key_id: agentKeyId || undefined }),
        },
      ]}
      chips={chips}
      onClear={() => onChange({})}
      actions={
        <label className="flex items-center gap-2 text-xs text-muted">
          <Switch
            aria-label={t.calls.includeAuxiliary}
            checked={auxiliary}
            disabled={Boolean(filters.lane)}
            onCheckedChange={onAuxiliaryChange}
          />
          <span>{t.calls.includeAuxiliary}</span>
          {auxiliary && <Badge tone="accent">{t.lanes.auxiliary}</Badge>}
        </label>
      }
    />
  );
}
