import type { ConsoleImpactView } from '../../../api/generated/models';
import { Dialog, DialogContent } from '../../../components/ui/dialog';
import { Button } from '../../../components/ui/button';
import { ImpactDialog } from '../../../components/form/ImpactDialog';
import { errorMessage, useLocale } from '../../../i18n';
import { impactReport, type ReplacementOption } from '../impact';

export function DeleteImpactDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  pending,
  impact,
  loading,
  error,
  projectNames,
  replacements,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  confirmLabel: string;
  pending: boolean;
  impact: ConsoleImpactView | undefined;
  loading: boolean;
  error: unknown;
  projectNames: Map<string, string>;
  replacements: ReplacementOption[];
  onConfirm: (replacementId?: string) => void;
}) {
  const { t } = useLocale();
  if (!open) return null;
  if (loading || !impact) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent
          title={title}
          footer={<Button onClick={() => onOpenChange(false)}>{t.common.cancel}</Button>}
        >
          {loading ? (
            <p className="text-sm text-muted">{t.common.loading}</p>
          ) : (
            <p className="text-sm text-muted">
              {t.common.loadFailed} {error ? errorMessage(t, error) : ''}
            </p>
          )}
        </DialogContent>
      </Dialog>
    );
  }
  const report = impactReport(impact, projectNames, replacements);
  return (
    <ImpactDialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      report={report}
      confirmLabel={confirmLabel}
      pending={pending}
      onConfirm={(label) =>
        onConfirm(replacements.find((replacement) => replacement.label === label)?.id)
      }
    />
  );
}
