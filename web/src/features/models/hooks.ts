import {
  createModel,
  deleteModel,
  getListModelsQueryKey,
  updateModel,
} from '../../api/generated/models/models';
import type {
  ConsoleCatalogModelInput,
  ConsoleUpdateCatalogModelInput,
} from '../../api/generated/models';
import { useLocale } from '../../i18n';
import { useConsoleMutation } from '../../lib/mutation';

export function useModelMutations() {
  const { t } = useLocale();
  const invalidates = [getListModelsQueryKey()];
  return {
    create: useConsoleMutation({
      mutationFn: (input: ConsoleCatalogModelInput) => createModel(input),
      invalidates,
      success: t.common.created,
    }),
    update: useConsoleMutation({
      mutationFn: ({
        modelId,
        input,
      }: {
        modelId: string;
        input: ConsoleUpdateCatalogModelInput;
      }) => updateModel(modelId, input),
      invalidates,
      success: t.common.saved,
    }),
    remove: useConsoleMutation({
      mutationFn: (modelId: string) => deleteModel(modelId),
      invalidates,
      success: t.common.deleted,
    }),
  };
}
