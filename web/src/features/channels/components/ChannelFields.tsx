import { ArrowDown, ArrowUp, Plus, X } from 'lucide-react';
import { useListProviders } from '../../../api/generated/providers/providers';
import type { ConsoleChannelInput } from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { Button } from '../../../components/ui/button';
import { Input, IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';

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
  const set = (patch: Partial<ConsoleChannelInput>) => onChange({ ...draft, ...patch });
  const selectProvider = (providerID: string) => {
    const provider = catalog.find((entry) => entry.id === providerID);
    const format = provider?.formats?.[0];
    set({
      provider_id: providerID,
      endpoints: format ? [{ format, base_url: '', path: '' }] : [],
      model_ids: [],
    });
  };
  const updateEndpoint = (
    index: number,
    patch: Partial<ConsoleChannelInput['endpoints'][number]>,
  ) =>
    set({
      endpoints: endpoints.map((endpoint, position) =>
        position === index ? { ...endpoint, ...patch } : endpoint,
      ),
    });
  const moveEndpoint = (index: number, direction: -1 | 1) => {
    const next = [...endpoints];
    const target = index + direction;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    set({ endpoints: next });
  };
  const removeEndpoint = (index: number) =>
    set({ endpoints: endpoints.filter((_, position) => position !== index) });
  const addEndpoint = () => {
    const format = formats.find(
      (candidate) => !endpoints.some((endpoint) => endpoint.format === candidate),
    );
    if (format) set({ endpoints: [...endpoints, { format, base_url: '', path: '' }] });
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
            disabled={!basic}
            options={catalog.map((provider) => ({
              value: provider.id,
              label: provider.label ?? provider.id,
            }))}
            onValueChange={selectProvider}
          />
        )}
      </Field>
      <fieldset className="md:col-span-2">
        <legend className="mb-1.5 text-xs font-medium text-muted">{t.channels.formats}</legend>
        <div className="grid grid-cols-12 gap-2 text-xs text-faint" aria-hidden>
          <span className="col-span-3">{t.channels.format}</span>
          <span className="col-span-4">{t.channels.baseUrl}</span>
          <span className="col-span-3">{t.channels.path}</span>
          <span className="col-span-2" />
        </div>
        <div className="mt-1 flex flex-col gap-2">
          {endpoints.map((endpoint, index) => (
            <div key={`${endpoint.format}-${index}`} className="grid grid-cols-12 gap-2">
              <Select
                className="col-span-3 font-mono"
                aria-label={t.channels.format}
                value={endpoint.format}
                options={options(
                  formats.filter(
                    (candidate) =>
                      candidate === endpoint.format ||
                      !endpoints.some(
                        (other, position) => position !== index && other.format === candidate,
                      ),
                  ),
                )}
                onValueChange={(format) => updateEndpoint(index, { format })}
              />
              <Input
                className="col-span-4 font-mono"
                type="url"
                aria-label={t.channels.baseUrl}
                placeholder={selected?.hosts?.[endpoint.format] ?? 'https://api.example.com'}
                required={selected?.custom}
                value={endpoint.base_url ?? ''}
                onChange={(event) => updateEndpoint(index, { base_url: event.target.value })}
              />
              <Input
                className="col-span-3 font-mono"
                aria-label={t.channels.path}
                placeholder="/v1/..."
                value={endpoint.path ?? ''}
                onChange={(event) => updateEndpoint(index, { path: event.target.value })}
              />
              <div className="col-span-2 flex justify-end gap-0.5">
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={t.channels.moveUp}
                  disabled={index === 0}
                  onClick={() => moveEndpoint(index, -1)}
                >
                  <ArrowUp aria-hidden />
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={t.channels.moveDown}
                  disabled={index === endpoints.length - 1}
                  onClick={() => moveEndpoint(index, 1)}
                >
                  <ArrowDown aria-hidden />
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={t.channels.removeEndpoint}
                  disabled={endpoints.length === 1}
                  onClick={() => removeEndpoint(index)}
                >
                  <X aria-hidden />
                </Button>
              </div>
            </div>
          ))}
        </div>
        <div className="mt-2 flex items-center justify-between gap-3">
          <p className="text-xs text-faint">{t.channels.formatsHint}</p>
          <Button size="sm" onClick={addEndpoint} disabled={endpoints.length >= formats.length}>
            <Plus aria-hidden />
            {t.channels.addEndpoint}
          </Button>
        </div>
      </fieldset>
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
