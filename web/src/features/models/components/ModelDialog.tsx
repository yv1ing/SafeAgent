import { useState } from 'react';
import { useListProviders } from '../../../api/generated/providers/providers';
import type {
  ConsoleCatalogModelInput,
  ConsoleCatalogModelInputKind,
  ConsoleCatalogModelView,
} from '../../../api/generated/models';
import { Callout } from '../../../components/feedback/Callout';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Badge } from '../../../components/ui/badge';
import { DecimalInput, Input } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { currencies } from '../../../lib/currency';
import { priceFromMicros, priceToMicros } from '../../../lib/format';
import { formatsForKind, parseEndpointCatalog } from '../../providers/endpoints';

const kinds: ConsoleCatalogModelInputKind[] = ['generation', 'embedding', 'moderation'];

export function ModelDialog({
  model,
  pending,
  onSubmit,
  onClose,
}: {
  model: ConsoleCatalogModelView | null;
  pending: boolean;
  onSubmit: (input: ConsoleCatalogModelInput) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const providers = useListProviders();
  const catalog = providers.data ?? [];
  const [draft, setDraft] = useState<ConsoleCatalogModelInput>(() => ({
    provider_id: model?.provider_id ?? catalog[0]?.id ?? '',
    upstream_name: model?.upstream_name ?? '',
    display_name: model?.display_name ?? '',
    kind: model?.kind ?? 'generation',
    input_price: model?.input_price ?? 0,
    output_price: model?.output_price ?? 0,
    cache_read_price: model?.cache_read_price ?? 0,
    cache_write_price: model?.cache_write_price ?? 0,
    currency: model?.currency ?? 'USD',
    enabled: model?.enabled ?? true,
    version: model?.version,
  }));
  const provider = catalog.find((entry) => entry.id === draft.provider_id);
  const endpoints = parseEndpointCatalog(provider?.endpoints);
  const formats = endpoints ? formatsForKind(endpoints, draft.kind) : [];
  const set = (patch: Partial<ConsoleCatalogModelInput>) => setDraft({ ...draft, ...patch });
  return (
    <FormDialog
      open
      wide
      onOpenChange={(open) => !open && onClose()}
      title={model ? t.models.edit : t.models.add}
      submitLabel={model ? t.common.save : t.common.create}
      pending={pending}
      disabled={
        draft.upstream_name.trim() === '' || draft.provider_id === '' || formats.length === 0
      }
      onSubmit={() => onSubmit(draft)}
    >
      <div className="grid gap-4 md:grid-cols-2">
        <Field label={t.models.provider}>
          {(id) => (
            <Select
              id={id}
              value={draft.provider_id}
              disabled={model !== null}
              options={catalog.map((entry) => ({
                value: entry.id,
                label: entry.label,
              }))}
              onValueChange={(providerId) => set({ provider_id: providerId })}
            />
          )}
        </Field>
        <Field label={t.models.kind} hint={model ? t.models.immutable : undefined}>
          {(id) => (
            <Select
              id={id}
              value={draft.kind}
              disabled={model !== null}
              options={kinds.map((kind) => ({ value: kind, label: t.models.kinds[kind] }))}
              onValueChange={(kind) => set({ kind: kind as ConsoleCatalogModelInputKind })}
            />
          )}
        </Field>
        <Field label={t.models.upstreamName} hint={t.models.upstreamNameHint}>
          {(id) => (
            <Input
              id={id}
              required
              autoFocus
              className="font-mono"
              value={draft.upstream_name}
              onChange={(event) => set({ upstream_name: event.target.value })}
            />
          )}
        </Field>
        <Field label={t.models.displayName} hint={t.models.displayNameHint}>
          {(id) => (
            <Input
              id={id}
              value={draft.display_name ?? ''}
              onChange={(event) => set({ display_name: event.target.value })}
            />
          )}
        </Field>
      </div>
      <div className="flex flex-col gap-1.5">
        <p className="text-xs font-medium text-muted">{t.models.formats}</p>
        <div className="flex flex-wrap gap-1.5">
          {formats.map((format) => (
            <Badge key={format}>{format}</Badge>
          ))}
        </div>
        <p className="text-xs text-faint">{t.models.formatHint}</p>
        {formats.length === 0 && <Callout tone="warning">{t.models.noFormats}</Callout>}
      </div>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {(
          [
            ['input_price', t.models.inputPrice],
            ['output_price', t.models.outputPrice],
            ['cache_read_price', t.models.cacheReadPrice],
            ['cache_write_price', t.models.cacheWritePrice],
          ] as const
        ).map(([field, label]) => (
          <Field key={field} label={label} hint={t.models.priceHint}>
            {(id) => (
              <DecimalInput
                id={id}
                value={priceFromMicros(draft[field])}
                onValueChange={(value) => set({ [field]: priceToMicros(value) })}
              />
            )}
          </Field>
        ))}
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <Field label={t.models.currency}>
          {(id) => (
            <Select
              id={id}
              value={draft.currency ?? 'USD'}
              options={currencies.map((currency) => ({ value: currency, label: currency }))}
              onValueChange={(currency) => set({ currency })}
            />
          )}
        </Field>
        <label className="flex items-center gap-2 self-end pb-2 text-sm">
          <Switch
            checked={draft.enabled ?? false}
            onCheckedChange={(enabled) => set({ enabled })}
          />
          {t.common.enabled}
        </label>
      </div>
    </FormDialog>
  );
}
