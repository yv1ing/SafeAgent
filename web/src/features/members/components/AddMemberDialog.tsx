import { useState } from 'react';
import type { ConsoleMemberView } from '../../../api/generated/models';
import { useListUsers } from '../../../api/generated/users/users';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Select } from '../../../components/ui/select';
import { useLocale } from '../../../i18n';

export function AddMemberDialog({
  members,
  pending,
  onSubmit,
  onClose,
}: {
  members: ConsoleMemberView[];
  pending: boolean;
  onSubmit: (userId: string) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const users = useListUsers();
  const [userId, setUserId] = useState('');
  const candidates = (users.data ?? []).filter(
    (user) => !user.disabled && !members.some((member) => member.user_id === user.id),
  );
  return (
    <FormDialog
      open
      onOpenChange={(open) => !open && onClose()}
      title={t.members.add}
      submitLabel={t.members.add}
      pending={pending}
      disabled={userId === ''}
      onSubmit={() => onSubmit(userId)}
    >
      <Field
        label={t.members.user}
        hint={users.isSuccess && candidates.length === 0 ? t.members.allAdded : undefined}
      >
        {(id) => (
          <Select
            id={id}
            value={userId}
            onValueChange={setUserId}
            placeholder={t.members.user}
            options={candidates.map((user) => ({ value: user.id, label: user.username }))}
          />
        )}
      </Field>
    </FormDialog>
  );
}
