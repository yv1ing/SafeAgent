import type { TooltipContentProps } from 'recharts';

export function ChartTooltip({
  active,
  payload,
  label,
  formatValue,
}: Partial<TooltipContentProps> & { formatValue: (value: number) => string }) {
  if (!active || !payload?.length) return null;
  return (
    <div className="rounded-md border bg-raised px-3 py-2 text-xs shadow-pop">
      <p className="mb-1 font-medium">{label}</p>
      {payload.map((entry) => (
        <p key={String(entry.dataKey)} className="flex items-center gap-2">
          <span aria-hidden className="size-2 rounded-full" style={{ background: entry.color }} />
          <span className="text-muted">{entry.name}</span>
          <span className="num ml-auto pl-3 font-medium">{formatValue(Number(entry.value))}</span>
        </p>
      ))}
    </div>
  );
}
