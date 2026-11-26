import { Badge } from '../../../components/ui/badge';
import { useLocale } from '../../../i18n';

function labelOf<T extends { [key: string]: string }>(catalog: T, value: string) {
  return value in catalog ? catalog[value as keyof T] : value;
}

export function OperationBadge({ operation }: { operation: string }) {
  const { t } = useLocale();
  return (
    <Badge className="font-mono text-[11px]">{labelOf(t.classify.operations, operation)}</Badge>
  );
}

export function PurposeBadge({ purpose, source }: { purpose: string; source: string }) {
  const { t } = useLocale();
  const sourceLabel = labelOf(t.classify.purposeSources, source);
  return (
    <Badge title={`${t.call.purposeSource}: ${sourceLabel}`} className="text-[11px]">
      {labelOf(t.classify.purposes, purpose)}
    </Badge>
  );
}

export function ResolutionBadge({ resolution }: { resolution: string }) {
  const { t } = useLocale();
  if (resolution === 'exact' || resolution === '') return null;
  return (
    <Badge
      tone={resolution === 'fallback' ? 'warning' : 'neutral'}
      title={resolution === 'fallback' ? t.call.fallbackHint : undefined}
      className="text-[11px]"
    >
      {labelOf(t.classify.resolutions, resolution)}
    </Badge>
  );
}
