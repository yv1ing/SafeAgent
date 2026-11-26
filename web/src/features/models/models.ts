import type {
  ConsoleCatalogModelInput,
  ConsoleCatalogModelView,
  ConsoleUpdateCatalogModelInput,
} from '../../api/generated/models';

export interface ProviderModelGroup {
  providerId: string;
  label: string;
  models: ConsoleCatalogModelView[];
}

// Models are a provider-level catalog, so the page groups a provider's rows together and keeps the
// catalog order the API returned inside each group.
export function groupModelsByProvider(
  models: readonly ConsoleCatalogModelView[],
  providerLabels: Map<string, string>,
): ProviderModelGroup[] {
  const groups = new Map<string, ConsoleCatalogModelView[]>();
  for (const model of models) {
    const bucket = groups.get(model.provider_id);
    if (bucket) {
      bucket.push(model);
    } else {
      groups.set(model.provider_id, [model]);
    }
  }
  return [...groups.entries()]
    .map(([providerId, entries]) => ({
      providerId,
      label: providerLabels.get(providerId) ?? providerId,
      models: entries,
    }))
    .sort((left, right) => left.label.localeCompare(right.label));
}

export function modelInput(model: ConsoleCatalogModelView): ConsoleUpdateCatalogModelInput {
  return {
    provider_id: model.provider_id,
    upstream_name: model.upstream_name,
    display_name: model.display_name ?? '',
    kind: model.kind,
    input_price: model.input_price ?? 0,
    output_price: model.output_price ?? 0,
    cache_read_price: model.cache_read_price ?? 0,
    cache_write_price: model.cache_write_price ?? 0,
    currency: model.currency,
    enabled: model.enabled ?? true,
    version: model.version,
  };
}

export function modelBatchInputs(
  providerId: string,
  names: readonly string[],
  base: Pick<ConsoleCatalogModelInput, 'kind' | 'currency' | 'enabled'>,
): ConsoleCatalogModelInput[] {
  return names.map((upstreamName) => ({
    provider_id: providerId,
    upstream_name: upstreamName,
    kind: base.kind,
    currency: base.currency,
    enabled: base.enabled,
  }));
}
