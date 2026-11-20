import { useState } from 'react';
import type { ConsoleUserInput } from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Input } from '../../../components/ui/input';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';

export function UserDialog({
  pending,
  onSubmit,
  onClose,
}: {
  pending: boolean;
  onSubmit: (input: ConsoleUserInput) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const [username, setUsername] = useState('');
  const [administrator, setAdministrator] = useState(false);
  return (
    <FormDialog
      open
      onOpenChange={(open) => !open && onClose()}
      title={t.users.add}
      submitLabel={t.common.create}
      pending={pending}
      onSubmit={() => onSubmit({ username, administrator })}
    >
      <Field label={t.users.username} hint={t.errors.invalid_username}>
        {(id) => (
          <Input
            id={id}
            required
            autoFocus
            autoComplete="off"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
          />
        )}
      </Field>
      <label className="flex items-center gap-2 text-sm">
        <Switch checked={administrator} onCheckedChange={setAdministrator} />
        {t.users.administrator}
      </label>
    </FormDialog>
  );
}
