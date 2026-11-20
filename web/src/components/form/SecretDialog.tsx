import { useLocale } from '../../i18n';
import { CopyField } from './CopyField';
import { Button } from '../ui/button';
import { Dialog, DialogContent } from '../ui/dialog';

export function SecretDialog({
  secret,
  title,
  description,
  onClose,
}: {
  secret: string | null;
  title: string;
  description: string;
  onClose: () => void;
}) {
  const { t } = useLocale();
  return (
    <Dialog open={secret !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={title}
        description={description}
        dismissible={false}
        footer={
          <Button variant="primary" onClick={onClose}>
            {t.common.secretSaved}
          </Button>
        }
      >
        <CopyField value={secret ?? ''} label={title} />
        <p className="mt-3 text-xs text-warning">{t.common.secretOnce}</p>
      </DialogContent>
    </Dialog>
  );
}
