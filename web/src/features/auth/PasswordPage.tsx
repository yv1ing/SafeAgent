import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { changePassword, getMeQueryKey, useMe } from '../../api/generated/session/session';
import { Callout } from '../../components/feedback/Callout';
import { Field } from '../../components/form/Field';
import { Button, buttonClass } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { errorMessage, useLocale } from '../../i18n';
import { AuthLayout } from './AuthLayout';

export function PasswordPage() {
  const { t } = useLocale();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const forced = useMe().data?.user.must_change_password ?? false;
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const mismatch = confirm !== '' && confirm !== next;
  const update = useMutation({
    mutationFn: () => changePassword({ current, next }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: getMeQueryKey() });
      toast.success(t.auth.updated);
      await navigate({ to: '/' });
    },
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!mismatch) update.mutate();
  };
  const fields = [
    { label: t.auth.current, value: current, set: setCurrent, autoComplete: 'current-password' },
    { label: t.auth.next, value: next, set: setNext, autoComplete: 'new-password' },
    { label: t.auth.confirm, value: confirm, set: setConfirm, autoComplete: 'new-password' },
  ];
  return (
    <AuthLayout
      title={t.auth.passwordTitle}
      description={forced ? t.auth.passwordForced : t.auth.passwordHint}
    >
      <form onSubmit={submit} className="flex flex-col gap-4">
        {fields.map((field, index) => (
          <Field
            key={field.label}
            label={field.label}
            hint={index === 1 && forced ? t.auth.passwordHint : undefined}
          >
            {(id) => (
              <Input
                id={id}
                type="password"
                required
                autoFocus={index === 0}
                autoComplete={field.autoComplete}
                aria-invalid={index === 2 && mismatch}
                value={field.value}
                onChange={(event) => field.set(event.target.value)}
              />
            )}
          </Field>
        ))}
        {(mismatch || update.isError) && (
          <Callout tone="danger">
            {mismatch ? t.auth.mismatch : errorMessage(t, update.error)}
          </Callout>
        )}
        <div className="flex gap-2">
          {!forced && (
            <Link to="/" className={buttonClass('secondary')}>
              {t.common.back}
            </Link>
          )}
          <Button type="submit" variant="primary" className="flex-1" loading={update.isPending}>
            {t.auth.update}
          </Button>
        </div>
      </form>
    </AuthLayout>
  );
}
