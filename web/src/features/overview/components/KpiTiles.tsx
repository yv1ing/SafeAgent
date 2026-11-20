import type { ConsoleSeriesView, ConsoleStatsView } from '../../../api/generated/models';
import { StatTile } from '../../../components/data/StatTile';
import { Card } from '../../../components/ui/card';
import { interpolate, useLocale } from '../../../i18n';
import {
  formatCost,
  formatDuration,
  formatNumber,
  formatPercent,
  formatTokens,
} from '../../../lib/format';

export function KpiTiles({
  stats,
  series,
}: {
  stats: ConsoleStatsView;
  series?: ConsoleSeriesView;
}) {
  const { t } = useLocale();
  const totals = stats.totals;
  const buckets = series?.buckets ?? [];
  const trend = (pick: (bucket: (typeof buckets)[number]) => number) => buckets.map(pick);
  return (
    <Card className="p-0">
      <div className="grid grid-cols-2 gap-px bg-line md:grid-cols-3 xl:grid-cols-6">
        <StatTile
          label={t.overview.requests}
          value={formatNumber(totals.calls)}
          detail={interpolate(t.overview.failures, { count: formatNumber(totals.failures) })}
          trend={trend((bucket) => bucket.requests)}
        />
        <StatTile
          label={t.overview.blocked}
          value={formatNumber(totals.blocked)}
          detail={interpolate(t.overview.blockedDetail, {
            percent: formatPercent(totals.blocked, totals.calls),
          })}
          trend={trend((bucket) => bucket.blocked)}
        />
        <StatTile
          label={t.overview.tokens}
          value={formatTokens(totals.input_tokens + totals.output_tokens)}
          detail={interpolate(t.overview.tokensDetail, {
            input: formatTokens(totals.input_tokens),
            output: formatTokens(totals.output_tokens),
          })}
          trend={trend((bucket) => bucket.input_tokens + bucket.output_tokens)}
        />
        <StatTile
          label={t.overview.latency}
          value={formatDuration(totals.duration_p95)}
          detail={interpolate(t.overview.latencyDetail, {
            p50: formatDuration(totals.duration_p50),
            firstToken: formatDuration(totals.first_token_p50),
          })}
          trend={trend((bucket) => bucket.duration_p95)}
        />
        <StatTile
          label={t.overview.highRisk}
          value={formatNumber(totals.high_severity)}
          detail={interpolate(
            totals.redaction_hits === 1 ? t.overview.redactionsOne : t.overview.redactions,
            { count: formatNumber(totals.redaction_hits) },
          )}
          trend={trend((bucket) => bucket.findings_high)}
          alert={totals.high_severity > 0}
        />
        <StatTile
          label={t.overview.spend}
          value={
            stats.costs.length === 0
              ? t.overview.noSpend
              : formatCost(stats.costs[0]!.cost_micros, stats.costs[0]!.currency)
          }
          detail={stats.costs
            .slice(1)
            .map((cost) => formatCost(cost.cost_micros, cost.currency))
            .join(' · ')}
        />
      </div>
    </Card>
  );
}
