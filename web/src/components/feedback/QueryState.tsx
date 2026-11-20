import type { UseQueryResult } from '@tanstack/react-query';
import { AlertTriangle } from 'lucide-react';
import type { ReactNode } from 'react';
import { errorMessage, useLocale } from '../../i18n';
import { Button } from '../ui/button';

export function QueryState<T>({
  query,
  loading,
  children,
}: {
  query: UseQueryResult<T, unknown>;
  loading: ReactNode;
  children: (data: T) => ReactNode;
}) {
  const { t } = useLocale();
  if (query.isPending) {
    return (
      <div role="status" aria-label={t.common.loading}>
        {loading}
      </div>
    );
  }
  if (query.isError) {
    return (
      <div role="alert" className="flex flex-col items-center gap-2 px-6 py-12 text-center">
        <AlertTriangle aria-hidden className="size-6 text-danger" strokeWidth={1.5} />
        <p className="text-sm font-medium">{t.common.loadFailed}</p>
        <p className="max-w-sm text-sm text-muted">{errorMessage(t, query.error)}</p>
        <Button size="sm" className="mt-2" onClick={() => void query.refetch()}>
          {t.common.retry}
        </Button>
      </div>
    );
  }
  return <>{children(query.data)}</>;
}
