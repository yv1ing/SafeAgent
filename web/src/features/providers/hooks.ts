import { useQuery } from '@tanstack/react-query';
import { getListChannelsQueryKey } from '../../api/generated/channels/channels';
import {
  connectProvider,
  getGetConfigurationHealthQueryKey,
  getGetConfigurationImpactQueryKey,
  getGetConfigurationImpactUrl,
} from '../../api/generated/configuration/configuration';
import type { ConsoleImpactView } from '../../api/generated/models';
import { getListModelsQueryKey } from '../../api/generated/models/models';
import {
  createProvider,
  deleteProvider,
  getListProvidersQueryKey,
  resetProvider,
  updateProvider,
} from '../../api/generated/providers/providers';
import type {
  ConsoleConnectInput,
  ConsoleConnectView,
  ConsoleProviderInput,
  ConsoleUpdateProviderInput,
} from '../../api/generated/models';
import { request } from '../../api/client';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

const controlKeys = [
  getListProvidersQueryKey(),
  getListChannelsQueryKey(),
  getListModelsQueryKey(),
];

export function useCreateProvider() {
  const { t } = useLocale();
  return useConsoleMutation<ConsoleProviderInput, unknown>({
    mutationFn: (input) => createProvider(input),
    invalidates: [...controlKeys, getGetConfigurationHealthQueryKey()],
    success: t.common.created,
  });
}

export function useUpdateProvider(providerId: string) {
  const { t } = useLocale();
  return useConsoleMutation<ConsoleUpdateProviderInput, unknown>({
    mutationFn: (input) => updateProvider(providerId, input),
    invalidates: [
      ...controlKeys,
      getGetConfigurationHealthQueryKey(),
      [`/console/api/providers/${providerId}`],
    ],
    success: t.common.saved,
  });
}

export function useDeleteProvider() {
  const { t } = useLocale();
  return useConsoleMutation<string, void>({
    mutationFn: (providerId) => deleteProvider(providerId),
    invalidates: [...controlKeys, getGetConfigurationHealthQueryKey()],
    success: t.common.deleted,
  });
}

export function useResetProvider() {
  const { t } = useLocale();
  return useConsoleMutation<string, unknown>({
    mutationFn: (providerId) => resetProvider(providerId),
    invalidates: [...controlKeys, getGetConfigurationHealthQueryKey()],
    success: t.providers.resetDone,
  });
}

export function useConnectProvider() {
  const { t } = useLocale();
  return useConsoleMutation<ConsoleConnectInput, ConsoleConnectView>({
    mutationFn: (input) => connectProvider(input),
    invalidates: [...controlKeys, getGetConfigurationHealthQueryKey(), ['/console/api/projects']],
    success: t.providers.connected,
  });
}

export function useConfigurationImpact(target: { channelId: string } | { modelId: string } | null) {
  const params =
    target && 'channelId' in target
      ? `channel_id=${encodeURIComponent(target.channelId)}`
      : target
        ? `model_id=${encodeURIComponent(target.modelId)}`
        : '';
  return useQuery<ConsoleImpactView>({
    queryKey: [...getGetConfigurationImpactQueryKey(), target],
    enabled: params !== '',
    queryFn: () => request<ConsoleImpactView>(`${getGetConfigurationImpactUrl()}?${params}`),
  });
}
