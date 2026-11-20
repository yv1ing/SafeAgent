import { useId, type FormEvent, type ReactNode } from 'react';
import { useLocale } from '../../i18n';
import { cn } from '../../lib/cn';
import { Button } from '../ui/button';
import { Dialog, DialogContent } from '../ui/dialog';

export function FormDialog({
  open,
  onOpenChange,
  title,
  description,
  submitLabel,
  pending,
  disabled,
  onSubmit,
  children,
  wide,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  submitLabel?: string;
  pending: boolean;
  disabled?: boolean;
  onSubmit: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  const { t } = useLocale();
  const formId = useId();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!pending && !disabled) onSubmit();
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={title}
        description={description}
        className={cn(wide && 'max-w-3xl')}
        footer={
          <>
            <Button onClick={() => onOpenChange(false)}>{t.common.cancel}</Button>
            <Button
              type="submit"
              form={formId}
              variant="primary"
              loading={pending}
              disabled={disabled}
            >
              {submitLabel ?? t.common.save}
            </Button>
          </>
        }
      >
        <form id={formId} onSubmit={submit} className="flex flex-col gap-4">
          {children}
        </form>
      </DialogContent>
    </Dialog>
  );
}
