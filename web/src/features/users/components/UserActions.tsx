import { MoreHorizontal } from 'lucide-react';
import type { ConsoleUserView } from '../../../api/generated/models';
import { Button } from '../../../components/ui/button';
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownTrigger,
} from '../../../components/ui/dropdown';
import { useLocale } from '../../../i18n';

export function UserActions({
  user,
  onUpdate,
  onResetPassword,
}: {
  user: ConsoleUserView;
  onUpdate: (input: { administrator: boolean; disabled: boolean }) => void;
  onResetPassword: () => void;
}) {
  const { t } = useLocale();
  const current = { administrator: user.administrator, disabled: user.disabled };
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <Button size="icon" variant="ghost" aria-label={`${t.common.actions}: ${user.username}`}>
          <MoreHorizontal aria-hidden />
        </Button>
      </DropdownTrigger>
      <DropdownContent>
        <DropdownItem onSelect={() => onUpdate({ ...current, administrator: !user.administrator })}>
          {user.administrator ? t.users.removeAdmin : t.users.makeAdmin}
        </DropdownItem>
        <DropdownItem onSelect={() => onUpdate({ ...current, disabled: !user.disabled })}>
          {user.disabled ? t.users.enable : t.users.disable}
        </DropdownItem>
        <DropdownItem onSelect={onResetPassword}>{t.users.resetPassword}</DropdownItem>
      </DropdownContent>
    </Dropdown>
  );
}
