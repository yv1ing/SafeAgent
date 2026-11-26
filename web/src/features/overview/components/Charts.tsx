import type { ConsoleSeriesView, ConsoleStatsView } from '../../../api/generated/models';
import { ChartCard } from '../../../components/charts/ChartCard';
import { TimeChart } from '../../../components/charts/TimeChart';
import { Card } from '../../../components/ui/card';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../components/ui/tabs';
import { findingLabel, useLocale } from '../../../i18n';
import { formatBucket, formatCost, formatNumber, formatTokens } from '../../../lib/format';
import { RankList } from './RankList';

const rankLimit = 6;

export function TrafficCharts({ series }: { series: ConsoleSeriesView }) {
  const { t, locale } = useLocale();
  const rows = series.buckets.map((bucket) => ({
    time: formatBucket(bucket.start, locale, series.window !== '24h'),
    requests: bucket.requests,
    errors: bucket.errors,
    input: bucket.input_tokens,
    output: bucket.output_tokens,
  }));
  const requestSeries = [
    { key: 'requests', label: t.overview.requests, color: 'var(--chart-1)' },
    { key: 'errors', label: t.overview.errors, color: 'var(--danger)' },
  ];
  const tokenSeries = [
    { key: 'input', label: t.overview.inputTokens, color: 'var(--chart-1)' },
    { key: 'output', label: t.overview.outputTokens, color: 'var(--chart-2)' },
  ];
  const table = (series: typeof requestSeries) => ({
    columns: [t.table.time, ...series.map((entry) => entry.label)],
    rows: rows.map((row) => [
      row.time,
      ...series.map((entry) => row[entry.key as keyof typeof row]),
    ]),
  });
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <ChartCard
        title={t.overview.requestsChart}
        legend={requestSeries}
        table={table(requestSeries)}
      >
        <TimeChart rows={rows} series={requestSeries} formatValue={formatNumber} />
      </ChartCard>
      <ChartCard title={t.overview.tokensChart} legend={tokenSeries} table={table(tokenSeries)}>
        <TimeChart rows={rows} series={tokenSeries} stacked formatValue={formatTokens} />
      </ChartCard>
    </div>
  );
}

function groupItems(stats: ConsoleStatsView | undefined) {
  return (stats?.groups ?? []).slice(0, rankLimit).map((group) => ({
    key: group.name,
    label: group.label || group.name,
    value: group.calls,
    detail: group.costs.map((cost) => formatCost(cost.cost_micros, cost.currency)).join(' · '),
  }));
}

export function Breakdowns({
  byModel,
  byKey,
  series,
}: {
  byModel: ConsoleStatsView;
  byKey?: ConsoleStatsView;
  series?: ConsoleSeriesView;
}) {
  const { t } = useLocale();
  const models = groupItems(byModel);
  const keys = groupItems(byKey);
  const codes = (series?.finding_codes ?? []).slice(0, rankLimit).map((entry) => ({
    key: entry.code,
    label: findingLabel(t, entry.code),
    value: entry.count,
  }));
  const redactions = (series?.redactions ?? []).slice(0, rankLimit).map((entry) => ({
    key: entry.label,
    label: <span className="font-mono">{entry.label}</span>,
    value: entry.values,
  }));
  return (
    <Tabs defaultValue="models">
      <Card>
        <div className="flex flex-col gap-2 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <h2 className="text-sm font-semibold">{t.overview.breakdowns}</h2>
          <TabsList className="border-0 px-0">
            <TabsTrigger value="models">{t.overview.byModel}</TabsTrigger>
            <TabsTrigger value="keys">{t.overview.byKey}</TabsTrigger>
            <TabsTrigger value="findings">{t.overview.findingCodes}</TabsTrigger>
            <TabsTrigger value="redactions">{t.overview.redactionLabels}</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="models">
          <RankList items={models} formatValue={formatNumber} empty={t.overview.noData} />
        </TabsContent>
        <TabsContent value="keys">
          <RankList items={keys} formatValue={formatNumber} empty={t.overview.noData} />
        </TabsContent>
        <TabsContent value="findings">
          <RankList items={codes} formatValue={formatNumber} empty={t.overview.noData} />
        </TabsContent>
        <TabsContent value="redactions">
          <RankList items={redactions} formatValue={formatNumber} empty={t.overview.noData} />
        </TabsContent>
      </Card>
    </Tabs>
  );
}
