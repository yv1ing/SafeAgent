import type { ConsoleHealthView } from '../../api/generated/models';
import { Badge, type Tone } from '../ui/badge';
import { interpolate, useLocale } from '../../i18n';

const breakerTone: Record<ConsoleHealthView['state'], Tone> = {
  closed: 'success',
  half_open: 'warning',
  open: 'danger',
};

export function HealthList({ health, empty }: { health: ConsoleHealthView[]; empty: string }) {
  const { t } = useLocale();
  if (health.length === 0) return <p className="px-4 py-6 text-sm text-muted">{empty}</p>;
  return (
    <ul className="divide-y">
      {health.map((entry) => (
        <li
          key={`${entry.channel_id}/${entry.upstream_model}`}
          className="flex items-center justify-between gap-3 px-4 py-2.5"
        >
          <div className="min-w-0">
            <p className="truncate text-sm">
              {entry.channel_name} / <span className="font-mono">{entry.upstream_model}</span>
            </p>
            <p className="text-xs text-muted">
              {interpolate(
                entry.consecutive_failures === 1
                  ? t.overview.failuresInRowOne
                  : t.overview.failuresInRow,
                { count: entry.consecutive_failures },
              )}
              {entry.cooldown_seconds > 0 &&
                ` · ${interpolate(t.overview.cooldown, { seconds: entry.cooldown_seconds })}`}
            </p>
          </div>
          <Badge tone={breakerTone[entry.state]}>{t.overview.breaker[entry.state]}</Badge>
        </li>
      ))}
    </ul>
  );
}
