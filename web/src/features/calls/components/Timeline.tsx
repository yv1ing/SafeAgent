import type {
  ConsoleCallDetailView,
  ConsoleTimelineEntryView,
} from '../../../api/generated/models';
import { Badge, type Tone } from '../../../components/ui/badge';
import { useLocale } from '../../../i18n';
import { formatDate, formatNumber } from '../../../lib/format';

function tone(status: string): Tone {
  switch (status) {
    case 'blocked':
    case 'failed':
    case 'broken':
      return 'danger';
    case 'cancelled':
    case 'incomplete':
      return 'warning';
    case 'ok':
    case 'completed':
    case 'verified':
      return 'success';
    case 'info':
    case 'in_progress':
    case 'running':
      return 'info';
    default:
      return 'neutral';
  }
}

function Detail({ entry }: { entry: ConsoleTimelineEntryView }) {
  const { t } = useLocale();
  const parts: string[] = [];
  if (entry.tool_name) parts.push(entry.tool_name);
  if (entry.channel_id) parts.push(entry.channel_id);
  if (entry.upstream_model) parts.push(entry.upstream_model);
  if (entry.response) {
    parts.push(
      `${entry.response.status || 'completed'} · ${formatNumber(entry.response.items)} items · ${formatNumber(entry.response.input_tokens)}/${formatNumber(entry.response.output_tokens)} tokens`,
    );
    if (entry.response.partial) parts.push('partial');
    if (entry.response.truncated) parts.push('truncated');
    if (entry.response.blocked) parts.push(t.call.timelineLabels.call_blocked);
  }
  if (parts.length === 0) return null;
  return <p className="mt-0.5 font-mono text-[11px] text-muted">{parts.join(' · ')}</p>;
}

export function Timeline({ detail }: { detail: ConsoleCallDetailView }) {
  const { t, locale } = useLocale();
  const integrity = detail.integrity;
  if (detail.timeline.length === 0) {
    return <p className="text-sm text-muted">{t.call.noTimeline}</p>;
  }
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2 rounded-md border bg-surface px-3 py-2 text-xs">
        <span className="text-muted">{t.call.integrity}</span>
        <Badge tone={tone(integrity.status)}>
          {t.call.integrityStatuses[integrity.status as keyof typeof t.call.integrityStatuses] ??
            integrity.status}
        </Badge>
        <span className="num text-faint">
          {formatNumber(integrity.events)} · {integrity.head || '—'}
        </span>
        {integrity.error && <span className="text-danger">{integrity.error}</span>}
      </div>
      <ol className="flex flex-col">
        {detail.timeline.map((entry) => (
          <li key={entry.sequence} className="flex gap-3 border-l border-line pb-3 pl-3 last:pb-0">
            <span className="num w-8 shrink-0 text-right text-[11px] text-faint">
              {entry.sequence}
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <Badge tone={tone(entry.status)}>
                  {t.call.timelineLabels[entry.label as keyof typeof t.call.timelineLabels] ??
                    entry.label}
                </Badge>
                <span className="font-mono text-[11px] text-faint">
                  {t.call.timelineCategories[
                    entry.category as keyof typeof t.call.timelineCategories
                  ] ?? entry.category}
                </span>
                {entry.code && (
                  <span className="font-mono text-[11px] text-muted">{entry.code}</span>
                )}
                <span className="ml-auto text-[11px] text-faint">
                  {formatDate(entry.at, locale)}
                </span>
              </div>
              {entry.message && <p className="mt-0.5 text-xs text-muted">{entry.message}</p>}
              <Detail entry={entry} />
            </div>
          </li>
        ))}
      </ol>
    </div>
  );
}
