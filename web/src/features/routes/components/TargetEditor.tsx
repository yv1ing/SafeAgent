import { Plus, X } from 'lucide-react';
import type { ConsoleChannelOption, ConsoleRouteTarget } from '../../../api/generated/models';
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
  const update = (index: number, patch: Partial<ConsoleRouteTarget>) =>
    onChange(
      targets.map((target, position) => (position === index ? { ...target, ...patch } : target)),
    );
  const add = () => {
    const channel = channels[0];
    if (!channel) return;
    onChange([
      ...targets,
      { channel_id: channel.id, upstream_model: channel.models?.[0] ?? '', priority: 0, weight: 1 },
    ]);
  };
  if (channels.length === 0) return <p className="text-sm text-muted">{t.routes.noChannels}</p>;
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1.5 text-xs font-medium text-muted">{t.routes.targets}</legend>
      <div className="grid grid-cols-12 gap-2 text-xs text-faint" aria-hidden>
        <span className="col-span-4">{t.routes.channel}</span>
        <span className="col-span-4">{t.routes.upstreamModel}</span>
        <span className="col-span-2">{t.routes.priorityHint}</span>
        <span className="col-span-2">{t.routes.weightHint}</span>
      </div>
      {targets.map((target, index) => {
        const models = channels.find((channel) => channel.id === target.channel_id)?.models ?? [];
        return (
          <div key={index} className="grid grid-cols-12 items-center gap-2">
            <Select
              className="col-span-4"
              aria-label={t.routes.channel}
              value={target.channel_id}
              options={channels.map((channel) => ({ value: channel.id, label: channel.name }))}
              onValueChange={(channel_id) =>
                update(index, {
                  channel_id,
                  upstream_model:
                    channels.find((channel) => channel.id === channel_id)?.models?.[0] ?? '',
                })
              }
            />
            <Select
              className="col-span-4"
              aria-label={t.routes.upstreamModel}
              value={target.upstream_model}
              options={models.map((model) => ({ value: model, label: model }))}
              onValueChange={(upstream_model) => update(index, { upstream_model })}
            />
            <IntegerInput
              className="col-span-2"
              aria-label={t.routes.priority}
              value={target.priority}
              onValueChange={(priority) => update(index, { priority })}
            />
            <div className="col-span-2 flex gap-1">
              <IntegerInput
                aria-label={t.routes.weight}
                min={1}
                value={target.weight}
                onValueChange={(weight) => update(index, { weight })}
              />
              <Button
                size="icon"
                variant="ghost"
                aria-label={t.routes.removeTarget}
                onClick={() => onChange(targets.filter((_, position) => position !== index))}
              >
                <X aria-hidden />
              </Button>
            </div>
          </div>
        );
      })}
      <div>
        <Button size="sm" onClick={add}>
          <Plus aria-hidden />
          {t.routes.addTarget}
        </Button>
      </div>
    </fieldset>
  );
}
