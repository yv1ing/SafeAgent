import { useState } from 'react';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Input } from '../../../components/ui/input';
import { useLocale } from '../../../i18n';

export function ProjectDialog({
  pending,
  onSubmit,
  onClose,
}: {
  pending: boolean;
  onSubmit: (name: string) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const [name, setName] = useState('');
  return (
    <FormDialog
      open
      onOpenChange={(open) => !open && onClose()}
      title={t.projects.add}
      description={t.projects.createHint}
      submitLabel={t.common.create}
      pending={pending}
      onSubmit={() => onSubmit(name)}
    >
      <Field label={t.common.name}>
        {(id) => (
          <Input
            id={id}
            required
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        )}
      </Field>
    </FormDialog>
  );
}
