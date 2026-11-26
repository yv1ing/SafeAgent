import { useState } from 'react';
import { discoverChannelModels, useListChannels } from '../../../api/generated/channels/channels';
import { useListProviders } from '../../../api/generated/providers/providers';
import type {
  ConsoleCatalogModelInputKind,
  ConsoleDiscoveryView,
} from '../../../api/generated/models';
import { Callout } from '../../../components/feedback/Callout';
import { FormDialog } from '../../../components/form/FormDialog';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { Textarea } from '../../../components/ui/textarea';
import { interpolate, useLocale } from '../../../i18n';
import { currencies } from '../../../lib/currency';
import { useConsoleMutation } from '../../../lib/mutation';
import { discoveryFormats, parseEndpointCatalog } from '../../providers/endpoints';
import { useCreateModelBatch } from '../hooks';
import { modelBatchInputs } from '../models';

const kinds: ConsoleCatalogModelInputKind[] = ['generation', 'embedding', 'moderation'];

function splitNames(text: string) {
  return Array.from(
    new Set(
      text
        .split(/[\n,]+/)
        .map((name) => name.trim())
        .filter((name) => name !== ''),
    ),
  );
}

export function ImportModelsDialog({ onClose }: { onClose: () => void }) {
  const { t } = useLocale();
  const providers = useListProviders();
  const channels = useListChannels();
  const providerRows = providers.data ?? [];
  const [providerId, setProviderId] = useState(() => providerRows[0]?.id ?? '');
  const [channelId, setChannelId] = useState('');
  const [format, setFormat] = useState('');
  const [discovered, setDiscovered] = useState<string[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [manual, setManual] = useState('');
  const [kind, setKind] = useState<ConsoleCatalogModelInputKind>('generation');
  const [currency, setCurrency] = useState('USD');
  const [enabled, setEnabled] = useState(true);
  const [discoveryError, setDiscoveryError] = useState(false);
  const createModels = useCreateModelBatch();
  const discover = useConsoleMutation<{ channelId: string; format: string }, ConsoleDiscoveryView>({
    mutationFn: ({ channelId: target, format: entry }) =>
      discoverChannelModels(target, { format: entry }),
    invalidates: [],
  });
  const provider = providerRows.find((entry) => entry.id === providerId);
  const endpoints = parseEndpointCatalog(provider?.endpoints);
  const formats = endpoints ? discoveryFormats(endpoints) : [];
  const channelRows = (channels.data ?? []).filter(
    (channel) => channel.provider_id === providerId && !channel.deleted_at,
  );
  const effectiveChannel = channelRows.some((channel) => channel.id === channelId)
    ? channelId
    : (channelRows[0]?.id ?? '');
  const effectiveFormat = formats.includes(format) ? format : (formats[0] ?? '');
  const candidates = Array.from(new Set([...discovered, ...splitNames(manual)]));
  const selectedNames = candidates.filter((name) => selected.includes(name));
  const toggle = (name: string) =>
    setSelected(
      selected.includes(name) ? selected.filter((entry) => entry !== name) : [...selected, name],
    );
  const runDiscovery = () => {
    if (effectiveChannel === '' || effectiveFormat === '') return;
    setDiscoveryError(false);
    discover.mutate(
      { channelId: effectiveChannel, format: effectiveFormat },
      {
        onSuccess: (view) => {
          const names = view.models ?? [];
          setDiscovered(names);
          setSelected(names);
        },
        onError: () => setDiscoveryError(true),
      },
    );
  };
  return (
    <FormDialog
      open
      wide
      onOpenChange={(open) => !open && onClose()}
      title={t.models.importTitle}
      description={t.models.importHint}
      submitLabel={t.models.importSelected}
      pending={createModels.isPending}
      disabled={selectedNames.length === 0}
      onSubmit={() =>
        createModels.mutate(
          {
            models: modelBatchInputs(providerId, selectedNames, { kind, currency, enabled }),
          },
          { onSuccess: onClose },
        )
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <Select
          aria-label={t.models.provider}
          value={providerId}
          options={providerRows.map((entry) => ({ value: entry.id, label: entry.label }))}
          onValueChange={(value) => {
            setProviderId(value);
            setDiscovered([]);
            setSelected([]);
          }}
        />
        <Select
          aria-label={t.models.importChannel}
          value={effectiveChannel}
          options={channelRows.map((channel) => ({ value: channel.id, label: channel.name }))}
          onValueChange={setChannelId}
        />
        <Select
          aria-label={t.models.formats}
          value={effectiveFormat}
          options={formats.map((entry) => ({ value: entry, label: entry }))}
          onValueChange={setFormat}
        />
        <Button
          loading={discover.isPending}
          disabled={effectiveChannel === '' || formats.length === 0}
          onClick={runDiscovery}
        >
          {t.models.importDiscover}
        </Button>
      </div>
      {channelRows.length === 0 && <Callout tone="warning">{t.models.importNoChannels}</Callout>}
      {discoveryError && <Callout tone="danger">{t.models.importFailed}</Callout>}
      {candidates.length > 0 ? (
        <ul className="scroll-thin flex max-h-56 flex-col gap-1 overflow-y-auto rounded-md border p-2">
          {candidates.map((name) => (
            <li key={name}>
              <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-sunken">
                <input
                  type="checkbox"
                  className="size-3.5 accent-accent"
                  checked={selected.includes(name)}
                  onChange={() => toggle(name)}
                />
                <span className="font-mono">{name}</span>
              </label>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted">{t.models.importEmpty}</p>
      )}
      <Textarea
        aria-label={t.models.importManual}
        spellCheck={false}
        className="min-h-16 font-mono text-xs"
        placeholder={t.models.importManualHint}
        value={manual}
        onChange={(event) => setManual(event.target.value)}
      />
      <div className="grid gap-3 sm:grid-cols-2">
        <Select
          aria-label={t.models.kind}
          value={kind}
          options={kinds.map((entry) => ({ value: entry, label: t.models.kinds[entry] }))}
          onValueChange={(value) => setKind(value as ConsoleCatalogModelInputKind)}
        />
        <Select
          aria-label={t.models.currency}
          value={currency}
          options={currencies.map((entry) => ({ value: entry, label: entry }))}
          onValueChange={setCurrency}
        />
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-xs text-faint">
          {interpolate(t.models.importCount, { count: selectedNames.length })}
        </span>
        <div className="flex flex-wrap items-center gap-2">
          <Badge>{t.models.kinds[kind]}</Badge>
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            {t.common.enabled}
          </label>
        </div>
      </div>
    </FormDialog>
  );
}
