import type { ConsoleProjectCreate, ConsoleUpdateProjectInput } from '../../api/generated/models';
import {
  createProject,
  deleteProject,
  getGetProjectQueryKey,
  getListProjectsQueryKey,
  updateProject,
} from '../../api/generated/projects/projects';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

export function useProjectMutations() {
  const { t } = useLocale();
  const invalidates = [getListProjectsQueryKey()];
  return {
    create: useConsoleMutation({
      mutationFn: (input: ConsoleProjectCreate) => createProject(input),
      invalidates,
      success: t.common.created,
    }),
    remove: useConsoleMutation({
      mutationFn: (projectId: string) => deleteProject(projectId),
      invalidates,
      success: t.common.deleted,
    }),
  };
}

export function useUpdateProject(projectId: string) {
  const { t } = useLocale();
  return useConsoleMutation({
    mutationFn: (input: ConsoleUpdateProjectInput) => updateProject(projectId, input),
    invalidates: [getListProjectsQueryKey(), getGetProjectQueryKey(projectId)],
    success: t.common.saved,
  });
}
