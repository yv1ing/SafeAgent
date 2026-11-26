import { useMemo, useState } from 'react';
import { useListChannelOptions } from '../../../api/generated/channel-options/channel-options';
import { useListProviders } from '../../../api/generated/providers/providers';
import { TableSkeleton } from '../../../components/data/DataTable';
import { QueryState } from '../../../components/feedback/QueryState';
import { FormDialog } from '../../../components/form/FormDialog';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { Select } from '../../../components/ui/select';
import { interpolate, useLocale } from '../../../i18n';
import { groupModelsByProvider } from '../../models/models';
import { useModels } from '../../models/hooks';
import { useRoutesFromModels } from '../hooks';

export function FromModelsDialog({
  projectId,
  onClose,
}: {
  projectId: string;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const models = useModels({});
  const channels = useListChannelOptions(projectId, { query: { refetchOnMount: 'always' } });
  const providers = useListProviders();
  const createRoutes = useRoutesFromModels(projectId);
  const [providerId, setProviderId] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const providerLabels = useMemo(
    () => new Map((providers.data ?? []).map((provider) => [provider.id, provider.label])),
    [providers.data],
  );
  const channelOf = (modelId: string, provider?: string) =>
    (channels.data ?? []).find(
      (channel) =>
        (provider === undefined || channel.provider_id === provider) &&
        (channel.models ?? []).some((model) => model.id === modelId),
    );
  const rows = (models.data ?? []).filter(
    (model) => providerId === '' || model.provider_id === providerId,
  );
  const groups = groupModelsByProvider(rows, providerLabels);
  const items = rows.flatMap((model) => {
    const channel = channelOf(model.id, model.provider_id);
    return channel ? [{ model, channelId: channel.id, channelName: channel.name }] : [];
  });
  const selectedItems = items.filter((item) => selected.includes(item.model.id));
  const toggle = (modelId: string) =>
    setSelected(
      selected.includes(modelId)
        ? selected.filter((entry) => entry !== modelId)
        : [...selected, modelId],
    );
  return (
    <FormDialog
      open
      wide
      onOpenChange={(open) => !open && onClose()}
      title={t.routes.fromModelsTitle}
      description={t.routes.fromModelsHint}
      submitLabel={interpolate(t.routes.fromModelsCreate, { count: selectedItems.length })}
      pending={createRoutes.isPending}
      disabled={selectedItems.length === 0}
      onSubmit={() =>
        createRoutes.mutate(
          selectedItems.map((item) => ({ model: item.model, channelId: item.channelId })),
          { onSuccess: onClose },
        )
      }
    >
      <Select
        aria-label={t.models.provider}
        value={providerId}
        options={[
          { value: '', label: t.models.allProviders },
          ...(providers.data ?? []).map((provider) => ({
            value: provider.id,
            label: provider.label,
          })),
        ]}
        onValueChange={setProviderId}
      />
      <QueryState query={models} loading={<TableSkeleton rows={4} />}>
        {() =>
          groups.length === 0 || items.length === 0 ? (
            <p className="text-sm text-muted">{t.routes.fromModelsEmpty}</p>
          ) : (
            <div className="scroll-thin flex max-h-80 flex-col gap-3 overflow-y-auto">
              {groups.map((group) => {
                const entries = items.filter((item) => item.model.provider_id === group.providerId);
                if (entries.length === 0) return null;
                return (
                  <section key={group.providerId} className="flex flex-col gap-1">
                    <h3 className="text-xs font-medium text-muted">{group.label}</h3>
                    {entries.map((entry) => (
                      <label
                        key={entry.model.id}
                        className="flex cursor-pointer items-center gap-2 rounded-md border px-2 py-1.5 text-sm hover:bg-sunken"
                      >
                        <input
                          type="checkbox"
                          className="size-3.5 accent-accent"
                          checked={selected.includes(entry.model.id)}
                          onChange={() => toggle(entry.model.id)}
                        />
                        <span className="min-w-0 flex-1 truncate font-mono text-xs">
                          {entry.model.upstream_name}
                        </span>
                        <Badge>{t.models.kinds[entry.model.kind]}</Badge>
                        <span className="text-xs text-muted">{entry.channelName}</span>
                      </label>
                    ))}
                  </section>
                );
              })}
            </div>
          )
        }
      </QueryState>
      <div className="flex items-center justify-between gap-2">
        <Button
          size="sm"
          onClick={() => setSelected(items.map((item) => item.model.id))}
          disabled={selected.length === items.length}
        >
          {t.providers.selectAll}
        </Button>
        <span className="text-xs text-faint">
          {interpolate(t.routes.fromModelsCount, { count: selectedItems.length })}
        </span>
      </div>
    </FormDialog>
  );
}
