import { Link, Outlet } from '@tanstack/react-router';
import { FolderKanban } from 'lucide-react';
import { Page } from '../components/layout/Page';
import { Empty } from '../components/ui/empty';
import { Button, buttonClass } from '../components/ui/button';
import { Skeleton } from '../components/ui/skeleton';
import { errorMessage, useLocale } from '../i18n';
import { useProject } from './context';

export function ProjectScope() {
  const { t } = useLocale();
  const { query } = useProject();

  if (query.isPending) {
    return (
      <Page>
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-24" />
        <Skeleton className="h-64" />
      </Page>
    );
  }

  if (query.isError) {
    return (
      <Page>
        <Empty
          icon={FolderKanban}
          title={t.projects.unavailableTitle}
          description={errorMessage(t, query.error)}
          action={
            <div className="flex flex-wrap items-center justify-center gap-2">
              <Button onClick={() => void query.refetch()}>{t.common.retry}</Button>
              <Link to="/projects" className={buttonClass()}>
                {t.app.allProjects}
              </Link>
            </div>
          }
        />
      </Page>
    );
  }

  return <Outlet />;
}
