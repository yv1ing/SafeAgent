import { FileClock } from 'lucide-react';
import { useListOperationLogs } from '../../api/generated/operation-logs/operation-logs';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Pager } from '../../components/data/Pager';
import { QueryState } from '../../components/feedback/QueryState';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { TableSkeleton } from '../../components/data/DataTable';
import { useLocale } from '../../i18n';
import { useCursorPages } from '../../lib/cursor';
import { OperationTable } from './components/OperationTable';

export function OperationsPage() {
  const { t } = useLocale();
  const pages = useCursorPages('operations');
  const operations = useListOperationLogs(pages.params);
  return (
    <Page>
      <PageHeader title={t.operations.title} description={t.operations.description} />
      <Card>
        <QueryState query={operations} loading={<TableSkeleton rows={10} />}>
          {(page) => (
            <>
              <OperationTable
                rows={page.items}
                showProject
                empty={
                  <Empty
                    icon={FileClock}
                    title={t.operations.emptyTitle}
                    description={t.operations.emptyHint}
                  />
                }
              />
              <Pager
                page={pages.page}
                hasPrevious={pages.hasPrevious}
                hasNext={page.next !== undefined}
                onPrevious={pages.previous}
                onNext={() => page.next && pages.next(page.next)}
              />
            </>
          )}
        </QueryState>
      </Card>
    </Page>
  );
}
