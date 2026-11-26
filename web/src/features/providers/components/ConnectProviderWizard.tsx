import { useState } from 'react';
import { discoverChannelModels } from '../../../api/generated/channels/channels';
import type {
  ConsoleCatalogModelInputKind,
  ConsoleCatalogModelView,
  ConsoleConnectInput,
  ConsoleConnectView,
  ConsoleDiscoveryView,
  ConsoleProviderView,
} from '../../../api/generated/models';
import { useListProjects } from '../../../api/generated/projects/projects';
import { Callout } from '../../../components/feedback/Callout';
import { Segmented } from '../../../components/feedback/Segmented';
import { Wizard, type WizardStep } from '../../../components/form/Wizard';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { Input, IntegerInput } from '../../../components/ui/input';
import { Select } from '../../../components/ui/select';
import { Switch } from '../../../components/ui/switch';
import { Textarea } from '../../../components/ui/textarea';
import { errorMessage, interpolate, useLocale } from '../../../i18n';
import { currencies } from '../../../lib/currency';
import { useConsoleMutation } from '../../../lib/mutation';
import { modelBatchInputs } from '../../models/models';
import { useCreateModelBatch } from '../../models/hooks';
import { useRouteMutations } from '../../routes/hooks';
import {
  catalogNeedsHost,
  discoveryFormats,
  endpointsToText,
  formatsForKind,
  parseEndpointCatalog,
  providerFormats,
  validateEndpointCatalog,
  type EndpointCatalog,
} from '../endpoints';
import { useConnectProvider } from '../hooks';
import { EndpointsEditor } from './EndpointsEditor';

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

