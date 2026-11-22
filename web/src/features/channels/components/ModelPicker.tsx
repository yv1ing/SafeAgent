import type { ConsoleCatalogModelView, ConsoleChannelInput } from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { useLocale } from '../../../i18n';

export function ModelPicker({
  draft,
  models,
  onChange,
}: {
  draft: ConsoleChannelInput;
  models: ConsoleCatalogModelView[];
  onChange: (modelIds: string[]) => void;
}) {
  const { t } = useLocale();
  const formats = new Set((draft.endpoints ?? []).map((endpoint) => endpoint.format));
  const matching = models.filter((model) => model.provider_id === draft.provider_id);
  const toggle = (modelId: string) =>
    onChange(
      draft.model_ids.includes(modelId)
        ? draft.model_ids.filter((id) => id !== modelId)
        : [...draft.model_ids, modelId],
    );
  if (matching.length === 0)
    return <p className="px-4 py-6 text-sm text-muted">{t.channels.noModels}</p>;
  return (
    <ul className="divide-y">
      {matching.map((model) => {
        const shared = model.formats.filter((format) => formats.has(format));
        const selected = draft.model_ids.includes(model.id);
        return (
          <li key={model.id} className="flex items-start gap-3 px-4 py-2.5">
            <input
              type="checkbox"
              className="mt-1 size-3.5 accent-accent"
              checked={selected}
              disabled={shared.length === 0 && !selected}
              aria-label={model.upstream_name}
              onChange={() => toggle(model.id)}
            />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-sm">{model.upstream_name}</span>
                <Badge>{t.models.kinds[model.kind]}</Badge>
                {!model.enabled && <Badge tone="warning">{t.common.disabled}</Badge>}
              </div>
              {model.display_name && <p className="text-xs text-muted">{model.display_name}</p>}
              <p className="font-mono text-xs text-faint">
                {shared.length > 0 ? shared.join(', ') : t.channels.unavailableModel}
              </p>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
