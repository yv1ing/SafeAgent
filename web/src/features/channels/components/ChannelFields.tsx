import type { ConsoleChannelInput } from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { Input, IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { currencies, formats, vendors } from '../constants';

const options = (values: string[]) => values.map((value) => ({ value, label: value }));

const endpointPlaceholders: Record<string, string> = {
  openai: 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com',
  google: 'https://generativelanguage.googleapis.com',
};

const getEndpointPlaceholder = (vendor: string | undefined) => {
  if (!vendor) return 'https://api.example.com';
  return endpointPlaceholders[vendor] || 'https://api.example.com';
};

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
  const set = (patch: Partial<ConsoleChannelInput>) => onChange({ ...draft, ...patch });
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
      <Field label={t.channels.format}>
        {(id) => (
          <Select
            id={id}
            value={draft.format}
            options={options(formats)}
            onValueChange={(format) => set({ format })}
          />
        )}
      </Field>
      <Field label={t.channels.endpoint} className="md:col-span-2">
        {(id) => (
          <Input
            id={id}
            required
            type="url"
            className="font-mono"
            placeholder={getEndpointPlaceholder(draft.vendor)}
            value={draft.endpoint}
            onChange={(event) => set({ endpoint: event.target.value })}
          />
        )}
      </Field>
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
          <Field label={t.channels.vendor}>
            {(id) => (
              <Select
                id={id}
                value={draft.vendor ?? ''}
                options={options(vendors)}
                onValueChange={(vendor) => set({ vendor })}
              />
            )}
          </Field>
          <Field
            label={t.channels.countEndpoint}
            hint={t.channels.countEndpointHint}
            className="md:col-span-2"
          >
            {(id) => (
              <Input
                id={id}
                type="url"
                className="font-mono"
                placeholder={getEndpointPlaceholder(draft.vendor)}
                value={draft.count_tokens_endpoint ?? ''}
                onChange={(event) => set({ count_tokens_endpoint: event.target.value })}
              />
            )}
          </Field>
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
