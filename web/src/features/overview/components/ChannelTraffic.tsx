import type { ConsoleSeriesView } from '../../../api/generated/models';
import { HealthList } from '../../../components/data/HealthList';
import { Card, CardHeader } from '../../../components/ui/card';
import { interpolate, useLocale } from '../../../i18n';
import { formatDuration, formatNumber, formatPercent } from '../../../lib/format';

export function ChannelTraffic({ series }: { series: ConsoleSeriesView }) {
  const { t } = useLocale();
  return (
    <Card>
      <CardHeader title={t.overview.channelHealth} />
      {series.channels.length > 0 && (
        <ul className="divide-y border-b">
          {series.channels.map((channel) => (
            <li key={channel.channel_id} className="flex items-center gap-3 px-4 py-2 text-sm">
              <span className="min-w-0 flex-1 truncate">{channel.channel_name}</span>
              <span className="num text-xs text-muted">
                {interpolate(t.overview.channelLine, {
                  requests: formatNumber(channel.requests),
                  errors: formatPercent(channel.errors, channel.requests),
                  p95: formatDuration(channel.duration_p95),
                })}
              </span>
            </li>
          ))}
        </ul>
      )}
      <HealthList health={series.health} empty={t.overview.noHealth} />
    </Card>
  );
}
