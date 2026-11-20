import type { ConsoleCreateRouteInput } from '../../api/generated/models';
import {
  createRoute,
  deleteRoute,
  getListRoutesQueryKey,
  updateRoute,
} from '../../api/generated/routes/routes';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

export function useRouteMutations(projectId: string) {
  const { t } = useLocale();
  const invalidates = [getListRoutesQueryKey(projectId)];
  return {
    create: useConsoleMutation({
      mutationFn: (input: ConsoleCreateRouteInput) => createRoute(projectId, input),
      invalidates,
      success: t.common.created,
    }),
    update: useConsoleMutation({
      mutationFn: ({ routeId, input }: { routeId: string; input: ConsoleCreateRouteInput }) =>
        updateRoute(projectId, routeId, input),
      invalidates,
      success: t.common.saved,
    }),
    remove: useConsoleMutation({
      mutationFn: (routeId: string) => deleteRoute(projectId, routeId),
      invalidates,
      success: t.common.deleted,
    }),
  };
}
