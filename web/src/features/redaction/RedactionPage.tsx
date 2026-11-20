import { Pencil, Plus, ScanEye } from 'lucide-react';
import { useState } from 'react';
import { useListDetectors } from '../../api/generated/detectors/detectors';
import type { ConsoleRuleView } from '../../api/generated/models';
import { useListRules } from '../../api/generated/rules/rules';
import { useCurrentUser, useProjectId } from '../../app/context';
import { Callout } from '../../components/feedback/Callout';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Select } from '../../components/ui/select';
import { Switch } from '../../components/ui/switch';
import { errorMessage, interpolate, useLocale } from '../../i18n';
import { RuleDialog, ruleInput } from './components/RuleDialog';
import { RuleTester } from './components/RuleTester';
import { useRuleMutations } from './hooks';

const actionTone = { replace: 'accent', block: 'danger', alert: 'warning' } as const;

export function RedactionPage() {
  const { t } = useLocale();
  const projectId = useProjectId();
  const user = useCurrentUser();
  const admin = user.administrator;
  const rules = useListRules(projectId);
  const detectors = useListDetectors();
  const mutations = useRuleMutations(projectId);
  const [editing, setEditing] = useState<ConsoleRuleView | 'new' | null>(null);
  const [deleting, setDeleting] = useState<ConsoleRuleView | null>(null);
  const [testedId, setTestedId] = useState('');
  const tested = rules.data?.find((rule) => rule.id === testedId) ?? rules.data?.[0];
  const detectorLabel = (id: string) =>
    detectors.data?.find((detector) => detector.id === id)?.label ?? id;
  const close = () => setEditing(null);

  return (
    <Page>
      <PageHeader
        title={t.redaction.title}
        description={t.redaction.description}
        actions={
          <Button variant="primary" disabled={!detectors.data} onClick={() => setEditing('new')}>
            <Plus aria-hidden />
            {t.redaction.add}
          </Button>
        }
      />
      {detectors.isError && <Callout tone="danger">{errorMessage(t, detectors.error)}</Callout>}
      <div className="grid items-start gap-4 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <QueryState query={rules} loading={<TableSkeleton />}>
            {(rows) => (
              <DataTable
                rows={rows}
                rowKey={(row) => row.id}
                empty={
                  <Empty
                    icon={ScanEye}
                    title={t.redaction.emptyTitle}
                    description={t.redaction.emptyHint}
                  />
                }
                columns={[
                  {
                    key: 'label',
                    header: t.common.label,
                    className: 'font-mono font-medium',
                    cell: (row) => row.label,
                  },
                  {
                    key: 'match',
                    header: t.redaction.kind,
                    cell: (row) =>
                      row.kind === 'builtin' ? (
                        <span>
                          <span className="text-muted">{t.redaction.kinds.builtin} · </span>
                          {detectorLabel(row.detector)}
                        </span>
                      ) : (
                        <code
                          className="block max-w-xs truncate font-mono text-xs"
                          title={row.pattern}
                        >
                          {row.pattern}
                        </code>
                      ),
                  },
                  {
                    key: 'action',
                    header: t.redaction.action,
                    cell: (row) => (
                      <Badge tone={actionTone[row.action]}>{t.redaction.actions[row.action]}</Badge>
                    ),
                  },
                  {
                    key: 'enabled',
                    header: t.common.enabled,
                    cell: (row) => (
                      <Switch
                        aria-label={`${t.common.enabled}: ${row.label}`}
                        checked={row.enabled}
                        disabled={(!admin && row.action === 'block') || mutations.update.isPending}
                        onCheckedChange={(enabled) =>
                          mutations.update.mutate({
                            ruleId: row.id,
                            input: { ...ruleInput(row), enabled },
                          })
                        }
                      />
                    ),
                  },
                  {
                    key: 'actions',
                    header: t.common.actions,
                    className: 'text-right',
                    cell: (row) => (
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={!admin && row.action === 'block'}
                          onClick={() => setEditing(row)}
                        >
                          <Pencil aria-hidden />
                          {t.common.edit}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="text-danger"
                          disabled={!admin && row.action === 'block'}
                          onClick={() => setDeleting(row)}
                        >
                          {t.common.delete}
                        </Button>
                      </div>
                    ),
                  },
                ]}
              />
            )}
          </QueryState>
        </Card>
        <Card>
          <CardHeader title={t.redaction.tester} description={t.redaction.testerHint} />
          <div className="flex flex-col gap-3 p-4">
            {tested ? (
              <>
                <Select
                  aria-label={t.redaction.testRule}
                  value={tested.id}
                  options={(rules.data ?? []).map((rule) => ({
                    value: rule.id,
                    label: rule.label,
                  }))}
                  onValueChange={setTestedId}
                />
                <RuleTester projectId={projectId} rule={ruleInput(tested)} />
              </>
            ) : (
              <p className="text-sm text-muted">{t.redaction.emptyHint}</p>
            )}
          </div>
        </Card>
      </div>
      {editing && detectors.data && (
        <RuleDialog
          projectId={projectId}
          rule={editing === 'new' ? null : editing}
          detectors={detectors.data}
          administrator={admin}
          pending={mutations.create.isPending || mutations.update.isPending}
          onClose={close}
          onSubmit={(input) =>
            editing === 'new'
              ? mutations.create.mutate(input, { onSuccess: close })
              : mutations.update.mutate({ ruleId: editing.id, input }, { onSuccess: close })
          }
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={interpolate(t.redaction.deleteTitle, { label: deleting?.label ?? '' })}
        description={t.common.irreversible}
        confirmLabel={t.common.delete}
        pending={mutations.remove.isPending}
        onConfirm={() =>
          deleting && mutations.remove.mutate(deleting.id, { onSuccess: () => setDeleting(null) })
        }
      />
    </Page>
  );
}
