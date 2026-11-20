import { AlertTriangle } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Empty } from '../components/ui/empty';
import { errorMessage, useLocale } from '../i18n';

export function RouteError({ error, reset }: { error: unknown; reset: () => void }) {
  const { t } = useLocale();
  return (
    <div className="flex min-h-72 items-center justify-center px-4 py-12">
      <Empty
        icon={AlertTriangle}
        title={t.common.loadFailed}
        description={errorMessage(t, error)}
        action={<Button onClick={reset}>{t.common.retry}</Button>}
      />
    </div>
  );
}
