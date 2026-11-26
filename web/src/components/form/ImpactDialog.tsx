import { useEffect, useState } from 'react';
import { useLocale } from '../../i18n';
import { formatNumber } from '../../lib/format';
import { Callout } from '../feedback/Callout';
import { Button } from '../ui/button';
import { Dialog, DialogContent } from '../ui/dialog';
import { Select } from '../ui/select';
import { Switch } from '../ui/switch';

export interface ImpactReport {
  routes: string[];
  projects: string[];
  liveTraffic: number;
  replacements: string[];
}

function ImpactList({ items }: { items: string[] }) {
  const { t } = useLocale();
  if (items.length === 0) return <p className="text-sm text-muted">{t.impact.empty}</p>;
  return (
    <ul className="scroll-thin flex max-h-40 flex-col gap-1 overflow-y-auto">
      {items.map((item) => (
        <li key={item} className="truncate font-mono text-xs">
          {item}
        </li>
      ))}
    </ul>
  );
}

export function ImpactDialog({
  open,
  onOpenChange,
  title,
  description,
  report,
  confirmLabel,
  pending = false,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  report: ImpactReport;
  confirmLabel: string;
  pending?: boolean;
  onConfirm: (replacement?: string) => void;
}) {
  const { t } = useLocale();
  const [acknowledged, setAcknowledged] = useState(false);
  const [replacement, setReplacement] = useState('');

  useEffect(() => {
    if (!open) return;
    setAcknowledged(false);
    setReplacement('');
  }, [open]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={title}
        description={description}
        dismissible={!pending}
        footer={
          <>
            <Button onClick={() => onOpenChange(false)} disabled={pending}>
              {t.common.cancel}
            </Button>
            <Button
              variant="danger"
              loading={pending}
              disabled={!acknowledged}
              onClick={() => onConfirm(replacement || undefined)}
            >
              {confirmLabel}
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          <Callout tone="danger">{description ?? t.impact.warning}</Callout>
          <dl className="grid gap-4 sm:grid-cols-2">
            <div className="flex min-w-0 flex-col gap-1.5">
              <dt className="text-xs font-medium text-muted">{t.impact.routes}</dt>
              <dd>
                <ImpactList items={report.routes} />
              </dd>
            </div>
            <div className="flex min-w-0 flex-col gap-1.5">
              <dt className="text-xs font-medium text-muted">{t.impact.projects}</dt>
              <dd>
                <ImpactList items={report.projects} />
              </dd>
            </div>
            <div className="flex flex-col gap-1">
              <dt className="text-xs font-medium text-muted">{t.impact.liveTraffic}</dt>
              <dd className="num text-lg font-semibold">{formatNumber(report.liveTraffic)}</dd>
            </div>
            <div className="flex flex-col gap-1">
              <dt className="text-xs font-medium text-muted">{t.impact.replacements}</dt>
              <dd>
                <ImpactList items={report.replacements} />
              </dd>
            </div>
          </dl>
          {report.replacements.length > 0 && (
            <div className="flex flex-col gap-1.5">
              <span className="text-xs font-medium text-muted">{t.impact.replaceWith}</span>
              <Select
                value={replacement}
                onValueChange={setReplacement}
                options={[
                  { value: '', label: t.impact.noReplacement },
                  ...report.replacements.map((item) => ({ value: item, label: item })),
                ]}
              />
            </div>
          )}
          <label className="flex items-center gap-2 text-sm">
            <Switch
              aria-label={t.impact.acknowledge}
              checked={acknowledged}
              onCheckedChange={setAcknowledged}
            />
            {t.impact.acknowledge}
          </label>
        </div>
      </DialogContent>
    </Dialog>
  );
}
