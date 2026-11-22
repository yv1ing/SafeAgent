import { useMemo, useState } from 'react';
import { toast } from 'sonner';
import {
  useGetActivePolicy,
  useListPolicies,
  useRollbackPolicy,
  useSavePolicy,
  useValidatePolicy,
} from '../../api/generated/policies/policies';
import { useCurrentUser, useProjectId } from '../../app/context';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { Skeleton } from '../../components/ui/skeleton';
import { errorMessage, useLocale } from '../../i18n';

function pretty(document: unknown) {
  return JSON.stringify(document, null, 2);
}

export function PoliciesPage() {
  const { t, locale } = useLocale();
  const projectId = useProjectId();
  const user = useCurrentUser();
  const active = useGetActivePolicy(projectId);
  const versions = useListPolicies(projectId);
  const save = useSavePolicy();
  const validate = useValidatePolicy();
  const rollback = useRollbackPolicy();
  const [draft, setDraft] = useState<string | null>(null);
  const editor = useMemo(
    () => draft ?? (active.data ? pretty(active.data.document) : ''),
    [draft, active.data],
  );
  const admin = user.administrator;

  const refresh = async () => {
    await Promise.all([active.refetch(), versions.refetch()]);
  };

  const parseDocument = (): unknown | undefined => {
    try {
      return JSON.parse(editor);
    } catch {
      toast.error(t.policies.invalidJson);
      return undefined;
    }
  };

  return (
    <Page>
      <PageHeader
        title={t.policies.title}
        description={t.policies.description}
        actions={<span className="text-xs text-muted">{t.policies.denyOverrides}</span>}
      />
      <QueryState query={active} loading={<Skeleton className="h-72" />}>
        {() => (
          <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
            <Card>
              <CardHeader
                title={t.policies.editor}
                description={t.policies.documentHint}
                actions={
                  <>
                    {!admin && (
                      <span className="text-xs text-muted">{t.policies.membersReadOnly}</span>
                    )}
                    {draft !== null && (
                      <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
                        {t.policies.discard}
                      </Button>
                    )}
                    {admin && (
                      <Button
                        size="sm"
                        variant="secondary"
                        loading={validate.isPending}
                        onClick={async () => {
                          const document = parseDocument();
                          if (document === undefined) return;
                          try {
                            const result = await validate.mutateAsync({
                              projectId,
                              data: { document },
                            });
                            toast[result.valid ? 'success' : 'error'](
                              result.valid ? t.policies.valid : t.policies.invalid,
                            );
                          } catch (error) {
                            toast.error(errorMessage(t, error));
                          }
                        }}
                      >
                        {t.policies.validate}
                      </Button>
                    )}
                    {admin && (
                      <Button
                        size="sm"
                        loading={save.isPending}
                        onClick={async () => {
                          const document = parseDocument();
                          if (document === undefined) return;
                          try {
                            await save.mutateAsync({
                              projectId,
                              data: { document, note: '' },
                            });
                            setDraft(null);
                            toast.success(t.policies.saved);
                            await refresh();
                          } catch (error) {
                            toast.error(errorMessage(t, error));
                          }
                        }}
                      >
                        {t.policies.save}
                      </Button>
                    )}
                  </>
                }
              />
              <textarea
                aria-label={t.policies.document}
                spellCheck={false}
                readOnly={!admin}
                value={editor}
                onChange={(event) => setDraft(event.target.value)}
                className="min-h-[28rem] w-full resize-y bg-surface p-3 font-mono text-xs leading-relaxed outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
              <p className="border-t px-3 py-2 text-xs text-muted">{t.policies.fieldsHint}</p>
            </Card>
            <Card>
              <CardHeader title={t.policies.versions} />
              {versions.isError && (
                <p className="p-4 text-sm text-danger">{errorMessage(t, versions.error)}</p>
              )}
              <ul className="divide-y">
                {(versions.data?.items ?? []).map((version) => {
                  const current = version.version === active.data?.version;
                  return (
                    <li key={version.id} className="flex flex-col gap-1 px-3 py-2 text-xs">
                      <div className="flex items-center gap-2">
                        <span className="font-mono font-medium">v{version.version}</span>
                        <Badge tone={current ? 'success' : 'neutral'}>
                          {current ? t.policies.active : t.policies.historical}
                        </Badge>
                        <span className="ml-auto text-faint">
                          {new Date(version.created_at).toLocaleString(locale)}
                        </span>
                      </div>
                      <span className="truncate font-mono text-faint">{version.hash}</span>
                      {version.note && <span className="text-muted">{version.note}</span>}
                      <Button
                        size="sm"
                        variant="ghost"
                        className="self-start"
                        disabled={!admin || current}
                        loading={rollback.isPending}
                        onClick={async () => {
                          try {
                            await rollback.mutateAsync({ projectId, version: version.version });
                            setDraft(null);
                            toast.success(t.policies.restored);
                            await refresh();
                          } catch (error) {
                            toast.error(errorMessage(t, error));
                          }
                        }}
                      >
                        {t.policies.rollback}
                      </Button>
                    </li>
                  );
                })}
              </ul>
            </Card>
          </div>
        )}
      </QueryState>
    </Page>
  );
}
