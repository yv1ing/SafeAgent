import { Search, X } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useListKeys } from '../../../api/generated/keys/keys';
import { ListCallsMinSeverity, ListCallsState } from '../../../api/generated/models';
import { slashFocusAttribute } from '../../../lib/shortcuts';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { Input } from '../../../components/ui/input';
import { Kbd } from '../../../components/ui/kbd';
import { Select } from '../../../components/ui/select';
import { useLocale } from '../../../i18n';
import { shortId } from '../../../lib/format';
import { hasFilters, type CallFilters } from '../search';

const debounceMilliseconds = 400;

export function CallFilterBar({
  projectId,
  filters,
  onChange,
}: {
  projectId: string;
  filters: CallFilters;
  onChange: (filters: CallFilters) => void;
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
  return (
    <div role="search" aria-label={t.calls.filters} className="flex flex-wrap items-center gap-2">
      <div className="relative w-full sm:w-60">
        <Search
          aria-hidden
          className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint"
        />
        <Input
          {...{ [slashFocusAttribute]: '' }}
          aria-label={t.calls.model}
          placeholder={t.calls.modelPlaceholder}
          className="pr-8 pl-8 font-mono"
          value={model}
          onChange={(event) => setModel(event.target.value)}
        />
        <Kbd className="absolute top-1.5 right-2">/</Kbd>
      </div>
      <Select
        className="w-40"
        aria-label={t.calls.state}
        value={filters.state ?? ''}
        options={[
          { value: '', label: t.calls.anyState },
          ...Object.values(ListCallsState).map((state) => ({
            value: state,
            label: t.states[state],
          })),
        ]}
        onValueChange={(state) =>
          onChange({ ...filters, state: (state || undefined) as CallFilters['state'] })
        }
      />
      <Select
        className="w-40"
        aria-label={t.calls.minSeverity}
        value={filters.min_severity ?? ''}
        options={[
          { value: '', label: t.calls.anySeverity },
          ...Object.values(ListCallsMinSeverity).map((severity) => ({
            value: severity,
            label: t.severities[severity],
          })),
        ]}
        onValueChange={(severity) =>
          onChange({
            ...filters,
            min_severity: (severity || undefined) as CallFilters['min_severity'],
          })
        }
      />
      <Select
        className="w-44"
        aria-label={t.calls.agentKey}
        value={filters.agent_key_id ?? ''}
        options={[
          { value: '', label: t.calls.anyKey },
          ...(keys.data ?? []).map((key) => ({ value: key.id, label: key.label ?? key.id })),
        ]}
        onValueChange={(agentKeyId) =>
          onChange({ ...filters, agent_key_id: agentKeyId || undefined })
        }
      />
      {filters.session_id && (
        <Badge tone="accent" className="h-7 gap-1.5 pr-0.5">
          {t.calls.session}: <span className="font-mono">{shortId(filters.session_id)}</span>
          <button
            type="button"
            aria-label={`${t.common.clearFilters}: ${t.calls.session}`}
            className="rounded-sm p-0.5 hover:bg-accent/15"
            onClick={() => onChange({ ...filters, session_id: undefined })}
          >
            <X aria-hidden />
          </button>
        </Badge>
      )}
      {hasFilters(filters) && (
        <Button size="sm" variant="ghost" onClick={() => onChange({})}>
          {t.common.clearFilters}
        </Button>
      )}
    </div>
  );
}
