import { useState } from 'react';
import type {
  ConsoleAgentKeyView,
  ConsoleCreateKeyInput,
  ConsoleCreateKeyInputRole,
} from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Input, IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { useCurrentUser } from '../../../app/context';

export function KeyDialog({
  agentKey,
  pending,
  onSubmit,
  onClose,
}: {
  agentKey: ConsoleAgentKeyView | null;
  pending: boolean;
  onSubmit: (input: ConsoleCreateKeyInput) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const user = useCurrentUser();
  const [draft, setDraft] = useState<Required<ConsoleCreateKeyInput>>({
    label: agentKey?.label ?? '',
    requests_per_minute: agentKey?.requests_per_minute ?? 0,
    concurrency: agentKey?.concurrency ?? 0,
    trust_level: agentKey?.trust_level ?? 0,
    role: (agentKey?.role as ConsoleCreateKeyInputRole) ?? 'agent',
    enabled: agentKey?.enabled ?? true,
  });
  const set = (patch: Partial<ConsoleCreateKeyInput>) => setDraft({ ...draft, ...patch });
  return (
    <FormDialog
      open
      onOpenChange={(open) => !open && onClose()}
      title={agentKey ? t.keys.edit : t.keys.add}
      submitLabel={agentKey ? t.common.save : t.common.create}
      pending={pending}
      onSubmit={() => onSubmit(draft)}
    >
      <Field label={t.common.label}>
        {(id) => (
          <Input
            id={id}
            required
            autoFocus
            value={draft.label}
            onChange={(event) => set({ label: event.target.value })}
          />
        )}
      </Field>
      <div className="grid grid-cols-2 gap-3">
        <Field label={t.keys.rpm} hint={t.keys.limitHint}>
          {(id) => (
            <IntegerInput
              id={id}
              value={draft.requests_per_minute}
              onValueChange={(requests_per_minute) => set({ requests_per_minute })}
            />
          )}
        </Field>
        <Field label={t.keys.concurrency} hint={t.keys.limitHint}>
          {(id) => (
            <IntegerInput
              id={id}
              value={draft.concurrency}
              onValueChange={(concurrency) => set({ concurrency })}
            />
          )}
        </Field>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field label={t.keys.trustLevel} hint={t.keys.trustHint}>
          {(id) => (
            <IntegerInput
              id={id}
              value={draft.trust_level}
              disabled={!user.administrator}
              onValueChange={(trust_level) => set({ trust_level })}
            />
          )}
        </Field>
        <Field label={t.keys.role}>
          {(id) => (
            <Select
              id={id}
              value={draft.role ?? 'agent'}
              disabled={!user.administrator}
              onValueChange={(role) => set({ role: role as ConsoleCreateKeyInputRole })}
              options={(Object.keys(t.keys.roles) as ConsoleCreateKeyInputRole[]).map((role) => ({
                value: role,
                label: t.keys.roles[role],
              }))}
            />
          )}
        </Field>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <Switch checked={draft.enabled} onCheckedChange={(enabled) => set({ enabled })} />
        {t.common.enabled}
      </label>
    </FormDialog>
  );
}
