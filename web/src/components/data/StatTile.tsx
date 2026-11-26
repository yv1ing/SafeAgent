import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';

function sparklinePath(points: number[], width: number, height: number) {
  const maximum = Math.max(...points, 1);
  const step = points.length > 1 ? width / (points.length - 1) : 0;
  return points
    .map((point, index) => {
      const x = (index * step).toFixed(1);
      const y = (height - 1 - (point / maximum) * (height - 2)).toFixed(1);
      return `${index === 0 ? 'M' : 'L'}${x},${y}`;
    })
    .join(' ');
}

export function StatTile({
  label,
  value,
  detail,
  trend,
  alert = false,
  icon: Icon,
  className,
}: {
  label: string;
  value: string;
  detail?: string;
  trend?: number[];
  alert?: boolean;
  icon?: LucideIcon;
  className?: string;
}) {
  return (
    <div className={cn('flex min-w-0 gap-3 bg-surface p-4', className)}>
      {Icon && (
        <span
          className={cn(
            'grid size-8 shrink-0 place-items-center rounded-md',
            alert ? 'bg-danger-soft text-danger' : 'bg-accent-soft text-accent',
          )}
        >
          <Icon aria-hidden className="size-4" />
        </span>
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="text-xs font-medium text-muted">{label}</span>
        <span className={cn('num text-2xl leading-8 font-semibold', alert && 'text-danger')}>
          {value}
        </span>
        {(detail || (trend && trend.length > 1)) && (
          <div className="flex min-h-6 items-end justify-between gap-2">
            <span className="line-clamp-2 text-[11px] leading-4 text-faint">{detail}</span>
            {trend && trend.length > 1 && (
              <svg aria-hidden viewBox="0 0 80 24" className="h-6 w-14 shrink-0 overflow-visible">
                <path
                  d={sparklinePath(trend, 80, 24)}
                  fill="none"
                  stroke={alert ? 'var(--danger)' : 'var(--chart-1)'}
                  strokeWidth={1.5}
                  strokeLinejoin="round"
                  strokeLinecap="round"
                />
              </svg>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
