import { AlertTriangle, CheckCircle2, Info, ShieldAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

type CalloutTone = 'info' | 'success' | 'warning' | 'danger';

const tones: Record<CalloutTone, { box: string; icon: typeof Info }> = {
  info: { box: 'border-info/25 bg-info-soft/60 text-info', icon: Info },
  success: { box: 'border-success/25 bg-success-soft/60 text-success', icon: CheckCircle2 },
  warning: { box: 'border-warning/25 bg-warning-soft/60 text-warning', icon: AlertTriangle },
  danger: { box: 'border-danger/25 bg-danger-soft/60 text-danger', icon: ShieldAlert },
};

export function Callout({
  tone = 'info',
  children,
  className,
}: {
  tone?: CalloutTone;
  children: ReactNode;
  className?: string;
}) {
  const { box, icon: Icon } = tones[tone];
  return (
    <div
      role={tone === 'danger' ? 'alert' : 'status'}
      className={cn(
        'flex items-start gap-2.5 rounded-md border px-3.5 py-2.5 text-sm',
        box,
        className,
      )}
    >
      <Icon aria-hidden className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 text-ink">{children}</div>
    </div>
  );
}
