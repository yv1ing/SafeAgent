import { Plus, X } from 'lucide-react';
import { useState } from 'react';
import { useListChannelOptions } from '../../../api/generated/channel-options/channel-options';
import type {
  ConsoleCreateRouteInput,
  ConsoleRouteView,
  ConsoleUpdateRouteInput,
} from '../../../api/generated/models';
import { Callout } from '../../../components/feedback/Callout';
import { QueryState } from '../../../components/feedback/QueryState';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { Input } from '../../../components/ui/input';
import { Switch } from '../../../components/ui/switch';
import { interpolate, useLocale } from '../../../i18n';
import { manualAliases, targetInput, validateTargets, type DraftTarget } from '../targets';
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
  onSubmit: (input: ConsoleCreateRouteInput | ConsoleUpdateRouteInput) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const channels = useListChannelOptions(projectId, { query: { refetchOnMount: 'always' } });
  const [name, setName] = useState(route?.name ?? '');
  const [enabled, setEnabled] = useState(route?.enabled ?? true);
  const [targets, setTargets] = useState<DraftTarget[]>(() =>
    (route?.targets ?? []).map((target) => ({
      ...targetInput(target),
      priority: target.priority,
      weight: target.weight,
    })),
  );
  const [aliases, setAliases] = useState<string[]>(() => (route ? manualAliases(route) : []));
  const renamed = (route?.aliases ?? []).filter((alias) => alias.source === 'renamed');
  const channelRows = channels.data ?? [];
  const problem = validateTargets(targets, channelRows);
  const valid = name.trim() !== '' && problem === null;
  return (
    <FormDialog
      open
      wide
      onOpenChange={(open) => !open && onClose()}
      title={route ? interpolate(t.routes.edit, { name: route.name }) : t.routes.add}
      description={route ? t.routes.editHint : t.routes.addHint}
      submitLabel={route ? t.common.save : t.common.create}
      pending={pending}
      disabled={!valid}
      onSubmit={() =>
        onSubmit({
          name: name.trim(),
          enabled,
          aliases: aliases.filter((alias) => alias.trim() !== ''),
          targets,
          version: route?.version,
        })
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
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
        <label className="flex items-center gap-2 self-end pb-2 text-sm">
          <Switch checked={enabled} onCheckedChange={setEnabled} />
          {t.common.enabled}
        </label>
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1.5 text-xs font-medium text-muted">{t.routes.aliases}</legend>
        <p className="text-xs text-faint">{t.routes.aliasHint}</p>
        {aliases.map((alias, index) => (
          <div key={index} className="flex gap-2">
            <Input
              aria-label={t.routes.alias}
              className="font-mono"
              value={alias}
              onChange={(event) =>
                setAliases(
                  aliases.map((entry, position) =>
                    position === index ? event.target.value : entry,
                  ),
                )
              }
            />
            <Button
              size="icon"
              variant="ghost"
              aria-label={t.routes.removeAlias}
              onClick={() => setAliases(aliases.filter((_, position) => position !== index))}
            >
              <X aria-hidden />
            </Button>
          </div>
        ))}
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={() => setAliases([...aliases, ''])}>
            <Plus aria-hidden />
            {t.routes.addAlias}
          </Button>
          {renamed.map((alias) => (
            <Badge key={alias.alias} tone="neutral">
              {alias.alias} · {t.routes.renamedAlias}
            </Badge>
          ))}
        </div>
      </fieldset>
      <QueryState
        query={channels}
        loading={<p className="text-sm text-muted">{t.common.loading}</p>}
      >
        {(rows) => <TargetEditor targets={targets} channels={rows} onChange={setTargets} />}
      </QueryState>
      {channels.data && problem === null && targets.length === 0 && (
        <Callout tone="warning">{t.routes.targetsRequired}</Callout>
      )}
    </FormDialog>
  );
}
