import {
  createKey,
  getListKeysQueryKey,
  revokeKey,
  updateKey,
} from '../../api/generated/keys/keys';
import type { ConsoleCreateKeyInput, ConsoleUpdateKeyInput } from '../../api/generated/models';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

export function useKeyMutations(projectId: string) {
  const { t } = useLocale();
  const invalidates = [getListKeysQueryKey(projectId)];
  return {
    create: useConsoleMutation({
      mutationFn: (input: ConsoleCreateKeyInput) => createKey(projectId, input),
      invalidates,
    }),
    update: useConsoleMutation({
      mutationFn: ({ keyId, input }: { keyId: string; input: ConsoleUpdateKeyInput }) =>
        updateKey(projectId, keyId, input),
      invalidates,
      success: t.common.saved,
    }),
    revoke: useConsoleMutation({
      mutationFn: (keyId: string) => revokeKey(projectId, keyId),
      invalidates,
      success: t.keys.revoked,
    }),
  };
}
