import { useState } from 'react';
import { useListChannelOptions } from '../../../api/generated/channel-options/channel-options';
import type { ConsoleCreateRouteInput, ConsoleRouteView } from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Input } from '../../../components/ui/input';
import { interpolate, useLocale } from '../../../i18n';
import { TargetEditor } from './TargetEditor';

export function RouteDialog({
  projectId,
  route,
  pending,
  onSubmit,
  onClose,
}: {
  projectId: string;
  route: ConsoleRouteView | null;
  pending: boolean;
  onSubmit: (input: ConsoleCreateRouteInput) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const channels = useListChannelOptions(projectId, { query: { refetchOnMount: 'always' } });
  const [name, setName] = useState(route?.name ?? '');
  const [targets, setTargets] = useState(route?.targets ?? []);
  return (
    <FormDialog
      open
      wide
      onOpenChange={(open) => !open && onClose()}
      title={route ? interpolate(t.routes.edit, { name: route.name }) : t.routes.add}
      submitLabel={route ? t.common.save : t.common.create}
      pending={pending}
      onSubmit={() => onSubmit({ name, targets, version: route?.version })}
    >
      <Field label={t.common.name} hint={t.routes.nameHint}>
        {(id) => (
          <Input
            id={id}
            required
            autoFocus
            className="font-mono"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        )}
      </Field>
      <TargetEditor targets={targets} channels={channels.data ?? []} onChange={setTargets} />
    </FormDialog>
  );
}