export function ConnectProviderWizard({
  open,
  onOpenChange,
  providers,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  providers: ConsoleProviderView[];
}) {
  const { t } = useLocale();
  const builtins = providers.filter((provider) => provider.source === 'builtin');
  const customSeed = builtins.find((provider) => provider.id === 'custom');
  const projects = useListProjects();
  const connect = useConnectProvider();
  const createModels = useCreateModelBatch();

  const [mode, setMode] = useState<'builtin' | 'custom'>('builtin');
  const [builtinId, setBuiltinId] = useState(() => builtins[0]?.id ?? '');
  const [customLabel, setCustomLabel] = useState('');
  const [customText, setCustomText] = useState(() => endpointsToText(customSeed?.endpoints ?? {}));
  const [channelName, setChannelName] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [keysText, setKeysText] = useState('');
  const [allowPrivateNetwork, setAllowPrivateNetwork] = useState(false);
  const [format, setFormat] = useState('');
  const [connection, setConnection] = useState<ConsoleConnectView | null>(null);
  const [discovered, setDiscovered] = useState<string[]>([]);
  const [discoveryError, setDiscoveryError] = useState<unknown>(null);
  const [manualNames, setManualNames] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [kind, setKind] = useState<ConsoleCatalogModelInputKind>('generation');
  const [currency, setCurrency] = useState('USD');
  const [enabled, setEnabled] = useState(true);
  const [routeEnabled, setRouteEnabled] = useState(false);
  const [projectId, setProjectId] = useState('');
  const [routeName, setRouteName] = useState('');
  const [routeMaxOutput, setRouteMaxOutput] = useState(0);
  const [createdModels, setCreatedModels] = useState<ConsoleCatalogModelView[] | null>(null);

  const discover = useConsoleMutation<{ channelId: string; format: string }, ConsoleDiscoveryView>({
    mutationFn: ({ channelId, format: entry }) =>
      discoverChannelModels(channelId, { format: entry }),
    invalidates: [],
  });
  const routeMutations = useRouteMutations(projectId);

  const selectedProvider = builtins.find((provider) => provider.id === builtinId);
  const customParsed = validateEndpointCatalog(customText);
  const catalog: EndpointCatalog | null =
    mode === 'builtin'
      ? parseEndpointCatalog(selectedProvider?.endpoints)
      : (customParsed.catalog ?? null);
  const formats = catalog ? providerFormats(catalog) : [];
  const discovery = catalog ? discoveryFormats(catalog) : [];
  const needsHost = catalog ? catalogNeedsHost(catalog) : true;
  const keys = splitNames(keysText);
  const connected = connection !== null;
  const candidates = Array.from(new Set([...discovered, ...splitNames(manualNames)]));
  const selectedNames = candidates.filter((name) => selected.includes(name));
  const modelNames = createdModels?.map((model) => model.upstream_name) ?? selectedNames;
  const effectiveFormat = discovery.includes(format) ? format : (discovery[0] ?? '');
  const canConnect =
    channelName.trim() !== '' &&
    keys.length > 0 &&
    keys.length <= 8 &&
    (!needsHost || baseUrl.trim() !== '') &&
    (mode === 'builtin' ? builtinId !== '' : customLabel.trim() !== '' && catalog !== null);
  const busy = connect.isPending || discover.isPending;
  const pending = createModels.isPending || routeMutations.create.isPending;

  const toggleName = (name: string) =>
    setSelected(
      selected.includes(name) ? selected.filter((entry) => entry !== name) : [...selected, name],
    );

  const runConnect = () => {
    if (!canConnect) return;
    const input: ConsoleConnectInput = {
      channel: {
        name: channelName.trim(),
        provider_id: mode === 'builtin' ? builtinId : '',
        base_url: baseUrl.trim(),
        enabled: true,
        allow_private_network: allowPrivateNetwork,
      },
      keys,
      models: [],
    };
    if (mode === 'custom' && catalog) {
      input.provider = { label: customLabel.trim(), endpoints: catalog, enabled: true };
    }
    connect.mutate(input, {
      onSuccess: (result) => {
        setConnection(result);
        if (effectiveFormat === '') {
          setDiscovered([]);
          return;
        }
        discover.mutate(
          { channelId: result.channel.id, format: effectiveFormat },
          {
            onSuccess: (view) => setDiscovered(view.models ?? []),
            onError: (error) => setDiscoveryError(error),
          },
        );
      },
    });
  };

  const runDiscovery = () => {
    if (!connected || effectiveFormat === '') return;
    setDiscoveryError(null);
    discover.mutate(
      { channelId: connection.channel.id, format: effectiveFormat },
      {
        onSuccess: (view) => setDiscovered(view.models ?? []),
        onError: (error) => setDiscoveryError(error),
      },
    );
  };

  const finish = async () => {
    if (!connection) return;
    try {
      let models = createdModels;
      if (!models && selectedNames.length > 0) {
        const providerId = connection.provider?.id ?? connection.channel.provider_id;
        models = await createModels.mutateAsync({
          models: modelBatchInputs(providerId, selectedNames, { kind, currency, enabled }),
        });
        setCreatedModels(models);
      }
      if (routeEnabled && projectId !== '' && models !== null && models.length > 0) {
        await routeMutations.create.mutateAsync({
          name: routeName.trim() || models[0]!.upstream_name,
          targets: models.map((model) => ({
            channel_id: connection.channel.id,
            model_id: model.id,
            max_output_tokens: routeMaxOutput || undefined,
            enabled: true,
          })),
          enabled: true,
        });
      }
      onOpenChange(false);
    } catch {
      // useConsoleMutation has already surfaced the failure; keep the wizard open so the remaining
      // steps can be retried without recreating the connection.
    }
  };

  const providerStep = (
    <div className="flex flex-col gap-4">
      <Segmented
        label={t.providers.providerMode}
        value={mode}
        options={[
          { value: 'builtin', label: t.providers.builtinProvider },
          { value: 'custom', label: t.providers.customProvider },
        ]}
        onChange={(next) => {
          if (!connected) setMode(next);
        }}
      />
      {mode === 'builtin' ? (
        <>
          <Select
            aria-label={t.providers.provider}
            value={builtinId}
            disabled={connected}
            options={builtins.map((provider) => ({
              value: provider.id,
              label: provider.label,
            }))}
            onValueChange={setBuiltinId}
          />
          <div className="flex flex-wrap items-center gap-1.5">
            {formats.map((entry) => (
              <Badge key={entry}>{entry}</Badge>
            ))}
          </div>
          <Callout tone="info">{t.providers.builtinHint}</Callout>
        </>
      ) : (
        <>
          <Input
            aria-label={t.common.label}
            placeholder={t.providers.customLabelPlaceholder}
            value={customLabel}
            disabled={connected}
            onChange={(event) => setCustomLabel(event.target.value)}
          />
          <EndpointsEditor text={customText} disabled={connected} onChange={setCustomText} />
        </>
      )}
    </div>
  );

  const channelStep = (
    <div className="flex flex-col gap-4">
      {connected ? (
        <Callout tone="success">
          {t.providers.alreadyConnected} {connection.channel.name}
        </Callout>
      ) : null}
      <Input
        aria-label={t.channels.name}
        placeholder={t.channels.add}
        value={channelName}
        disabled={connected}
        onChange={(event) => setChannelName(event.target.value)}
      />
      <div className="flex flex-col gap-1.5">
        <Input
          aria-label={t.channels.baseUrl}
          placeholder="https://api.example.com"
          className="font-mono"
          value={baseUrl}
          disabled={connected}
          onChange={(event) => setBaseUrl(event.target.value)}
        />
        <p className="text-xs text-faint">
          {needsHost ? t.channels.baseUrlRequired : t.channels.baseUrlOptional}
        </p>
      </div>
      <div className="flex flex-col gap-1.5">
        <Textarea
          aria-label={t.channels.secret}
          spellCheck={false}
          className="min-h-20 font-mono text-xs"
          placeholder={t.providers.keysPlaceholder}
          value={keysText}
          disabled={connected}
          onChange={(event) => setKeysText(event.target.value)}
        />
        <p className="text-xs text-faint">{t.providers.keysHint}</p>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <Switch
          checked={allowPrivateNetwork}
          disabled={connected}
          onCheckedChange={setAllowPrivateNetwork}
        />
        {t.channels.privateNetwork}
      </label>
    </div>
  );

  const discoveryStep = (
    <div className="flex flex-col gap-4">
      <dl className="grid gap-2 rounded-md border bg-sunken/40 p-3 text-sm sm:grid-cols-2">
        <div className="flex flex-col">
          <dt className="text-xs text-muted">{t.providers.provider}</dt>
          <dd>{mode === 'custom' ? customLabel || '—' : (selectedProvider?.label ?? '—')}</dd>
        </div>
        <div className="flex flex-col">
          <dt className="text-xs text-muted">{t.channels.name}</dt>
          <dd>{channelName || '—'}</dd>
        </div>
        <div className="flex flex-col">
          <dt className="text-xs text-muted">{t.channels.keys}</dt>
          <dd className="num">{keys.length}</dd>
        </div>
        <div className="flex flex-col">
          <dt className="text-xs text-muted">{t.channels.baseUrl}</dt>
          <dd className="truncate font-mono text-xs">{baseUrl || '—'}</dd>
        </div>
      </dl>
      {discovery.length > 0 ? (
        <Select
          aria-label={t.channels.formats}
          value={effectiveFormat}
          disabled={connected}
          options={discovery.map((entry) => ({ value: entry, label: entry }))}
          onValueChange={setFormat}
        />
      ) : (
        <Callout tone="warning">{t.providers.noDiscovery}</Callout>
      )}
      {!connected ? (
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="primary" loading={busy} disabled={!canConnect} onClick={runConnect}>
            {t.providers.connectDiscover}
          </Button>
          <span className="text-xs text-faint">{t.providers.connectNote}</span>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone="success">{t.providers.connected}</Badge>
            <span className="font-mono text-xs">{connection.channel.name}</span>
            {discovery.length > 0 && (
              <Button size="sm" loading={discover.isPending} onClick={runDiscovery}>
                {t.providers.discoverAgain}
              </Button>
            )}
          </div>
          {discoveryError ? (
            <Callout tone="danger">{errorMessage(t, discoveryError)}</Callout>
          ) : null}
          {discovered.length > 0 ? (
            <div className="scroll-thin flex max-h-40 flex-wrap gap-1.5 overflow-y-auto">
              {discovered.map((name) => (
                <Badge key={name}>{name}</Badge>
              ))}
            </div>
          ) : discovery.length > 0 && !discover.isPending ? (
            <p className="text-xs text-faint">{t.providers.noDiscoveryModels}</p>
          ) : null}
          <p className="text-xs text-faint">{t.providers.closeKeepsConnection}</p>
        </div>
      )}
    </div>
  );

  const modelsStep = (
    <div className="flex flex-col gap-4">
      {candidates.length > 0 ? (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              onClick={() => setSelected(candidates)}
              disabled={selected.length === candidates.length}
            >
              {t.providers.selectAll}
            </Button>
            <Button size="sm" onClick={() => setSelected([])} disabled={selected.length === 0}>
              {t.providers.selectNone}
            </Button>
            <span className="text-xs text-faint">
              {selectedNames.length} / {candidates.length}
            </span>
          </div>
          <ul className="scroll-thin flex max-h-56 flex-col gap-1 overflow-y-auto rounded-md border p-2">
            {candidates.map((name) => (
              <li key={name}>
                <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-sunken">
                  <input
                    type="checkbox"
                    className="size-3.5 accent-accent"
                    checked={selected.includes(name)}
                    onChange={() => toggleName(name)}
                  />
                  <span className="font-mono">{name}</span>
                </label>
              </li>
            ))}
          </ul>
        </>
      ) : (
        <Callout tone="info">{t.providers.noCandidates}</Callout>
      )}
      <Textarea
        aria-label={t.providers.manualModels}
        spellCheck={false}
        className="min-h-16 font-mono text-xs"
        placeholder={t.providers.manualModelsHint}
        value={manualNames}
        onChange={(event) => setManualNames(event.target.value)}
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
      <div className="flex flex-wrap items-center gap-1.5">
        {(catalog ? formatsForKind(catalog, kind) : []).map((entry) => (
          <Badge key={entry}>{entry}</Badge>
        ))}
      </div>
      <label className="flex items-center gap-2 text-sm">
        <Switch checked={enabled} onCheckedChange={setEnabled} />
        {t.common.enabled}
      </label>
    </div>
  );

  const routeStep = (
    <div className="flex flex-col gap-4">
      <label className="flex items-center gap-2 text-sm">
        <Switch checked={routeEnabled} onCheckedChange={setRouteEnabled} />
        {t.providers.createRoute}
      </label>
      {routeEnabled ? (
        <div className="flex flex-col gap-3">
          <Select
            aria-label={t.providers.project}
            value={projectId}
            options={(projects.data ?? []).map((project) => ({
              value: project.id,
              label: project.name,
            }))}
            onValueChange={setProjectId}
          />
          <Input
            aria-label={t.channels.name}
            placeholder={t.providers.routeNamePlaceholder}
            className="font-mono"
            value={routeName}
            onChange={(event) => setRouteName(event.target.value)}
          />
          <div className="flex flex-wrap items-center gap-1.5">
            {modelNames.map((name) => (
              <Badge key={name}>{name}</Badge>
            ))}
          </div>
          <p className="text-xs text-faint">
            {interpolate(t.providers.routeTargets, { count: modelNames.length })}
          </p>
          <IntegerInput
            aria-label={t.channels.defaultMaxOutput}
            value={routeMaxOutput}
            onValueChange={setRouteMaxOutput}
          />
        </div>
      ) : (
        <p className="text-xs text-faint">{t.providers.routeOptional}</p>
      )}
      <Callout tone="info">{t.providers.finishHint}</Callout>
    </div>
  );

  const steps: WizardStep[] = [
    {
      key: 'provider',
      title: t.providers.stepProvider,
      description: t.providers.stepProviderHint,
      content: providerStep,
      blocked: mode === 'custom' && (customLabel.trim() === '' || !catalog),
    },
    {
      key: 'channel',
      title: t.providers.stepChannel,
      description: t.providers.stepChannelHint,
      content: channelStep,
      blocked: !canConnect,
    },
    {
      key: 'discovery',
      title: t.providers.stepDiscover,
      description: t.providers.stepDiscoverHint,
      content: discoveryStep,
      blocked: !connected,
    },
    {
      key: 'models',
      title: t.providers.stepModels,
      description: t.providers.stepModelsHint,
      content: modelsStep,
    },
    {
      key: 'route',
      title: t.providers.stepRoute,
      description: t.providers.stepRouteHint,
      content: routeStep,
      blocked: routeEnabled && (projectId === '' || modelNames.length === 0),
    },
  ];

  return (
    <Wizard
      open={open}
      onOpenChange={onOpenChange}
      title={t.providers.setupTitle}
      description={t.providers.setupDescription}
      steps={steps}
      finishLabel={t.providers.finish}
      pending={pending}
      onSubmit={() => void finish()}
      wide
    />
  );
}
