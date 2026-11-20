import { FileClock } from 'lucide-react';
import { useListProjectOperations } from '../../api/generated/operations/operations';
import { useProjectId } from '../../app/context';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Pager } from '../../components/data/Pager';
import { QueryState } from '../../components/feedback/QueryState';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { TableSkeleton } from '../../components/data/DataTable';
import { useLocale } from '../../i18n';
import { useCursorPages } from '../../lib/cursor';
import { OperationTable } from '../operations/components/OperationTable';

export function ProjectActivityPage() {
  const { t } = useLocale();
  const projectId = useProjectId();
  const pages = useCursorPages(projectId);
  const operations = useListProjectOperations(projectId, pages.params);
  return (
    <Page>
      <PageHeader title={t.nav.activity} description={t.operations.projectDescription} />
      <Card>
        <QueryState query={operations} loading={<TableSkeleton rows={10} />}>
          {(page) => (
            <>
              <OperationTable
                rows={page.items}
                empty={
                  <Empty
                    icon={FileClock}
                    title={t.operations.emptyTitle}
                    description={t.operations.projectEmptyHint}
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
