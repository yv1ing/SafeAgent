import { useNavigate } from '@tanstack/react-router';
import { Trash2 } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';
import type {
  ConsoleDecisionModelView,
  ConsoleProjectView,
  ConsoleUpdateDecisionModelInputKind,
} from '../../api/generated/models';
import {
  useGetDecisionModel,
  useUpdateDecisionModel,
} from '../../api/generated/decision-model/decision-model';
import { useCurrentUser, useProject, useProjectId } from '../../app/context';
import { Callout } from '../../components/feedback/Callout';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { Field } from '../../components/form/Field';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { DecimalInput, Input, IntegerInput } from '../../components/ui/input';
import { Select } from '../../components/ui/select';
import { Skeleton } from '../../components/ui/skeleton';
import { Switch } from '../../components/ui/switch';
import { errorMessage, interpolate, useLocale } from '../../i18n';
import { ProjectFields, type ProjectDraft } from './components/ProjectFields';
import { useProjectMutations, useUpdateProject } from './hooks';

function draftOf(project: ConsoleProjectView): ProjectDraft {
  return {
    name: project.name,
    retention_days: project.retention_days,
    requests_per_minute: project.requests_per_minute,
    concurrency: project.concurrency,
    daily_token_quota: project.daily_token_quota,
    default_max_output_tokens: project.default_max_output_tokens ?? 0,
  };
}

function ProjectSettingsForm({ project }: { project: ConsoleProjectView }) {
  const { t } = useLocale();
  const user = useCurrentUser();
  const update = useUpdateProject(project.id);
  const [draft, setDraft] = useState(() => draftOf(project));
  const original = draftOf(project);
  const dirty = JSON.stringify(draft) !== JSON.stringify(original);
  const limitsReadOnly = !user.administrator;

  return (
    <div className="flex flex-col gap-5">
      {limitsReadOnly && <Callout tone="info">{t.projects.limitsAdminOnly}</Callout>}
      <Card>
        <CardHeader title={t.projects.general} description={t.projects.generalHint} />
        <div className="p-4">
          <ProjectFields draft={draft} onChange={setDraft} limitsReadOnly={limitsReadOnly} />
        </div>
        <div className="flex items-center justify-end gap-2 border-t bg-sunken/40 px-4 py-3">
          <Button disabled={!dirty || update.isPending} onClick={() => setDraft(original)}>
            {t.common.cancel}
          </Button>
          <Button
            variant="primary"
            loading={update.isPending}
            disabled={!dirty}
            onClick={() => update.mutate({ ...draft, version: project.version })}
          >
            {t.common.save}
          </Button>
        </div>
      </Card>
    </div>
  );
}

