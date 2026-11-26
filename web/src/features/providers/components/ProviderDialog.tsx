import { useState } from 'react';
import type { ConsoleProviderView } from '../../../api/generated/models';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { Badge } from '../../../components/ui/badge';
import { Input } from '../../../components/ui/input';
import { Switch } from '../../../components/ui/switch';
import { useLocale } from '../../../i18n';
import { endpointsToText, type EndpointCatalog, validateEndpointCatalog } from '../endpoints';
import { EndpointsEditor } from './EndpointsEditor';

export interface ProviderDraft {
  label: string;
  enabled: boolean;
  endpoints: EndpointCatalog;
}

export function ProviderDialog({
  provider,
  pending,
  onSubmit,
  onClose,
}: {
  provider: ConsoleProviderView | null;
  pending: boolean;
  onSubmit: (draft: ProviderDraft) => void;
  onClose: () => void;
}) {
  const { t } = useLocale();
  const [label, setLabel] = useState(provider?.label ?? '');
  const [enabled, setEnabled] = useState(provider?.enabled ?? true);
  const [text, setText] = useState(() => endpointsToText(provider?.endpoints ?? {}));
  const parsed = validateEndpointCatalog(text);
  const valid = parsed.catalog !== undefined && label.trim() !== '';
  return (
    <FormDialog
      open
      wide
      onOpenChange={(open) => !open && onClose()}
      title={provider ? t.providers.edit : t.providers.add}
      description={provider ? t.providers.endpointsHint : t.providers.addHint}
      submitLabel={provider ? t.common.save : t.common.create}
      pending={pending}
      disabled={!valid}
      onSubmit={() => parsed.catalog && onSubmit({ label, enabled, endpoints: parsed.catalog })}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t.common.label}>
          {(id) => (
            <Input
              id={id}
              required
              autoFocus
              value={label}
              onChange={(event) => setLabel(event.target.value)}
            />
          )}
        </Field>
        <div className="flex items-end justify-between gap-2 pb-2">
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            {t.common.enabled}
          </label>
          {provider && (
            <Badge tone={provider.source === 'builtin' ? 'accent' : 'neutral'}>
              {t.providers.sources[provider.source]}
            </Badge>
          )}
        </div>
      </div>
      <EndpointsEditor text={text} onChange={setText} />
    </FormDialog>
  );
}
