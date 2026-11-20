import {
  addMember,
  getListMembersQueryKey,
  removeMember,
} from '../../api/generated/members/members';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

export function useMemberMutations(projectId: string) {
  const { t } = useLocale();
  const invalidates = [getListMembersQueryKey(projectId)];
  return {
    add: useConsoleMutation({
      mutationFn: (userId: string) => addMember(projectId, { user_id: userId }),
      invalidates,
      success: t.common.saved,
    }),
    remove: useConsoleMutation({
      mutationFn: (userId: string) => removeMember(projectId, userId),
      invalidates,
      success: t.common.saved,
    }),
  };
}
