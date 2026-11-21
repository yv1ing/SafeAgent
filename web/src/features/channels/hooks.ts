import {
  createChannel,
  createChannelKey,
  discoverChannelModels,
  deleteChannelKey,
  getGetChannelQueryKey,
  getListChannelKeysQueryKey,
  getListChannelsQueryKey,
  updateChannel,
} from '../../api/generated/channels/channels';
import type {
  ConsoleChannelInput,
  ConsoleCreateChannelKeyInput,
  ConsoleDiscoveryView,
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

export function useDiscoverChannelModels(channelId: string) {
  const { t } = useLocale();
  return useConsoleMutation<void, ConsoleDiscoveryView>({
    mutationFn: () => discoverChannelModels(channelId),
    invalidates: [],
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
