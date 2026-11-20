import { useLocale } from '../../i18n';
import { Button } from '../ui/button';
import { Dialog, DialogContent } from '../ui/dialog';

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  pending,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  pending: boolean;
  onConfirm: () => void;
}) {
  const { t } = useLocale();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={title}
        footer={
          <>
            <Button onClick={() => onOpenChange(false)}>{t.common.cancel}</Button>
            <Button variant="danger" loading={pending} onClick={onConfirm}>
              {confirmLabel}
            </Button>
          </>
        }
      >
        <p className="text-sm text-muted">{description}</p>
      </DialogContent>
    </Dialog>
  );
}
