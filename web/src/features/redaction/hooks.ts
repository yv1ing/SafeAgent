import type { ConsoleRuleInput } from '../../api/generated/models';
import {
  createRule,
  deleteRule,
  getListRulesQueryKey,
  updateRule,
} from '../../api/generated/rules/rules';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

export function useRuleMutations(projectId: string) {
  const { t } = useLocale();
  const invalidates = [getListRulesQueryKey(projectId)];
  return {
    create: useConsoleMutation({
      mutationFn: (input: ConsoleRuleInput) => createRule(projectId, input),
      invalidates,
      success: t.common.created,
    }),
    update: useConsoleMutation({
      mutationFn: ({ ruleId, input }: { ruleId: string; input: ConsoleRuleInput }) =>
        updateRule(projectId, ruleId, input),
      invalidates,
      success: t.common.saved,
    }),
    remove: useConsoleMutation({
      mutationFn: (ruleId: string) => deleteRule(projectId, ruleId),
      invalidates,
      success: t.common.deleted,
    }),
  };
}