function DecisionModelForm({ projectId }: { projectId: string }) {
  const { t } = useLocale();
  const query = useGetDecisionModel(projectId);
  const update = useUpdateDecisionModel();
  const [draft, setDraft] = useState<ConsoleDecisionModelView | null>(null);
  const [apiKey, setApiKey] = useState('');
  const current = draft ?? query.data;
  if (query.isPending) {
    return (
      <Card>
        <CardHeader title={t.projects.decisionModel} description={t.projects.decisionModelHint} />
        <Skeleton className="m-4 h-40" />
      </Card>
    );
  }
  if (query.isError || !current) {
    return (
      <Card>
        <CardHeader title={t.projects.decisionModel} />
        <p className="p-4 text-sm text-danger">{errorMessage(t, query.error)}</p>
      </Card>
    );
  }
  const set = <K extends keyof ConsoleDecisionModelView>(
    key: K,
    value: ConsoleDecisionModelView[K],
  ) => setDraft({ ...current, [key]: value });
  return (
    <Card>
      <CardHeader title={t.projects.decisionModel} description={t.projects.decisionModelHint} />
      <div className="grid gap-3 p-4 sm:grid-cols-2">
        <div className="flex items-center justify-between gap-3 text-sm">
          <span>{t.projects.decisionModelEnabled}</span>
          <Switch
            aria-label={t.projects.decisionModelEnabled}
            checked={current.enabled}
            onCheckedChange={(enabled) => set('enabled', enabled)}
          />
        </div>
        <div className="flex items-center justify-between gap-3 text-sm">
          <span>{t.projects.useRaw}</span>
          <Switch
            aria-label={t.projects.useRaw}
            checked={current.use_raw}
            onCheckedChange={(use_raw) => set('use_raw', use_raw)}
          />
        </div>
        <div className="flex items-center justify-between gap-3 text-sm">
          <span>{t.projects.allowPrivate}</span>
          <Switch
            aria-label={t.projects.allowPrivate}
            checked={current.allow_private}
            onCheckedChange={(allow_private) => set('allow_private', allow_private)}
          />
        </div>
        <Field label={t.projects.provider}>
          {(id) => (
            <Select
              id={id}
              value={current.kind}
              onValueChange={(kind) => set('kind', kind as ConsoleUpdateDecisionModelInputKind)}
              options={[
                { value: 'typesafe', label: 'TypeSafe Jev' },
                { value: 'openai', label: 'OpenAI-compatible' },
              ]}
            />
          )}
        </Field>
        <Field label={t.projects.endpoint} hint={t.projects.endpointHint}>
          {(id) => (
            <Input
              id={id}
              value={current.endpoint}
              onChange={(event) => set('endpoint', event.target.value)}
            />
          )}
        </Field>
        <Field label={t.projects.model}>
          {(id) => (
            <Input
              id={id}
              value={current.model}
              onChange={(event) => set('model', event.target.value)}
            />
          )}
        </Field>
        <Field
          label={t.projects.apiKey}
          hint={current.api_key_set ? t.projects.apiKeySet : undefined}
        >
          {(id) => (
            <Input
              id={id}
              type="password"
              autoComplete="new-password"
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
            />
          )}
        </Field>
        <Field label={t.projects.timeout}>
          {(id) => (
            <IntegerInput
              id={id}
              min={1}
              value={current.timeout_seconds}
              onValueChange={(timeout_seconds) => set('timeout_seconds', timeout_seconds)}
            />
          )}
        </Field>
        <Field label={t.projects.dailyBudget}>
          {(id) => (
            <IntegerInput
              id={id}
              value={current.daily_token_budget}
              onValueChange={(daily_token_budget) => set('daily_token_budget', daily_token_budget)}
            />
          )}
        </Field>
        <Field label={t.projects.authorizeBelow}>
          {(id) => (
            <DecimalInput
              id={id}
              value={current.authorize_below}
              onValueChange={(authorize_below) => set('authorize_below', authorize_below)}
            />
          )}
        </Field>
      </div>
      <div className="flex justify-end gap-2 border-t px-4 py-3">
        <Button
          loading={update.isPending}
          onClick={() =>
            update.mutate(
              {
                projectId,
                data: {
                  enabled: current.enabled,
                  kind: current.kind,
                  endpoint: current.endpoint,
                  model: current.model,
                  api_key: apiKey || undefined,
                  timeout_seconds: current.timeout_seconds,
                  allow_private: current.allow_private,
                  use_raw: current.use_raw,
                  daily_token_budget: current.daily_token_budget,
                  authorize_below: current.authorize_below,
                },
              },
              {
                onSuccess: async () => {
                  setDraft(null);
                  setApiKey('');
                  toast.success(t.projects.decisionModelSaved);
                  await query.refetch();
                },
                onError: (error) => toast.error(errorMessage(t, error)),
              },
            )
          }
        >
          {t.common.save}
        </Button>
      </div>
    </Card>
  );
}

function DeleteProject({ project }: { project: ConsoleProjectView }) {
  const { t } = useLocale();
  const navigate = useNavigate();
  const projectId = useProjectId();
  const mutations = useProjectMutations();
  const [confirming, setConfirming] = useState(false);
  return (
    <Card className="border-danger/30">
      <CardHeader title={t.projects.dangerZone} description={t.projects.dangerHint} />
      <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p className="text-sm font-medium">{t.projects.deleteTitleShort}</p>
          <p className="mt-0.5 text-sm text-muted">{t.projects.deleteHint}</p>
        </div>
        <Button variant="danger" onClick={() => setConfirming(true)}>
          <Trash2 aria-hidden />
          {t.projects.deleteProject}
        </Button>
      </div>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={interpolate(t.projects.deleteTitle, { name: project.name })}
        description={t.common.irreversible}
        confirmLabel={t.common.delete}
        pending={mutations.remove.isPending}
        onConfirm={() =>
          mutations.remove.mutate(projectId, {
            onSuccess: async () => {
              setConfirming(false);
              await navigate({ to: '/projects' });
            },
          })
        }
      />
    </Card>
  );
}

export function ProjectSettingsPage() {
  const { t } = useLocale();
  const user = useCurrentUser();
  const { project } = useProject();
  if (!project) return null;
  return (
    <Page>
      <PageHeader title={t.common.settings} description={t.projects.settingsDescription} />
      <ProjectSettingsForm key={project.version} project={project} />
      {user.administrator && <DecisionModelForm projectId={project.id} />}
      {user.administrator && <DeleteProject project={project} />}
    </Page>
  );
}
