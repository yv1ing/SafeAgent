import { Check } from 'lucide-react';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { interpolate, useLocale } from '../../i18n';
import { cn } from '../../lib/cn';
import { Button } from '../ui/button';
import { Dialog, DialogContent } from '../ui/dialog';

export interface WizardStep {
  key: string;
  title: string;
  description?: string;
  content: ReactNode;
  blocked?: boolean;
}

export function Wizard({
  open,
  onOpenChange,
  title,
  description,
  steps,
  finishLabel,
  pending = false,
  onSubmit,
  wide = false,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  steps: WizardStep[];
  finishLabel: string;
  pending?: boolean;
  onSubmit: () => void;
  wide?: boolean;
}) {
  const { t } = useLocale();
  const [active, setActive] = useState(0);
  const heading = useRef<HTMLHeadingElement>(null);
  const mounted = useRef(false);
  const current = Math.min(active, steps.length - 1);
  const last = current === steps.length - 1;
  const blocked = steps[current]?.blocked ?? false;

  useEffect(() => {
    if (open) {
      setActive(0);
      mounted.current = false;
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    if (!mounted.current) {
      mounted.current = true;
      return;
    }
    heading.current?.focus();
  }, [active, open]);

  if (steps.length === 0) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={title}
        description={description}
        dismissible={!pending}
        className={cn(wide && 'max-w-3xl')}
        footer={
          <>
            <Button onClick={() => onOpenChange(false)} disabled={pending}>
              {t.common.cancel}
            </Button>
            {current > 0 && (
              <Button onClick={() => setActive(current - 1)} disabled={pending}>
                {t.common.back}
              </Button>
            )}
            {last ? (
              <Button variant="primary" loading={pending} disabled={blocked} onClick={onSubmit}>
                {finishLabel}
              </Button>
            ) : (
              <Button variant="primary" disabled={blocked} onClick={() => setActive(current + 1)}>
                {t.wizard.next}
              </Button>
            )}
          </>
        }
      >
        <ol aria-label={t.wizard.progress} className="mb-4 flex flex-wrap items-center gap-2">
          {steps.map((step, index) => {
            const done = index < current;
            return (
              <li key={step.key} className="flex items-center gap-2">
                <span
                  aria-hidden
                  className={cn(
                    'grid size-5 shrink-0 place-items-center rounded-full border text-[11px] font-medium',
                    index === current && 'border-accent bg-accent text-accent-ink',
                    done && 'border-accent bg-accent-soft text-accent',
                    index > current && 'border-line-strong text-muted',
                  )}
                >
                  {done ? <Check className="size-3" /> : index + 1}
                </span>
                <span
                  className={cn(
                    'text-xs',
                    index === current ? 'font-medium text-ink' : 'text-muted',
                  )}
                >
                  {step.title}
                </span>
                {index < steps.length - 1 && (
                  <span aria-hidden className="h-px w-4 bg-line-strong" />
                )}
              </li>
            );
          })}
        </ol>
        <p className="mb-3 text-xs text-faint">
          {interpolate(t.wizard.step, { current: current + 1, total: steps.length })}
        </p>
        {steps.map((step, index) => (
          <section
            key={step.key}
            hidden={index !== current}
            aria-labelledby={`wizard-step-${step.key}`}
          >
            <h3
              id={`wizard-step-${step.key}`}
              ref={index === current ? heading : undefined}
              tabIndex={-1}
              className="text-sm font-semibold outline-none"
            >
              {step.title}
            </h3>
            {step.description && <p className="mt-0.5 text-xs text-muted">{step.description}</p>}
            <div className="mt-3">{step.content}</div>
          </section>
        ))}
      </DialogContent>
    </Dialog>
  );
}
