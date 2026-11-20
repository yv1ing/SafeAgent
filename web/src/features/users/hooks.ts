import type { ConsoleUpdateUserInput, ConsoleUserInput } from '../../api/generated/models';
import {
  createUser,
  getListUsersQueryKey,
  resetPassword,
  updateUser,
} from '../../api/generated/users/users';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

export function useUserMutations() {
  const { t } = useLocale();
  const invalidates = [getListUsersQueryKey()];
  return {
    create: useConsoleMutation({
      mutationFn: (input: ConsoleUserInput) => createUser(input),
      invalidates,
    }),
    update: useConsoleMutation({
      mutationFn: ({ userId, input }: { userId: string; input: ConsoleUpdateUserInput }) =>
        updateUser(userId, input),
      invalidates,
      success: t.common.saved,
    }),
    resetPassword: useConsoleMutation({
      mutationFn: (userId: string) => resetPassword(userId),
      invalidates,
    }),
  };
}
