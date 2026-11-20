import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { ChartTooltip } from './ChartTooltip';

interface Series {
  key: string;
  label: string;
  color: string;
}

const axisTick = { fill: 'var(--muted)', fontSize: 12 };

export function TimeChart({
  rows,
  series,
  stacked,
  formatValue,
}: {
  rows: Record<string, string | number>[];
  series: Series[];
  stacked?: boolean;
  formatValue: (value: number) => string;
}) {
  return (
    <div className="h-56">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={rows} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} stroke="var(--line)" />
          <XAxis dataKey="time" tick={axisTick} tickLine={false} axisLine={false} minTickGap={24} />
          <YAxis
            tick={axisTick}
            tickLine={false}
            axisLine={false}
            width={44}
            allowDecimals={false}
            tickFormatter={formatValue}
          />
          <Tooltip
            cursor={{ stroke: 'var(--line-strong)' }}
            content={(props) => <ChartTooltip {...props} formatValue={formatValue} />}
          />
          {series.map((entry) => (
            <Area
              key={entry.key}
              dataKey={entry.key}
              name={entry.label}
              type="monotone"
              stackId={stacked ? 'stack' : undefined}
              stroke={entry.color}
              strokeWidth={2}
              fill={entry.color}
              fillOpacity={stacked ? 0.35 : 0.12}
              activeDot={{ r: 4, stroke: 'var(--surface)', strokeWidth: 2 }}
              isAnimationActive={false}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
