import { Plus, X } from 'lucide-react';
import type {
  ConsoleChannelModelOption,
  ConsoleChannelOption,
  ConsoleRouteTarget,
} from '../../../api/generated/models';
import { Button } from '../../../components/ui/button';
import { IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { useLocale } from '../../../i18n';

export function TargetEditor({
  targets,
  channels,
  onChange,
}: {
  targets: ConsoleRouteTarget[];
  channels: ConsoleChannelOption[];
  onChange: (targets: ConsoleRouteTarget[]) => void;
}) {
  const { t } = useLocale();
  const modelOf = (channelID: string, modelID: string): ConsoleChannelModelOption | undefined =>
    channels
      .find((channel) => channel.id === channelID)
      ?.models?.find((model) => model.id === modelID);
  const kind = modelOf(targets[0]?.channel_id ?? '', targets[0]?.model_id ?? '')?.kind;
  const eligibleChannels = kind
    ? channels.filter((channel) => channel.models?.some((model) => model.kind === kind))
    : channels;
  const eligibleModels = (channelID: string) => {
    const models = channels.find((channel) => channel.id === channelID)?.models ?? [];
    return kind ? models.filter((model) => model.kind === kind) : models;
  };
  const update = (index: number, patch: Partial<ConsoleRouteTarget>) =>
    onChange(
      targets.map((target, position) => (position === index ? { ...target, ...patch } : target)),
    );
  const add = () => {
    const channel = eligibleChannels.find((entry) => (entry.models?.length ?? 0) > 0);
    const model = channel?.models?.[0];
    if (!channel || !model) return;
    onChange([...targets, { channel_id: channel.id, model_id: model.id, priority: 0, weight: 1 }]);
  };
  if (channels.length === 0) return <p className="text-sm text-muted">{t.routes.noChannels}</p>;
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1.5 text-xs font-medium text-muted">{t.routes.targets}</legend>
      {kind && <p className="text-xs text-faint">{t.routes.kindLocked}</p>}
      <div className="grid grid-cols-12 gap-2 text-xs text-faint" aria-hidden>
        <span className="col-span-5">{t.routes.channel}</span>
        <span className="col-span-5">{t.routes.model}</span>
        <span className="col-span-2" />
        <span className="col-span-4">{t.routes.priorityHint}</span>
        <span className="col-span-4">{t.routes.weightHint}</span>
        <span className="col-span-4">{t.routes.outputLimit}</span>
      </div>
      {targets.map((target, index) => {
        const models = eligibleModels(target.channel_id);
        return (
          <div
            key={index}
            className="grid grid-cols-12 items-center gap-2 rounded-md border border-line p-2"
          >
            <Select
              className="col-span-5"
              aria-label={t.routes.channel}
              value={target.channel_id}
              options={eligibleChannels.map((channel) => ({
                value: channel.id,
                label: `${channel.name} · ${(channel.formats ?? []).join(', ')}`,
              }))}
              onValueChange={(channel_id) => {
                const next = eligibleModels(channel_id)[0];
                update(index, { channel_id, model_id: next?.id ?? '' });
              }}
            />
            <Select
              className="col-span-5"
              aria-label={t.routes.model}
              value={target.model_id}
              options={models.map((model) => ({
                value: model.id,
                label: `${model.display_name || model.name} · ${t.models.kinds[model.kind]}`,
              }))}
              onValueChange={(model_id) => update(index, { model_id })}
            />
            <div className="col-span-2 flex justify-end">
              <Button
                size="icon"
                variant="ghost"
                aria-label={t.routes.removeTarget}
                onClick={() => onChange(targets.filter((_, position) => position !== index))}
              >
                <X aria-hidden />
              </Button>
            </div>
            <IntegerInput
              className="col-span-4"
              aria-label={t.routes.priority}
              value={target.priority}
              onValueChange={(priority) => update(index, { priority })}
            />
            <IntegerInput
              className="col-span-4"
              aria-label={t.routes.weight}
              min={1}
              value={target.weight}
              onValueChange={(weight) => update(index, { weight })}
            />
            <IntegerInput
              className="col-span-4"
              aria-label={t.routes.outputLimit}
              value={target.max_output_tokens ?? 0}
              onValueChange={(max_output_tokens) => update(index, { max_output_tokens })}
            />
          </div>
        );
      })}
      <div>
        <Button size="sm" onClick={add} disabled={eligibleChannels.length === 0}>
          <Plus aria-hidden />
          {t.routes.addTarget}
        </Button>
      </div>
    </fieldset>
  );
}
