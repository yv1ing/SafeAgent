import { useQuery } from '@tanstack/react-query';
import {
  createModel,
  createModelBatch,
  getDeleteModelUrl,
  getListModelsQueryKey,
  getListModelsUrl,
  restoreModel,
  updateModel,
} from '../../api/generated/models/models';
import { getGetConfigurationHealthQueryKey } from '../../api/generated/configuration/configuration';
import { getRoute, updateRoute } from '../../api/generated/routes/routes';
import type {
  ConsoleCatalogModelBatchInput,
  ConsoleCatalogModelInput,
  ConsoleCatalogModelView,
  ConsoleImpactView,
  ConsoleUpdateCatalogModelInput,
} from '../../api/generated/models';
import { request } from '../../api/client';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';
import { manualAliases, rewireTargets } from '../routes/targets';
import { modelInput } from './models';

const projectKeys = ['/console/api/projects'];

function modelInvalidations() {
  return [getListModelsQueryKey(), projectKeys, getGetConfigurationHealthQueryKey()];
}

export interface ModelQueryFilter {
  providerId?: string;
  unassociated?: boolean;
  includeDeleted?: boolean;
}

export function useModels(filter: ModelQueryFilter) {
  const params = new URLSearchParams();
  if (filter.providerId) params.set('provider_id', filter.providerId);
  if (filter.unassociated) params.set('unassociated', 'true');
  if (filter.includeDeleted) params.set('include_deleted', 'true');
  const query = params.toString();
  return useQuery<ConsoleCatalogModelView[]>({
    queryKey: [...getListModelsQueryKey(), { ...filter }],
    queryFn: () =>
      request<ConsoleCatalogModelView[]>(
        query ? `${getListModelsUrl()}?${query}` : getListModelsUrl(),
      ),
  });
}

export function useCreateModel() {
  const { t } = useLocale();
  return useConsoleMutation<ConsoleCatalogModelInput, ConsoleCatalogModelView>({
    mutationFn: (input) => createModel(input),
    invalidates: modelInvalidations(),
    success: t.common.created,
  });
}

export function useCreateModelBatch() {
  const { t } = useLocale();
  return useConsoleMutation<ConsoleCatalogModelBatchInput, ConsoleCatalogModelView[]>({
    mutationFn: (input) => createModelBatch(input),
    invalidates: modelInvalidations(),
    success: t.models.imported,
  });
}

export function useUpdateModel(modelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<ConsoleUpdateCatalogModelInput, ConsoleCatalogModelView>({
    mutationFn: (input) => updateModel(modelId, input),
    invalidates: [...modelInvalidations(), [`/console/api/models/${modelId}`]],
    success: t.common.saved,
  });
}

export interface ToggleModelVariables {
  model: ConsoleCatalogModelView;
  enabled: boolean;
}

export function useToggleModel(modelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<ToggleModelVariables, ConsoleCatalogModelView>({
    mutationFn: ({ model, enabled }) => updateModel(modelId, { ...modelInput(model), enabled }),
    invalidates: [...modelInvalidations(), [`/console/api/models/${modelId}`]],
    success: t.common.saved,
  });
}

export interface DeleteModelVariables {
  model: ConsoleCatalogModelView;
  impact: ConsoleImpactView;
  replacementId?: string;
}

export function useDeleteModel() {
  const { t } = useLocale();
  return useConsoleMutation<DeleteModelVariables, void>({
    mutationFn: async ({ model, impact, replacementId }) => {
      if (replacementId) {
        for (const reference of impact.routes) {
          const route = await getRoute(reference.project_id, reference.route_id);
          await updateRoute(reference.project_id, reference.route_id, {
            name: route.name,
            enabled: route.enabled,
            aliases: manualAliases(route),
            targets: rewireTargets(route.targets, 'model_id', model.id, replacementId),
            version: route.version,
          });
        }
      }
      await request<void>(`${getDeleteModelUrl(model.id)}?acknowledge=true`, { method: 'DELETE' });
    },
    invalidates: modelInvalidations(),
    success: t.common.deleted,
  });
}

export function useRestoreModel(modelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<void, ConsoleCatalogModelView>({
    mutationFn: () => restoreModel(modelId),
    invalidates: [...modelInvalidations(), [`/console/api/models/${modelId}`]],
    success: t.models.restored,
  });
}
