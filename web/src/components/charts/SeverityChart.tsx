import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { useLocale } from '../../i18n';
import { formatNumber } from '../../lib/format';
import { ChartTooltip } from './ChartTooltip';

const severityColor = { high: 'var(--danger)', medium: 'var(--warning)', info: 'var(--info)' };
const order = ['high', 'medium', 'info'] as const;
const axisTick = { fill: 'var(--muted)', fontSize: 12 };

interface SeverityCount {
  severity: string;
  count: number;
}

export function SeverityChart({ severities }: { severities: SeverityCount[] }) {
  const { t } = useLocale();
  const rows = order.map((severity) => ({
    severity,
    label: t.severities[severity],
    count: severities.find((entry) => entry.severity === severity)?.count ?? 0,
  }));
  return (
    <div>
      <div className="h-48">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart
            data={rows}
            margin={{ top: 16, right: 4, bottom: 0, left: 0 }}
            barCategoryGap="30%"
          >
            <CartesianGrid vertical={false} stroke="var(--line)" />
            <XAxis dataKey="label" tick={axisTick} tickLine={false} axisLine={false} />
            <YAxis
              tick={axisTick}
              tickLine={false}
              axisLine={false}
              width={44}
              allowDecimals={false}
            />
            <Tooltip
              cursor={{ fill: 'var(--sunken)' }}
              content={(props) => <ChartTooltip {...props} formatValue={formatNumber} />}
            />
            <Bar
              dataKey="count"
              name={t.table.severity}
              radius={[4, 4, 0, 0]}
              label={{ position: 'top', fill: 'var(--muted)', fontSize: 12 }}
              isAnimationActive={false}
            >
              {rows.map((row) => (
                <Cell key={row.severity} fill={severityColor[row.severity]} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>
      <details className="mt-2 text-xs">
        <summary className="cursor-pointer text-muted">{t.table.showAsTable}</summary>
        <table className="mt-2 w-full border-collapse text-left">
          <thead>
            <tr className="border-b">
              <th className="py-1 pr-2 font-medium">{t.table.severity}</th>
              <th className="py-1 text-right font-medium">{t.table.count}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.severity} className="border-b last:border-0">
                <td className="py-1 pr-2">{row.label}</td>
                <td className="num py-1 text-right">{formatNumber(row.count)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </details>
    </div>
  );
}
