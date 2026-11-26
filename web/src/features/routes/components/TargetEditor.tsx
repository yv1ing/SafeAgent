import { Plus, X } from 'lucide-react';
import type { ConsoleChannelOptionView } from '../../../api/generated/models';
import { Callout } from '../../../components/feedback/Callout';
import { Button } from '../../../components/ui/button';
import { IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { useLocale } from '../../../i18n';
import {
  eligibleModels,
  firstTarget,
  targetKind,
  validateTargets,
  type DraftTarget,
} from '../targets';

export function TargetEditor({
  targets,
  channels,
  onChange,
}: {
  targets: DraftTarget[];
  channels: ConsoleChannelOptionView[];
  onChange: (targets: DraftTarget[]) => void;
}) {
  const { t } = useLocale();
  const kind = targets[0] ? targetKind(targets[0], channels) : undefined;
  const problem = validateTargets(targets, channels);
  const update = (index: number, patch: Partial<DraftTarget>) =>
    onChange(
      targets.map((target, position) => (position === index ? { ...target, ...patch } : target)),
    );
  const add = () => {
    const next = firstTarget(channels, kind);
    if (next) onChange([...targets, next]);
  };
  const eligibleChannels = kind
    ? channels.filter((channel) => (channel.models ?? []).some((model) => model.kind === kind))
    : channels;
  if (channels.length === 0) return <p className="text-sm text-muted">{t.routes.noChannels}</p>;
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1.5 text-xs font-medium text-muted">{t.routes.targets}</legend>
      {kind && <p className="text-xs text-faint">{t.routes.kindLocked}</p>}
      {targets.map((target, index) => {
        const models = eligibleModels(
          channels.find((channel) => channel.id === target.channel_id),
          kind,
        );
        const model = models.find((entry) => entry.id === target.model_id);
        return (
          <div
            key={index}
            className="grid grid-cols-2 gap-2 rounded-md border border-line p-2 sm:grid-cols-12"
          >
            <Select
              className="col-span-2 sm:col-span-5"
              aria-label={t.routes.channel}
              value={target.channel_id}
              options={eligibleChannels.map((channel) => ({
                value: channel.id,
                label: channel.name,
              }))}
              onValueChange={(channelId) => {
                const next = eligibleModels(
                  channels.find((channel) => channel.id === channelId),
                  kind,
                )[0];
                update(index, {
                  channel_id: channelId,
                  model_id: next?.id ?? '',
                  format: undefined,
                });
              }}
            />
            <Select
              className="col-span-2 sm:col-span-5"
              aria-label={t.routes.model}
              value={target.model_id}
              options={models.map((entry) => ({
                value: entry.id,
                label: `${entry.display_name || entry.upstream_name} · ${t.models.kinds[entry.kind]}`,
              }))}
              onValueChange={(modelId) => update(index, { model_id: modelId, format: undefined })}
            />
            <div className="col-span-2 flex justify-end sm:col-span-2">
              <Button
                size="icon"
                variant="ghost"
                aria-label={t.routes.removeTarget}
                onClick={() => onChange(targets.filter((_, position) => position !== index))}
              >
                <X aria-hidden />
              </Button>
            </div>
            <Select
              className="col-span-2 sm:col-span-4"
              aria-label={t.routes.format}
              value={target.format ?? ''}
              options={[
                { value: '', label: t.routes.formatAuto },
                ...(model?.formats ?? []).map((format) => ({ value: format, label: format })),
              ]}
              onValueChange={(format) => update(index, { format: format || undefined })}
            />
            <IntegerInput
              className="col-span-1 sm:col-span-2"
              aria-label={t.routes.priority}
              value={target.priority}
              onValueChange={(priority) => update(index, { priority })}
            />
            <IntegerInput
              className="col-span-1 sm:col-span-2"
              aria-label={t.routes.weight}
              value={target.weight}
              onValueChange={(weight) => update(index, { weight })}
            />
            <IntegerInput
              className="col-span-2 sm:col-span-4"
              aria-label={t.routes.outputLimit}
              value={target.max_output_tokens ?? 0}
              onValueChange={(max_output_tokens) => update(index, { max_output_tokens })}
            />
          </div>
        );
      })}
      <div className="flex items-center justify-between gap-2">
        <Button size="sm" onClick={add} disabled={eligibleChannels.length === 0}>
          <Plus aria-hidden />
          {t.routes.addTarget}
        </Button>
        <span className="text-xs text-faint">
          {t.routes.priorityHint} · {t.routes.weightHint}
        </span>
      </div>
      {problem && problem !== 'empty' && (
        <Callout tone="danger">{t.routes.targetProblems[problem]}</Callout>
      )}
    </fieldset>
  );
}
