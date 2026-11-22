import { useState } from 'react';
import { useListProviders } from '../../../api/generated/providers/providers';
import type {
  ConsoleCatalogModelInput,
  ConsoleCatalogModelInputKind,
  ConsoleCatalogModelView,
} from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { DecimalInput, Input } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { currencies } from '../../../lib/currency';
import { priceFromMicros, priceToMicros } from '../../../lib/format';

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
  const defaultProvider = catalog[0];
  const [draft, setDraft] = useState<ConsoleCatalogModelInput>({
    provider_id: model?.provider_id ?? defaultProvider?.id ?? 'openai',
    upstream_name: model?.upstream_name ?? '',
    display_name: model?.display_name ?? '',
    kind: (model?.kind as ConsoleCatalogModelInputKind) ?? 'generation',
    formats:
      model?.formats ??
      (defaultProvider?.formats?.[0] ? [defaultProvider.formats[0]] : ['openai_chat']),
    input_price: model?.input_price ?? 0,
    output_price: model?.output_price ?? 0,
    cache_read_price: model?.cache_read_price ?? 0,
    cache_write_price: model?.cache_write_price ?? 0,
    currency: model?.currency ?? 'USD',
    enabled: model?.enabled ?? true,
    version: model?.version,
  });
  const provider = catalog.find((entry) => entry.id === draft.provider_id);
  const formats = provider?.formats ?? [];
  const set = (patch: Partial<ConsoleCatalogModelInput>) => setDraft({ ...draft, ...patch });
  const toggleFormat = (format: string) =>
    set({
      formats: draft.formats.includes(format)
        ? draft.formats.filter((entry) => entry !== format)
        : [...draft.formats, format],
    });
  const selectProvider = (providerID: string) => {
    const selected = catalog.find((entry) => entry.id === providerID);
    set({
      provider_id: providerID,
      formats: selected?.formats?.[0] ? [selected.formats[0]] : [],
    });
  };
  return (
    <FormDialog
      open
      wide
      onOpenChange={(open) => !open && onClose()}
      title={model ? t.models.edit : t.models.add}
      submitLabel={model ? t.common.save : t.common.create}
      pending={pending}
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
                label: entry.label ?? entry.id,
              }))}
              onValueChange={selectProvider}
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
      <fieldset>
        <legend className="mb-1.5 text-xs font-medium text-muted">{t.models.formats}</legend>
        <div className="flex flex-wrap gap-3">
          {formats.map((format) => (
            <label key={format} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-3.5 accent-accent"
                checked={draft.formats.includes(format)}
                onChange={() => toggleFormat(format)}
              />
              <span className="font-mono">{format}</span>
            </label>
          ))}
        </div>
        <p className="mt-1 text-xs text-faint">{t.models.formatHint}</p>
      </fieldset>
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
