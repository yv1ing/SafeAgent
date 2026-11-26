import { cn } from '../../lib/cn';

export type ProgressTone = 'accent' | 'success' | 'warning' | 'danger' | 'info';

const tones: Record<ProgressTone, string> = {
  accent: 'bg-accent',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
  info: 'bg-info',
};

export function Progress({
  value,
  max = 100,
  tone = 'accent',
  label,
  className,
}: {
  value: number;
  max?: number;
  tone?: ProgressTone;
  label?: string;
  className?: string;
}) {
  const percent = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0;
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuenow={value}
      className={cn('h-1.5 overflow-hidden rounded-full bg-sunken', className)}
    >
      <div
        aria-hidden
        className={cn('h-full rounded-full transition-[width]', tones[tone])}
        style={{ width: `${percent}%` }}
      />
    </div>
  );
}
