import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { ChevronDown, KeyRound, LogOut, UserRound } from 'lucide-react';
import { toast } from 'sonner';
import { logout } from '../api/generated/session/session';
import { Button } from '../components/ui/button';
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownLabel,
  DropdownSeparator,
  DropdownTrigger,
} from '../components/ui/dropdown';
import { errorMessage, useLocale } from '../i18n';

export function UserMenu({ username }: { username: string }) {
  const { t } = useLocale();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const signOut = async () => {
    try {
      await logout();
      queryClient.clear();
      await navigate({ to: '/login' });
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };
  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <Button variant="ghost" aria-label={t.app.userMenu}>
          <UserRound aria-hidden />
          <span className="hidden max-w-32 truncate sm:inline">{username}</span>
          <ChevronDown aria-hidden className="hidden text-faint sm:block" />
        </Button>
      </DropdownTrigger>
      <DropdownContent>
        <DropdownLabel>{username}</DropdownLabel>
        <DropdownSeparator />
        <DropdownItem onSelect={() => void navigate({ to: '/password' })}>
          <KeyRound aria-hidden />
          {t.app.changePassword}
        </DropdownItem>
        <DropdownItem onSelect={() => void signOut()}>
          <LogOut aria-hidden />
          {t.app.signOut}
        </DropdownItem>
      </DropdownContent>
    </Dropdown>
  );
}
