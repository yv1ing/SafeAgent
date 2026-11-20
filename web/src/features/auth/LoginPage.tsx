import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState, type FormEvent } from 'react';
import { getMeQueryKey, login } from '../../api/generated/session/session';
import { Callout } from '../../components/feedback/Callout';
import { Field } from '../../components/form/Field';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { errorMessage, useLocale } from '../../i18n';
import { AuthLayout } from './AuthLayout';

export function LoginPage() {
  const { t } = useLocale();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const signIn = useMutation({
    mutationFn: () => login({ username, password }),
    onSuccess: async (session) => {
      queryClient.clear();
      queryClient.setQueryData(getMeQueryKey(), session);
      await navigate({ to: session.user.must_change_password ? '/password' : '/' });
    },
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    signIn.mutate();
  };
  return (
    <AuthLayout title={t.auth.signInTitle} description={t.auth.signInHint}>
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label={t.auth.username}>
          {(id) => (
            <Input
              id={id}
              autoComplete="username"
              autoFocus
              required
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          )}
        </Field>
        <Field label={t.auth.password}>
          {(id) => (
            <Input
              id={id}
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          )}
        </Field>
        {signIn.isError && <Callout tone="danger">{errorMessage(t, signIn.error)}</Callout>}
        <Button type="submit" variant="primary" loading={signIn.isPending}>
          {t.auth.signIn}
        </Button>
      </form>
    </AuthLayout>
  );
}
