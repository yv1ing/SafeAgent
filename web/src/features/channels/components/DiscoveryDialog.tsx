import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { FormDialog } from '../../../components/form/FormDialog';
import { interpolate, useLocale } from '../../../i18n';
import { useDiscoverChannelModels, useImportChannelModels } from '../hooks';

export function DiscoveryDialog({
  channelId,
  format,
  onClose,
}: {
  channelId: string;
  format: string;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const discover = useDiscoverChannelModels(channelId);
  const importModels = useImportChannelModels(channelId);
  const [selected, setSelected] = useState<string[]>([]);
  useEffect(() => {
    discover.mutate(format, {
      onSuccess: (result) => setSelected(result.models ?? []),
    });
  }, [channelId, format]);
  const names = discover.data?.models ?? [];
  const toggle = (name: string) =>
    setSelected(
      selected.includes(name) ? selected.filter((entry) => entry !== name) : [...selected, name],
    );
  return (
    <FormDialog
      open
      onOpenChange={(open) => !open && onClose()}
      title={interpolate(t.channels.discoverTitle, { format })}
      description={t.channels.discoverHint}
      submitLabel={t.channels.importSelected}
      pending={importModels.isPending}
      disabled={discover.isPending || selected.length === 0}
      onSubmit={() =>
        importModels.mutate(
          { format, models: selected },
          {
            onSuccess: (result) => {
              if (result.skipped?.length) {
                toast.warning(
                  interpolate(t.channels.skipped, { models: result.skipped.join(', ') }),
                );
              }
              if (result.created?.length) {
                toast.success(interpolate(t.channels.imported, { count: result.created.length }));
              }
              onClose();
            },
          },
        )
      }
    >
      {discover.isPending && <p className="text-sm text-muted">{t.common.loading}</p>}
      {!discover.isPending && names.length === 0 && (
        <p className="text-sm text-muted">{t.channels.noDiscovery}</p>
      )}
      <ul className="scroll-thin flex max-h-80 flex-col gap-1 overflow-y-auto">
        {names.map((name) => (
          <li key={name}>
            <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-sunken">
              <input
                type="checkbox"
                className="size-3.5 accent-accent"
                checked={selected.includes(name)}
                onChange={() => toggle(name)}
              />
              <span className="font-mono">{name}</span>
            </label>
          </li>
        ))}
      </ul>
      <p className="text-xs text-faint">{t.channels.importHint}</p>
    </FormDialog>
  );
}
