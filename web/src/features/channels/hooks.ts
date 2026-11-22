import {
  createChannel,
  createChannelKey,
  deleteChannel,
  deleteChannelKey,
  discoverChannelModels,
  getGetChannelQueryKey,
  getListChannelKeysQueryKey,
  getListChannelsQueryKey,
  importChannelModels,
  updateChannel,
} from '../../api/generated/channels/channels';
import { getListModelsQueryKey } from '../../api/generated/models/models';
import type {
  ConsoleChannelInput,
  ConsoleCreateChannelKeyInput,
  ConsoleDiscoveryView,
  ConsoleImportModelsView,
  ConsoleUpdateChannelInput,
} from '../../api/generated/models';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

export function useCreateChannel() {
  const { t } = useLocale();
  return useConsoleMutation({
    mutationFn: (input: ConsoleChannelInput) => createChannel(input),
    invalidates: [getListChannelsQueryKey()],
    success: t.common.created,
  });
}

export function useUpdateChannel(channelId: string) {
  const { t } = useLocale();
  return useConsoleMutation({
    mutationFn: (input: ConsoleUpdateChannelInput) => updateChannel(channelId, input),
    invalidates: [getListChannelsQueryKey(), getGetChannelQueryKey(channelId)],
    success: t.common.saved,
  });
}

export function useDeleteChannel() {
  const { t } = useLocale();
  return useConsoleMutation({
    mutationFn: (channelId: string) => deleteChannel(channelId),
    invalidates: [getListChannelsQueryKey()],
    success: t.common.deleted,
  });
}

export function useDiscoverChannelModels(channelId: string) {
  return useConsoleMutation<string, ConsoleDiscoveryView>({
    mutationFn: (format: string) => discoverChannelModels(channelId, { format }),
    invalidates: [],
  });
}

export function useImportChannelModels(channelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<{ format: string; models: string[] }, ConsoleImportModelsView>({
    mutationFn: ({ format, models }: { format: string; models: string[] }) =>
      importChannelModels(channelId, { format, models }),
    invalidates: [
      getGetChannelQueryKey(channelId),
      getListChannelsQueryKey(),
      getListModelsQueryKey(),
    ],
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
