import { Eye, ShieldAlert, X } from 'lucide-react';
import type {
  ConsoleRedactionIndexView,
  ConsoleRedactionLocationView,
  ConsoleRedactionValueView,
} from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { interpolate, useLocale } from '../../../i18n';
import { formatDate, shortId } from '../../../lib/format';
import { filteredValues, groupOccurrences, redactionSummary, sortedValues } from '../redaction';

export type RedactionRevealHandler = (
  occurrence: ConsoleRedactionLocationView,
  valueId: string,
) => void;

export function RedactionSummaryChips({
  index,
  onlyValueId,
  onOpen,
  onClearValue,
}: {
  index: ConsoleRedactionIndexView;
  onlyValueId: string | null;
  onOpen: () => void;
  onClearValue: () => void;
}) {
  const { t } = useLocale();
  const summary = redactionSummary(index);
  const active = onlyValueId
    ? index.values.find((value) => value.value_id === onlyValueId)
    : undefined;
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        onClick={onOpen}
        className="inline-flex h-7 items-center gap-1.5 rounded-md border bg-surface px-2.5 text-xs font-medium text-muted transition-colors hover:bg-sunken hover:text-ink"
      >
        <ShieldAlert aria-hidden className="size-3.5" />
        {interpolate(summary.values === 1 ? t.redaction.summaryOne : t.redaction.summary, {
          values: summary.values,
          total: summary.total,
        })}
      </button>
      {active && (
        <Badge tone="warning" className="h-7 gap-1.5 pr-0.5">
          <span className="font-mono">{active.label}</span>
          <button
            type="button"
            aria-label={t.redaction.allValues}
            className="rounded-sm p-0.5 hover:bg-warning/15"
            onClick={onClearValue}
          >
            <X aria-hidden />
          </button>
        </Badge>
      )}
    </div>
  );
}

function OccurrenceRow({
  occurrence,
  valueId,
  onReveal,
  onOnlyValue,
}: {
  occurrence: ConsoleRedactionLocationView;
  valueId: string;
  onReveal: RedactionRevealHandler;
  onOnlyValue?: () => void;
}) {
  const { t, locale } = useLocale();
  return (
    <li className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 py-1.5 text-[11px]">
      <span className="text-faint">
        {t.classify.sections[occurrence.section as keyof typeof t.classify.sections] ??
          occurrence.section}{' '}
        · #{occurrence.ordinal}
      </span>
      {occurrence.path && <span className="truncate font-mono text-muted">{occurrence.path}</span>}
      <span className="ml-auto text-faint">{formatDate(occurrence.call_created_at, locale)}</span>
      <Button size="sm" variant="ghost" onClick={() => onReveal(occurrence, valueId)}>
        <Eye aria-hidden />
        {t.redaction.showInOriginal}
      </Button>
      {onOnlyValue && (
        <Button size="sm" variant="ghost" onClick={onOnlyValue}>
          {t.redaction.onlyThisValue}
        </Button>
      )}
    </li>
  );
}

function ValueItem({
  value,
  onlyValueId,
  onOnlyValue,
  onReveal,
}: {
  value: ConsoleRedactionValueView;
  onlyValueId: string | null;
  onOnlyValue: (valueId: string | null) => void;
  onReveal: RedactionRevealHandler;
}) {
  const { t, locale } = useLocale();
  const groups = groupOccurrences(value.occurrences);
  return (
    <li className="rounded-md border bg-surface">
      <details>
        <summary className="flex cursor-pointer flex-wrap items-center gap-2 px-3 py-2 text-xs">
          <span className="font-mono font-medium">{value.label}</span>
          <Badge tone={value.action === 'block' ? 'danger' : 'warning'}>
            {t.redaction.actions[value.action]}
          </Badge>
          {value.detector && (
            <span className="font-mono text-[11px] text-faint">{value.detector}</span>
          )}
          {value.secret && <Badge tone="danger">{t.call.secret}</Badge>}
          <span className="ml-auto text-muted">
            {interpolate(t.redaction.occurrences, { count: value.count })}
          </span>
        </summary>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t px-3 py-2 text-[11px] text-faint">
          <span>
            {interpolate(t.redaction.firstCall, {
              call: shortId(value.first_call_id),
              time: formatDate(groups[0]?.createdAt, locale),
            })}
          </span>
          <span>
            {interpolate(t.redaction.lastCall, {
              call: shortId(value.last_call_id),
              time: formatDate(groups.at(-1)?.createdAt, locale),
            })}
          </span>
          <Button
            size="sm"
            variant={onlyValueId === value.value_id ? 'secondary' : 'ghost'}
            className="ml-auto"
            onClick={() => onOnlyValue(onlyValueId === value.value_id ? null : value.value_id)}
          >
            {onlyValueId === value.value_id ? t.redaction.allValues : t.redaction.onlyThisValue}
          </Button>
        </div>
        <RedactionOccurrences value={value} onReveal={onReveal} />
      </details>
    </li>
  );
}

export function RedactionOccurrences({
  value,
  onReveal,
}: {
  value: ConsoleRedactionValueView;
  onReveal: RedactionRevealHandler;
}) {
  const { locale } = useLocale();
  const groups = groupOccurrences(value.occurrences);
  return (
    <ul className="divide-y border-t">
      {groups.map((group) => (
        <li key={group.callId}>
          <p className="flex items-center gap-2 bg-sunken/50 px-3 py-1 font-mono text-[11px] text-muted">
            {shortId(group.callId)}
            <span className="font-sans text-faint">{formatDate(group.createdAt, locale)}</span>
          </p>
          <ul className="divide-y">
            {group.occurrences.map((occurrence) => (
              <OccurrenceRow
                key={`${occurrence.call_id}:${occurrence.section}:${occurrence.ordinal}:${occurrence.start}`}
                occurrence={occurrence}
                valueId={value.value_id}
                onReveal={onReveal}
              />
            ))}
          </ul>
        </li>
      ))}
    </ul>
  );
}

export function RedactionPanel({
  index,
  onlyValueId,
  onOnlyValue,
  onReveal,
}: {
  index: ConsoleRedactionIndexView;
  onlyValueId: string | null;
  onOnlyValue: (valueId: string | null) => void;
  onReveal: RedactionRevealHandler;
}) {
  const { t } = useLocale();
  const values = sortedValues(filteredValues(index.values, onlyValueId));
  return (
    <div className="flex flex-col gap-3 p-4">
      <div>
        <h2 className="text-sm font-semibold">{t.redaction.sessionTitle}</h2>
        <p className="mt-0.5 text-xs text-muted">{t.redaction.sessionHint}</p>
      </div>
      {values.length === 0 ? (
        <p className="text-sm text-muted">{t.redaction.noValues}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {values.map((value) => (
            <ValueItem
              key={value.value_id}
              value={value}
              onlyValueId={onlyValueId}
              onOnlyValue={onOnlyValue}
              onReveal={onReveal}
            />
          ))}
        </ul>
      )}
    </div>
  );
}
