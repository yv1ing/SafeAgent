import { useEffect, useState } from 'react';
import { useListKeys } from '../../../api/generated/keys/keys';
import {
  ListCallsLane,
  ListCallsMinSeverity,
  ListCallsOperation,
  ListCallsOutcome,
  ListCallsPurpose,
  ListCallsState,
} from '../../../api/generated/models';
import { FilterBar } from '../../../components/data/FilterBar';
import { Badge } from '../../../components/ui/badge';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { shortId } from '../../../lib/format';
import type { CallFilters } from '../search';

const debounceMilliseconds = 400;

function options<T extends string>(catalog: Record<string, T>, labels: Record<string, string>) {
  return Object.values(catalog).map((value) => ({ value, label: labels[value] ?? value }));
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
          options: [{ value: '', label: t.calls.anyState }, ...options(ListCallsState, t.states)],
          onChange: (state) =>
            onChange({ ...filters, state: (state || undefined) as CallFilters['state'] }),
        },
        {
          key: 'severity',
          label: t.calls.minSeverity,
          value: filters.min_severity ?? '',
          width: 'w-36',
          options: [
            { value: '', label: t.calls.anySeverity },
            ...options(ListCallsMinSeverity, t.severities),
          ],
          onChange: (severity) =>
            onChange({
              ...filters,
              min_severity: (severity || undefined) as CallFilters['min_severity'],
            }),
        },
        {
          key: 'lane',
          label: t.calls.lane,
          value: filters.lane ?? '',
          width: 'w-32',
          options: [{ value: '', label: t.calls.anyLane }, ...options(ListCallsLane, t.lanes)],
          onChange: (lane) =>
            onChange({ ...filters, lane: (lane || undefined) as CallFilters['lane'] }),
        },
        {
          key: 'operation',
          label: t.calls.operation,
          value: filters.operation ?? '',
          width: 'w-40',
          options: [
            { value: '', label: t.calls.anyOperation },
            ...options(ListCallsOperation, t.classify.operations),
          ],
          onChange: (operation) =>
            onChange({
              ...filters,
              operation: (operation || undefined) as CallFilters['operation'],
            }),
        },
        {
          key: 'purpose',
          label: t.calls.purpose,
          value: filters.purpose ?? '',
          width: 'w-40',
          options: [
            { value: '', label: t.calls.anyPurpose },
            ...options(ListCallsPurpose, t.classify.purposes),
          ],
          onChange: (purpose) =>
            onChange({ ...filters, purpose: (purpose || undefined) as CallFilters['purpose'] }),
        },
        {
          key: 'outcome',
          label: t.calls.outcome,
          value: filters.outcome ?? '',
          width: 'w-40',
          options: [
            { value: '', label: t.calls.anyOutcome },
            ...options(ListCallsOutcome, t.outcomes),
          ],
          onChange: (outcome) =>
            onChange({ ...filters, outcome: (outcome || undefined) as CallFilters['outcome'] }),
        },
        {
          key: 'key',
          label: t.calls.agentKey,
          value: filters.agent_key_id ?? '',
          width: 'w-40',
          options: [
            { value: '', label: t.calls.anyKey },
            ...(keys.data ?? []).map((key) => ({
              value: key.id,
              label: key.label ?? key.id,
            })),
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
