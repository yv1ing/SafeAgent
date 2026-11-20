import { useState } from 'react';
import type {
  ConsoleDetectorView,
  ConsoleRuleInput,
  ConsoleRuleInputAction,
  ConsoleRuleInputKind,
  ConsoleRuleView,
} from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Input } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { RuleTester } from './RuleTester';

const kinds: ConsoleRuleInputKind[] = ['builtin', 'regex'];
const actions: ConsoleRuleInputAction[] = ['replace', 'block', 'alert'];

export function ruleInput(rule: ConsoleRuleView): ConsoleRuleInput {
  return {
    kind: rule.kind,
    detector: rule.detector,
    pattern: rule.pattern,
    capture: rule.capture,
    action: rule.action,
    label: rule.label,
    enabled: rule.enabled,
    version: rule.version,
  };
}

export function RuleDialog({
  projectId,
  rule,
  detectors,
  administrator,
  pending,
  onSubmit,
  onClose,
}: {
  projectId: string;
  rule: ConsoleRuleView | null;
  detectors: ConsoleDetectorView[];
  administrator: boolean;
  pending: boolean;
  onSubmit: (input: ConsoleRuleInput) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const actionOptions = administrator ? actions : actions.filter((action) => action !== 'block');
  const [draft, setDraft] = useState<ConsoleRuleInput>(
    rule
      ? ruleInput(rule)
      : {
          kind: 'builtin',
          detector: detectors[0]?.id ?? '',
          action: 'replace',
          label: '',
          enabled: true,
        },
  );
  const set = (patch: Partial<ConsoleRuleInput>) => setDraft({ ...draft, ...patch });
  const submitted: ConsoleRuleInput =
    draft.kind === 'builtin'
      ? { ...draft, pattern: undefined, capture: undefined }
      : { ...draft, detector: undefined };
  return (
    <FormDialog
      open
      wide
      onOpenChange={(open) => !open && onClose()}
      title={rule ? t.redaction.edit : t.redaction.add}
      submitLabel={rule ? t.common.save : t.common.create}
      pending={pending}
      onSubmit={() => onSubmit(submitted)}
    >
      <div className="grid gap-4 md:grid-cols-2">
        <div className="flex flex-col gap-4">
          <Field label={t.redaction.kind}>
            {(id) => (
              <Select
                id={id}
                value={draft.kind}
                options={kinds.map((kind) => ({ value: kind, label: t.redaction.kinds[kind] }))}
                onValueChange={(kind) => set({ kind: kind as ConsoleRuleInputKind })}
              />
            )}
          </Field>
          {draft.kind === 'builtin' ? (
            <Field label={t.redaction.detector}>
              {(id) => (
                <Select
                  id={id}
                  value={draft.detector ?? ''}
                  options={detectors.map((detector) => ({
                    value: detector.id,
                    label: detector.secret
                      ? `${detector.label} (${t.redaction.secretDetector})`
                      : detector.label,
                  }))}
                  onValueChange={(detector) => set({ detector })}
                />
              )}
            </Field>
          ) : (
            <>
              <Field label={t.redaction.pattern} hint={t.redaction.patternHint}>
                {(id) => (
                  <Input
                    id={id}
                    required
                    className="font-mono"
                    value={draft.pattern ?? ''}
                    onChange={(event) => set({ pattern: event.target.value })}
                  />
                )}
              </Field>
              <Field label={t.redaction.capture} hint={t.redaction.captureHint}>
                {(id) => (
                  <Input
                    id={id}
                    className="font-mono"
                    value={draft.capture ?? ''}
                    onChange={(event) => set({ capture: event.target.value })}
                  />
                )}
              </Field>
            </>
          )}
          <Field label={t.common.label} hint={t.redaction.labelHint}>
            {(id) => (
              <Input
                id={id}
                className="font-mono"
                value={draft.label ?? ''}
                onChange={(event) => set({ label: event.target.value })}
              />
            )}
          </Field>
          <Field label={t.redaction.action}>
            {(id) => (
              <Select
                id={id}
                value={draft.action}
                options={actionOptions.map((action) => ({
                  value: action,
                  label: t.redaction.actions[action],
                }))}
                onValueChange={(action) => set({ action: action as ConsoleRuleInputAction })}
              />
            )}
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={draft.enabled ?? true}
              onCheckedChange={(enabled) => set({ enabled })}
            />
            {t.common.enabled}
          </label>
        </div>
        <section
          aria-label={t.redaction.tester}
          className="flex flex-col gap-2 rounded-lg border bg-canvas p-3"
        >
          <h3 className="text-sm font-semibold">{t.redaction.tester}</h3>
          <p className="text-xs text-muted">{t.redaction.testerHint}</p>
          <RuleTester projectId={projectId} rule={submitted} />
        </section>
      </div>
    </FormDialog>
  );
}
