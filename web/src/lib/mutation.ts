import { useMutation, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { toast } from 'sonner';
import { errorMessage, useLocale } from '../i18n';

export function useConsoleMutation<TVariables, TResult>({
  mutationFn,
  invalidates,
  success,
}: {
  mutationFn: (variables: TVariables) => Promise<TResult>;
  invalidates: QueryKey[];
  success?: string;
}) {
  const queryClient = useQueryClient();
  const { t } = useLocale();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await Promise.all(invalidates.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
      if (success) toast.success(success);
    },
    onError: (error) => toast.error(errorMessage(t, error)),
  });
}
