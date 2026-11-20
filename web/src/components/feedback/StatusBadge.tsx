import type { ConsoleCallViewState } from '../../api/generated/models';
import { useLocale } from '../../i18n';
import { severityTone, stateTone } from '../../lib/format';
import { Badge } from '../ui/badge';

export function StateBadge({ state }: { state: ConsoleCallViewState }) {
  const { t } = useLocale();
  return <Badge tone={stateTone(state)}>{t.states[state] ?? state}</Badge>;
}

export function SeverityBadge({ severity }: { severity: string }) {
  const { t } = useLocale();
  if (!(severity in t.severities)) return null;
  return (
    <Badge tone={severityTone(severity)}>
      {t.severities[severity as keyof typeof t.severities]}
    </Badge>
  );
}
