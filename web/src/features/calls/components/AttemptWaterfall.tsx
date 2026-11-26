import type { ConsoleAttemptView } from '../../../api/generated/models';
import { Badge, type Tone } from '../../../components/ui/badge';
import { interpolate, useLocale } from '../../../i18n';
import { formatDuration } from '../../../lib/format';

const resultTone: Record<ConsoleAttemptView['result'], Tone> = {
  running: 'info',
  succeeded: 'success',
  failed: 'danger',
  cancelled: 'neutral',
};

export function AttemptWaterfall({ attempts }: { attempts: ConsoleAttemptView[] }) {
  const { t } = useLocale();
  const span = Math.max(...attempts.map((attempt) => attempt.end_ms), 1);
  const percent = (value: number) => `${(value / span) * 100}%`;
  return (
    <ol className="flex flex-col gap-3">
      {attempts.map((attempt) => {
        const active = attempt.result === 'succeeded' || attempt.result === 'running';
        return (
          <li key={attempt.index} className="flex flex-col gap-1.5">
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className="shrink-0 font-medium">
                {interpolate(t.call.attemptIndex, { index: attempt.index })}
              </span>
              <span className="min-w-0 flex-1 truncate">
                {interpolate(t.call.attemptLabel, {
                  channel: attempt.channel_name,
                  model: attempt.upstream_model,
                })}
              </span>
              <Badge tone={resultTone[attempt.result]}>
                {t.call.attemptResults[attempt.result]}
              </Badge>
              {attempt.recovered && <Badge tone="warning">{t.call.recovered}</Badge>}
              {attempt.code && (
                <span className="font-mono text-[11px] text-danger" title={attempt.class}>
                  {attempt.code}
                </span>
              )}
              <span className="num text-muted">
                {formatDuration(attempt.duration_ms || attempt.end_ms - attempt.start_ms)}
              </span>
            </div>
            <div className="relative h-2 rounded-full bg-sunken" aria-hidden>
              <div
                className={
                  active
                    ? 'absolute h-full rounded-full bg-chart-1'
                    : 'absolute h-full rounded-full bg-danger'
                }
                style={{
                  left: percent(attempt.start_ms),
                  width: percent(Math.max(attempt.end_ms - attempt.start_ms, span / 200)),
                }}
              />
              {attempt.first_token_ms > 0 && (
                <div
                  className="absolute -top-0.5 h-3 w-0.5 bg-ink"
                  style={{ left: percent(attempt.first_token_ms) }}
                />
              )}
            </div>
            <div className="flex flex-wrap gap-x-3 text-[11px] text-faint">
              <span className="font-mono">{attempt.format}</span>
              <span className="font-mono">{attempt.model_id}</span>
              {attempt.first_token_ms > 0 && (
                <span>{interpolate(t.call.firstTokenAt, { ms: attempt.first_token_ms })}</span>
              )}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
