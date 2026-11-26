import type { ConsoleRouteView } from '../../../api/generated/models';
import { useLocale } from '../../../i18n';
import { Field } from '../../../components/form/Field';
import { Input, IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';

export interface ProjectDraft {
  name: string;
  retention_days: number;
  requests_per_minute: number;
  concurrency: number;
  daily_token_quota: number;
  default_max_output_tokens: number;
  stream_review_bytes: number;
  stream_review_timeout_ms: number;
  auxiliary_retention_days: number;
  default_route_id: string;
}

export function ProjectFields({
  draft,
  onChange,
  limitsReadOnly = false,
  routes = [],
}: {
  draft: ProjectDraft;
  onChange: (draft: ProjectDraft) => void;
  limitsReadOnly?: boolean;
  routes?: ConsoleRouteView[];
}) {
  const { t } = useLocale();
  const set = (patch: Partial<ProjectDraft>) => onChange({ ...draft, ...patch });
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <Field label={t.common.name} className="sm:col-span-2">
        {(id) => (
          <Input
            id={id}
            required
            value={draft.name}
            onChange={(event) => set({ name: event.target.value })}
          />
        )}
      </Field>
      <Field label={t.projects.retention}>
        {(id) => (
          <IntegerInput
            id={id}
            min={1}
            value={draft.retention_days}
            disabled={limitsReadOnly}
            onValueChange={(retention_days) => set({ retention_days })}
          />
        )}
      </Field>
      <Field label={t.projects.auxiliaryRetention} hint={t.projects.auxiliaryRetentionHint}>
        {(id) => (
          <IntegerInput
            id={id}
            min={0}
            value={draft.auxiliary_retention_days}
            disabled={limitsReadOnly}
            onValueChange={(auxiliary_retention_days) => set({ auxiliary_retention_days })}
          />
        )}
      </Field>
      <Field label={t.projects.defaultRoute} hint={t.projects.defaultRouteHint}>
        {(id) => (
          <Select
            id={id}
            value={draft.default_route_id}
            disabled={limitsReadOnly}
            options={[
              { value: '', label: t.projects.defaultRouteNone },
              ...routes.map((route) => ({ value: route.id, label: route.name })),
            ]}
            onValueChange={(default_route_id) => set({ default_route_id })}
          />
        )}
      </Field>
      <Field label={t.projects.quota} hint={t.projects.limitHint}>
        {(id) => (
          <IntegerInput
            id={id}
            value={draft.daily_token_quota}
            disabled={limitsReadOnly}
            onValueChange={(daily_token_quota) => set({ daily_token_quota })}
          />
        )}
      </Field>
      <Field label={t.projects.defaultMaxOutput} hint={t.projects.defaultMaxOutputHint}>
        {(id) => (
          <IntegerInput
            id={id}
            value={draft.default_max_output_tokens}
            disabled={limitsReadOnly}
            onValueChange={(default_max_output_tokens) => set({ default_max_output_tokens })}
          />
        )}
      </Field>
      <Field label={t.projects.streamReviewBytes} hint={t.projects.streamReviewHint}>
        {(id) => (
          <IntegerInput
            id={id}
            min={0}
            value={draft.stream_review_bytes}
            disabled={limitsReadOnly}
            onValueChange={(stream_review_bytes) => set({ stream_review_bytes })}
          />
        )}
      </Field>
      <Field label={t.projects.streamReviewTimeout} hint={t.projects.streamReviewTimeoutHint}>
        {(id) => (
          <IntegerInput
            id={id}
            min={0}
            value={draft.stream_review_timeout_ms}
            disabled={limitsReadOnly}
            onValueChange={(stream_review_timeout_ms) => set({ stream_review_timeout_ms })}
          />
        )}
      </Field>
      <Field label={t.projects.rpm} hint={t.projects.limitHint}>
        {(id) => (
          <IntegerInput
            id={id}
            value={draft.requests_per_minute}
            disabled={limitsReadOnly}
            onValueChange={(requests_per_minute) => set({ requests_per_minute })}
          />
        )}
      </Field>
      <Field label={t.projects.concurrency} hint={t.projects.limitHint}>
        {(id) => (
          <IntegerInput
            id={id}
            value={draft.concurrency}
            disabled={limitsReadOnly}
            onValueChange={(concurrency) => set({ concurrency })}
          />
        )}
      </Field>
    </div>
  );
}
