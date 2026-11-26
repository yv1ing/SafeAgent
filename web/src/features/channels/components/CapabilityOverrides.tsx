import type { ConsoleCapabilityOverrideView } from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { Card, CardHeader } from '../../../components/ui/card';
import { useLocale } from '../../../i18n';
import { shortId } from '../../../lib/format';

export function CapabilityOverrides({
  overrides,
}: {
  overrides: ConsoleCapabilityOverrideView[] | undefined;
}) {
  const { t } = useLocale();
  const rows = overrides ?? [];
  return (
    <Card>
      <CardHeader
        title={t.channels.capabilityOverrides}
        description={t.channels.capabilityOverridesHint}
      />
      {rows.length === 0 ? (
        <p className="px-4 py-6 text-sm text-muted">{t.channels.noCapabilityOverrides}</p>
      ) : (
        <ul className="divide-y">
          {rows.map((override) => (
            <li
              key={`${override.model_id}/${override.format}/${override.field}`}
              className="flex flex-wrap items-center gap-2 px-4 py-2.5 text-sm"
            >
              <Badge tone="warning">{override.field}</Badge>
              <span className="font-mono text-xs">{override.format}</span>
              <span className="font-mono text-xs text-faint">{shortId(override.model_id)}</span>
              {override.reason && (
                <span className="min-w-0 flex-1 truncate text-xs text-muted">
                  {override.reason}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
