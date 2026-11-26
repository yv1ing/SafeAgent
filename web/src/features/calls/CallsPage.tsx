import { useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { Activity, SearchX } from 'lucide-react';
import { useCallback } from 'react';
import { useListCalls } from '../../api/generated/calls/calls';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Pager } from '../../components/data/Pager';
import { QueryState } from '../../components/feedback/QueryState';
import { TableSkeleton } from '../../components/data/DataTable';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Sheet } from '../../components/ui/sheet';
import { interpolate, useLocale } from '../../i18n';
import { useCursorPages } from '../../lib/cursor';
import { shortId } from '../../lib/format';
import { useMediaQuery } from '../../lib/media';
import { CallDetail } from './components/CallDetail';
import { CallFilterBar } from './components/CallFilterBar';
import { CallList } from './components/CallList';
import { filtersOf, hasFilters, listParams, type CallFilters, type CallsSearch } from './search';

export function CallsPage() {
  const { t } = useLocale();
  const { projectId } = useParams({ from: '/app/projects/$projectId/calls' });
  const search = useSearch({ from: '/app/projects/$projectId/calls' }) as CallsSearch;
  const navigate = useNavigate();
  const wide = useMediaQuery('(min-width: 80rem)');
  const filters = filtersOf(search);
  const params = listParams(filters);
  const pages = useCursorPages(JSON.stringify({ projectId, ...params }));
  const calls = useListCalls(projectId, { ...params, ...pages.params });
  const setFilters = useCallback(
    (next: CallFilters) =>
      void navigate({
        to: '/projects/$projectId/calls',
        params: { projectId },
        search: { ...next, call: search.call },
      }),
    [navigate, projectId, search.call],
  );
  const select = (call?: string) =>
    void navigate({
      to: '/projects/$projectId/calls',
      params: { projectId },
      search: { ...filters, call },
    });
  const detail = search.call && (
    <CallDetail key={search.call} projectId={projectId} callId={search.call} />
  );

  return (
    <Page>
      <PageHeader title={t.calls.title} description={t.calls.description} />
      <Card className="p-3">
        <CallFilterBar
          projectId={projectId}
          filters={filters}
          onChange={setFilters}
          auxiliary={Boolean(filters.include_auxiliary)}
          onAuxiliaryChange={(enabled) =>
            setFilters({ ...filters, include_auxiliary: enabled || undefined })
          }
        />
      </Card>
      <div className="grid items-start gap-4 xl:grid-cols-5">
        <Card className="xl:col-span-2">
          <QueryState query={calls} loading={<TableSkeleton rows={10} />}>
            {(page) =>
              page.items.length === 0 ? (
                hasFilters(filters) ? (
                  <Empty
                    icon={SearchX}
                    title={t.common.noResults}
                    description={t.common.noResultsHint}
                    action={<Button onClick={() => setFilters({})}>{t.common.clearFilters}</Button>}
                  />
                ) : (
                  <Empty
                    icon={Activity}
                    title={t.calls.emptyTitle}
                    description={t.calls.emptyHint}
                  />
                )
              ) : (
                <>
                  <CallList calls={page.items} selectedId={search.call} onSelect={select} />
                  <Pager
                    page={pages.page}
                    hasPrevious={pages.hasPrevious}
                    hasNext={page.next !== undefined}
                    onPrevious={pages.previous}
                    onNext={() => page.next && pages.next(page.next)}
                  />
                </>
              )
            }
          </QueryState>
        </Card>
        {wide && (
          <Card className="sticky top-20 col-span-3 max-h-[calc(100dvh-6rem)] overflow-y-auto">
            {detail || (
              <p className="px-4 py-16 text-center text-sm text-muted">{t.calls.selectHint}</p>
            )}
          </Card>
        )}
      </div>
      {!wide && (
        <Sheet
          open={Boolean(search.call)}
          onOpenChange={(open) => !open && select(undefined)}
          title={interpolate(t.call.title, { id: shortId(search.call ?? '') })}
        >
          {detail}
        </Sheet>
      )}
    </Page>
  );
}
