import { ShieldAlert } from 'lucide-react';
import type { ConsoleCallView } from '../../../api/generated/models';
import {
  LaneBadge,
  OutcomeBadge,
  SeverityBadge,
  StateBadge,
} from '../../../components/feedback/Badges';
import { useLocale } from '../../../i18n';
import { cn } from '../../../lib/cn';
import { formatDate, formatDuration, formatTokens } from '../../../lib/format';
import { OperationBadge, PurposeBadge, ResolutionBadge } from './CallBadges';

export function CallList({
  calls,
  selectedId,
  onSelect,
}: {
  calls: ConsoleCallView[];
  selectedId?: string;
  onSelect: (callId: string) => void;
}) {
  const { t, locale } = useLocale();
  return (
    <ul aria-label={t.calls.list} className="divide-y">
      {calls.map((call) => {
        const selected = call.id === selectedId;
        return (
          <li key={call.id}>
            <button
              type="button"
              aria-current={selected ? 'true' : undefined}
              onClick={() => onSelect(call.id)}
              className={cn(
                'flex w-full flex-col gap-1.5 border-l-2 border-transparent px-4 py-2.5 text-left hover:bg-sunken',
                selected && 'border-accent bg-accent-soft hover:bg-accent-soft',
              )}
            >
              <div className="flex items-center gap-2">
                <StateBadge state={call.state} />
                {call.lane !== 'main' && <LaneBadge lane={call.lane} />}
                <span className="min-w-0 flex-1 truncate font-mono text-sm">
                  {call.public_model}
                </span>
                <span className="shrink-0 text-xs text-faint">
                  {formatDate(call.created_at, locale)}
                </span>
              </div>
              <div className="flex flex-wrap items-center gap-1.5">
                <OperationBadge operation={call.operation} />
                <PurposeBadge purpose={call.purpose} source={call.purpose_source} />
                <OutcomeBadge outcome={call.outcome} />
                <ResolutionBadge resolution={call.resolution} />
              </div>
              <div className="flex items-center gap-3 text-xs text-muted">
                <span className="num">{formatDuration(call.duration_ms)}</span>
                <span className="num">
                  {formatTokens(call.input_tokens)} / {formatTokens(call.output_tokens)}
                </span>
                {call.redaction_values > 0 && (
                  <span className="flex items-center gap-1" title={t.call.redactionValues}>
                    <ShieldAlert aria-hidden className="size-3.5" />
                    {call.redaction_values}
                  </span>
                )}
                <span className="ml-auto">
                  <SeverityBadge severity={call.max_severity} />
                </span>
              </div>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
