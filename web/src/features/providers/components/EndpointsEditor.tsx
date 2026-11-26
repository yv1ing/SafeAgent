import { Braces } from 'lucide-react';
import { Callout } from '../../../components/feedback/Callout';
import { Field } from '../../../components/form/Field';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { Textarea } from '../../../components/ui/textarea';
import { useLocale } from '../../../i18n';
import { discoveryFormats, providerFormats, validateEndpointCatalog } from '../endpoints';

export function EndpointsEditor({
  text,
  disabled = false,
  onChange,
}: {
  text: string;
  disabled?: boolean;
  onChange: (text: string) => void;
}) {
  const { t } = useLocale();
  const parsed = validateEndpointCatalog(text);
  const formats = parsed.catalog ? providerFormats(parsed.catalog) : [];
  const discovery = parsed.catalog ? discoveryFormats(parsed.catalog) : [];
  const format = () => {
    if (!parsed.catalog) return;
    onChange(JSON.stringify(parsed.catalog, null, 2));
  };
  return (
    <div className="flex flex-col gap-2">
      <Field label={t.providers.endpoints} hint={t.providers.endpointsHint}>
        {(id) => (
          <Textarea
            id={id}
            aria-label={t.providers.endpoints}
            spellCheck={false}
            className="min-h-72 font-mono text-xs"
            value={text}
            disabled={disabled}
            onChange={(event) => onChange(event.target.value)}
          />
        )}
      </Field>
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" disabled={disabled || !parsed.catalog} onClick={format}>
          <Braces aria-hidden />
          {t.providers.formatJson}
        </Button>
        {formats.map((entry) => (
          <Badge key={entry} tone={discovery.includes(entry) ? 'accent' : 'neutral'}>
            {entry}
          </Badge>
        ))}
      </div>
      <p className="text-xs text-faint">{t.providers.formatsDerived}</p>
      {parsed.problem && (
        <Callout tone="danger">{t.providers.endpointProblems[parsed.problem]}</Callout>
      )}
    </div>
  );
}
