import type { ConsoleChannelInput, ConsoleProviderView } from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { Badge } from '../../../components/ui/badge';
import { Input, IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import {
  catalogNeedsHost,
  endpointHosts,
  parseEndpointCatalog,
  providerFormats,
} from '../../providers/endpoints';

export function ChannelFields({
  draft,
  providers,
  editing = false,
  basic = false,
  onChange,
}: {
  draft: ConsoleChannelInput;
  providers: ConsoleProviderView[];
  editing?: boolean;
  basic?: boolean;
  onChange: (draft: ConsoleChannelInput) => void;
}) {
  const { t } = useLocale();
  const provider = providers.find((entry) => entry.id === draft.provider_id);
  const catalog = parseEndpointCatalog(provider?.endpoints);
  const formats = catalog ? providerFormats(catalog) : (provider?.formats ?? []);
  const needsHost = catalog ? catalogNeedsHost(catalog) : false;
  const hosts = catalog ? endpointHosts(catalog) : [];
  const set = (patch: Partial<ConsoleChannelInput>) => onChange({ ...draft, ...patch });
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Field label={t.channels.name}>
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
            value={draft.provider_id}
            disabled={editing}
            options={providers.map((entry) => ({
              value: entry.id,
              label: `${entry.label} · ${t.providers.sources[entry.source]}`,
            }))}
            onValueChange={(providerId) => set({ provider_id: providerId })}
          />
        )}
      </Field>
      <Field
        label={t.channels.baseUrl}
        hint={needsHost ? t.channels.baseUrlRequired : t.channels.baseUrlOptional}
        className="md:col-span-2"
      >
        {(id) => (
          <Input
            id={id}
            type="url"
            className="font-mono"
            placeholder={hosts[0] ?? 'https://api.example.com'}
            value={draft.base_url ?? ''}
            onChange={(event) => set({ base_url: event.target.value })}
          />
        )}
      </Field>
      <div className="md:col-span-2">
        <p className="mb-1.5 text-xs font-medium text-muted">{t.channels.formats}</p>
        <div className="flex flex-wrap gap-1.5">
          {formats.map((format) => (
            <Badge key={format}>{format}</Badge>
          ))}
        </div>
        <p className="mt-1 text-xs text-faint">{t.channels.formatsProviderHint}</p>
      </div>
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
          <Field label={t.channels.forwardHeaders} hint={t.channels.forwardHeadersHint}>
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
