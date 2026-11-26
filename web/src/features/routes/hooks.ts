import { useQuery } from '@tanstack/react-query';
import {
  createRoute,
  deleteRoute,
  getGetRouteQueryKey,
  getListRoutesQueryKey,
  getListRoutesUrl,
  restoreRoute,
  updateRoute,
} from '../../api/generated/routes/routes';
import type {
  ConsoleCatalogModelView,
  ConsoleCreateRouteInput,
  ConsoleRouteView,
  ConsoleUpdateRouteInput,
} from '../../api/generated/models';
import { request } from '../../api/client';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';
import { manualAliases, routeTargetsInput } from './targets';

function routeInvalidations(projectId: string, routeId?: string) {
  return [
    getListRoutesQueryKey(projectId),
    ...(routeId ? [getGetRouteQueryKey(projectId, routeId)] : []),
  ];
}

export function useRoutes(projectId: string, includeDeleted: boolean) {
  return useQuery<ConsoleRouteView[]>({
    queryKey: [...getListRoutesQueryKey(projectId), { includeDeleted }],
    queryFn: () =>
      request<ConsoleRouteView[]>(
        includeDeleted
          ? `${getListRoutesUrl(projectId)}?include_deleted=true`
          : getListRoutesUrl(projectId),
      ),
  });
}

export function useRouteMutations(projectId: string) {
  const { t } = useLocale();
  return {
    create: useConsoleMutation<ConsoleCreateRouteInput, ConsoleRouteView>({
      mutationFn: (input) => createRoute(projectId, input),
      invalidates: routeInvalidations(projectId),
      success: t.common.created,
    }),
    update: useConsoleMutation<
      { routeId: string; input: ConsoleUpdateRouteInput },
      ConsoleRouteView
    >({
      mutationFn: ({ routeId, input }) => updateRoute(projectId, routeId, input),
      invalidates: routeInvalidations(projectId),
      success: t.common.saved,
    }),
    remove: useConsoleMutation<string, void>({
      mutationFn: (routeId) => deleteRoute(projectId, routeId),
      invalidates: [...routeInvalidations(projectId), ['/console/api/projects']],
      success: t.common.deleted,
    }),
    restore: useConsoleMutation<string, ConsoleRouteView>({
      mutationFn: (routeId) => restoreRoute(projectId, routeId),
      invalidates: routeInvalidations(projectId),
      success: t.routes.restored,
    }),
  };
}

export interface ToggleRouteVariables {
  route: ConsoleRouteView;
  enabled: boolean;
}

export function useToggleRoute(projectId: string) {
  const { t } = useLocale();
  return useConsoleMutation<ToggleRouteVariables, ConsoleRouteView>({
    mutationFn: ({ route, enabled }) =>
      updateRoute(projectId, route.id, {
        name: route.name,
        enabled,
        aliases: manualAliases(route),
        targets: routeTargetsInput(route),
        version: route.version,
      }),
    invalidates: routeInvalidations(projectId),
    success: t.common.saved,
  });
}

export interface RouteFromModel {
  model: ConsoleCatalogModelView;
  channelId: string;
}

export function useRoutesFromModels(projectId: string) {
  const { t } = useLocale();
  return useConsoleMutation<RouteFromModel[], number>({
    mutationFn: async (items) => {
      for (const item of items) {
        await createRoute(projectId, {
          name: item.model.upstream_name,
          targets: [{ channel_id: item.channelId, model_id: item.model.id, enabled: true }],
        });
      }
      return items.length;
    },
    invalidates: routeInvalidations(projectId),
    success: t.routes.fromModelsDone,
  });
}
