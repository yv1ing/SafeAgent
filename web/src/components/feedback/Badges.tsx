import type { ConsoleCallViewState } from '../../api/generated/models';
import { useLocale } from '../../i18n';
import { severityTone, stateTone } from '../../lib/format';
import { Badge, type Tone } from '../ui/badge';

export type Lane = 'main' | 'side' | 'auxiliary';

const laneTone: Record<Lane, Tone> = { main: 'accent', side: 'info', auxiliary: 'neutral' };

export type Outcome =
  | 'ok'
  | 'blocked'
  | 'rejected'
  | 'client_error'
  | 'upstream_error'
  | 'unsupported'
  | 'not_found'
  | 'rate_limited'
  | 'cancelled';

const outcomeTone: Record<Outcome, Tone> = {
  ok: 'success',
  blocked: 'warning',
  rejected: 'danger',
  client_error: 'danger',
  upstream_error: 'danger',
  unsupported: 'neutral',
  not_found: 'neutral',
  rate_limited: 'warning',
  cancelled: 'neutral',
};

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

export function LaneBadge({ lane }: { lane: string }) {
  const { t } = useLocale();
  if (!(lane in laneTone)) return <Badge>{lane}</Badge>;
  return <Badge tone={laneTone[lane as Lane]}>{t.lanes[lane as Lane]}</Badge>;
}

export function OutcomeBadge({ outcome }: { outcome?: string }) {
  const { t } = useLocale();
  if (!outcome) return null;
  if (!(outcome in outcomeTone)) return <Badge>{outcome}</Badge>;
  return <Badge tone={outcomeTone[outcome as Outcome]}>{t.outcomes[outcome as Outcome]}</Badge>;
}

export function EnabledBadge({ enabled, label }: { enabled?: boolean; label?: string }) {
  const { t } = useLocale();
  return enabled === true ? (
    <Badge tone="success">{label ?? t.common.enabled}</Badge>
  ) : (
    <Badge>{t.common.disabled}</Badge>
  );
}
