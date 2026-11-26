import { useQuery } from '@tanstack/react-query';
import {
  createChannel,
  createChannelKey,
  deleteChannelKey,
  detachChannelTargets,
  discoverChannelModels,
  getDeleteChannelUrl,
  getGetChannelQueryKey,
  getListChannelKeysQueryKey,
  getListChannelsQueryKey,
  getListChannelsUrl,
  importChannelModels,
  restoreChannel,
  updateChannel,
} from '../../api/generated/channels/channels';
import { getGetConfigurationHealthQueryKey } from '../../api/generated/configuration/configuration';
import { getListModelsQueryKey } from '../../api/generated/models/models';
import { getRoute, updateRoute } from '../../api/generated/routes/routes';
import type {
  ConsoleChannelInput,
  ConsoleChannelView,
  ConsoleCreateChannelKeyInput,
  ConsoleDiscoveryView,
  ConsoleImpactView,
  ConsoleImportModelsView,
  ConsoleUpdateChannelInput,
} from '../../api/generated/models';
import { request } from '../../api/client';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';
import { manualAliases, rewireTargets } from '../routes/targets';
import { channelInput } from './channel';

const projectKeys = ['/console/api/projects'];

function channelInvalidations(channelId?: string) {
  return [
    getListChannelsQueryKey(),
    projectKeys,
    getGetConfigurationHealthQueryKey(),
    ...(channelId ? [getGetChannelQueryKey(channelId)] : []),
  ];
}

export function useChannels(includeDeleted: boolean) {
  return useQuery<ConsoleChannelView[]>({
    queryKey: [...getListChannelsQueryKey(), { includeDeleted }],
    queryFn: () =>
      request<ConsoleChannelView[]>(
        includeDeleted ? `${getListChannelsUrl()}?include_deleted=true` : getListChannelsUrl(),
      ),
  });
}

export function useCreateChannel() {
  const { t } = useLocale();
  return useConsoleMutation<ConsoleChannelInput, ConsoleChannelView>({
    mutationFn: (input) => createChannel(input),
    invalidates: [...channelInvalidations(), getListModelsQueryKey()],
    success: t.common.created,
  });
}

export function useUpdateChannel(channelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<ConsoleUpdateChannelInput, ConsoleChannelView>({
    mutationFn: (input) => updateChannel(channelId, input),
    invalidates: channelInvalidations(channelId),
    success: t.common.saved,
  });
}

export interface DeleteChannelVariables {
  channel: ConsoleChannelView;
  impact: ConsoleImpactView;
  replacementId?: string;
}

export function useDeleteChannel() {
  const { t } = useLocale();
  return useConsoleMutation<DeleteChannelVariables, void>({
    mutationFn: async ({ channel, impact, replacementId }) => {
      if (replacementId) {
        for (const reference of impact.routes) {
          const route = await getRoute(reference.project_id, reference.route_id);
          await updateRoute(reference.project_id, reference.route_id, {
            name: route.name,
            enabled: route.enabled,
            aliases: manualAliases(route),
            targets: rewireTargets(route.targets, 'channel_id', channel.id, replacementId),
            version: route.version,
          });
        }
      }
      await request<void>(`${getDeleteChannelUrl(channel.id)}?acknowledge=true`, {
        method: 'DELETE',
      });
    },
    invalidates: channelInvalidations(),
    success: t.common.deleted,
  });
}

export function useRestoreChannel(channelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<void, ConsoleChannelView>({
    mutationFn: () => restoreChannel(channelId),
    invalidates: channelInvalidations(channelId),
    success: t.channels.restored,
  });
}

export interface ToggleChannelVariables {
  channel: ConsoleChannelView;
  enabled: boolean;
}

export function useToggleChannel(channelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<ToggleChannelVariables, ConsoleChannelView>({
    mutationFn: ({ channel, enabled }) =>
      updateChannel(channelId, { ...channelInput(channel), enabled }),
    invalidates: channelInvalidations(channelId),
    success: t.common.saved,
  });
}

export function useDetachChannelTargets(channelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<void, void>({
    mutationFn: () => detachChannelTargets(channelId),
    invalidates: channelInvalidations(channelId),
    success: t.channels.detached,
  });
}

export function useDiscoverChannelModels(channelId: string) {
  return useConsoleMutation<string, ConsoleDiscoveryView>({
    mutationFn: (format) => discoverChannelModels(channelId, { format }),
    invalidates: [],
  });
}

export function useImportChannelModels(channelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<{ format: string; models: string[] }, ConsoleImportModelsView>({
    mutationFn: ({ format, models }) => importChannelModels(channelId, { format, models }),
    invalidates: [...channelInvalidations(channelId), getListModelsQueryKey()],
    success: t.channels.discovered,
  });
}

export function useChannelKeyMutations(channelId: string) {
  const { t } = useLocale();
  const invalidates = [getListChannelKeysQueryKey(channelId)];
  return {
    create: useConsoleMutation({
      mutationFn: (input: ConsoleCreateChannelKeyInput) => createChannelKey(channelId, input),
      invalidates,
      success: t.common.created,
    }),
    remove: useConsoleMutation({
      mutationFn: (keyId: string) => deleteChannelKey(channelId, keyId),
      invalidates,
      success: t.common.deleted,
    }),
  };
}
