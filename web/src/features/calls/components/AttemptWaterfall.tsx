import type { ConsoleAttemptView } from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { interpolate, useLocale } from '../../../i18n';
import { formatDuration } from '../../../lib/format';

export function AttemptWaterfall({ attempts }: { attempts: ConsoleAttemptView[] }) {
  const { t } = useLocale();
  const span = Math.max(...attempts.map((attempt) => attempt.end_ms), 1);
  const percent = (value: number) => `${(value / span) * 100}%`;
  return (
    <ol className="flex flex-col gap-2.5">
      {attempts.map((attempt, index) => {
        const succeeded = attempt.result === 'succeeded';
        const running = attempt.result === 'running';
        const tone = succeeded ? 'success' : running ? 'info' : 'danger';
        return (
          <li key={index} className="flex flex-col gap-1">
            <div className="flex items-center gap-2 text-xs">
              <span className="min-w-0 flex-1 truncate">
                {interpolate(t.call.attemptLabel, {
                  channel: attempt.channel_name,
                  model: attempt.upstream_model,
                })}
              </span>
              <Badge tone={tone}>
                {attempt.code ||
                  (t.call.attemptResults[attempt.result as keyof typeof t.call.attemptResults] ??
                    attempt.result)}
              </Badge>
              <span className="num text-muted">
                {formatDuration(attempt.end_ms - attempt.start_ms)}
              </span>
            </div>
            <div className="relative h-2 rounded-full bg-sunken" aria-hidden>
              <div
                className={
                  succeeded
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
            {attempt.first_token_ms > 0 && (
              <span className="text-xs text-faint">
                {interpolate(t.call.firstTokenAt, { ms: attempt.first_token_ms })}
              </span>
            )}
          </li>
        );
      })}
    </ol>
  );
}
