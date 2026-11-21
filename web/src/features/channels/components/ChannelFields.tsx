import { useListProviders } from '../../../api/generated/providers/providers';
import type { ConsoleChannelInput } from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { Input, IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { currencies } from '../constants';

const options = (values: string[]) => values.map((value) => ({ value, label: value }));

export function ChannelFields({
  draft,
  onChange,
  basic,
}: {
  draft: ConsoleChannelInput;
  onChange: (draft: ConsoleChannelInput) => void;
  basic?: boolean;
}) {
  const { t } = useLocale();
  const providers = useListProviders();
  const catalog = providers.data ?? [];
  const selected = catalog.find((provider) => provider.id === draft.provider_id);
  const formats = selected?.formats ?? [];
  const endpoints = draft.endpoints ?? [];
  const endpointFor = (format: string) => endpoints.find((entry) => entry.format === format);
  const set = (patch: Partial<ConsoleChannelInput>) => onChange({ ...draft, ...patch });
  const setEndpoint = (format: string, baseURL: string) => {
    const rest = endpoints.filter((entry) => entry.format !== format);
    set({
      endpoints: baseURL ? [...rest, { format, base_url: baseURL }] : rest,
    });
  };
  const selectProvider = (providerID: string) => {
    const provider = catalog.find((entry) => entry.id === providerID);
    const available = new Set(provider?.formats ?? []);
    set({
      provider_id: providerID,
      endpoints: endpoints.filter((entry) => available.has(entry.format)),
    });
  };
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Field label={t.common.name}>
        {(id) => (
          <Input
            id={id}
            required
            value={draft.name}
            onChange={(event) => set({ name: event.target.value })}
          />
        )}
      </Field>
      <Field label={t.channels.provider} hint={t.channels.providerHint}>
        {(id) => (
          <Select
            id={id}
            value={draft.provider_id ?? ''}
            options={catalog.map((provider) => ({
              value: provider.id,
              label: provider.label ?? provider.id,
            }))}
            onValueChange={selectProvider}
          />
        )}
      </Field>
      {!basic &&
        formats.map((format) => (
          <Field
            key={format}
            label={`${t.channels.baseUrl} · ${format}`}
            hint={t.channels.baseUrlHint}
            className="md:col-span-2"
          >
            {(id) => (
              <Input
                id={id}
                type="url"
                className="font-mono"
                placeholder={selected?.hosts?.[format] ?? 'https://api.example.com'}
                value={endpointFor(format)?.base_url ?? ''}
                onChange={(event) => setEndpoint(format, event.target.value)}
              />
            )}
          </Field>
        ))}
      <Field label={t.channels.currency}>
        {(id) => (
          <Select
            id={id}
            value={draft.currency ?? 'USD'}
            options={options(currencies)}
            onValueChange={(currency) => set({ currency })}
          />
        )}
      </Field>
      {!basic && (
        <>
          <Field label={t.channels.timeout} hint={t.channels.timeoutHint}>
            {(id) => (
              <IntegerInput
                id={id}
                value={draft.timeout_seconds ?? 0}
                onValueChange={(timeout_seconds) => set({ timeout_seconds })}
              />
            )}
          </Field>
          <Field label={t.channels.firstByte} hint={t.channels.timeoutHint}>
            {(id) => (
              <IntegerInput
                id={id}
                value={draft.first_byte_timeout_seconds ?? 0}
                onValueChange={(first_byte_timeout_seconds) => set({ first_byte_timeout_seconds })}
              />
            )}
          </Field>
          <Field label={t.channels.defaultMaxOutput} hint={t.channels.defaultMaxOutputHint}>
            {(id) => (
              <IntegerInput
                id={id}
                value={draft.default_max_output_tokens ?? 0}
                onValueChange={(default_max_output_tokens) => set({ default_max_output_tokens })}
              />
            )}
          </Field>
          <Field
            label={t.channels.forwardHeaders}
            hint={t.channels.forwardHeadersHint}
            className="md:col-span-2"
          >
            {(id) => (
              <Input
                id={id}
                value={(draft.forward_headers ?? []).join(', ')}
                onChange={(event) =>
                  set({
                    forward_headers: event.target.value
                      .split(',')
                      .map((name) => name.trim())
                      .filter(Boolean),
                  })
                }
              />
            )}
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={draft.enabled ?? false}
              onCheckedChange={(enabled) => set({ enabled })}
            />
            {t.common.enabled}
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={draft.allow_private_network ?? false}
              onCheckedChange={(allow_private_network) => set({ allow_private_network })}
            />
            {t.channels.privateNetwork}
          </label>
        </>
      )}
    </div>
  );
}
