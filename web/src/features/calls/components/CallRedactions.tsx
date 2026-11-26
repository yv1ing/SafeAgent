import { Eye, ShieldAlert } from 'lucide-react';
import type { ConsoleCallHitView } from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { interpolate, useLocale } from '../../../i18n';

export function CallRedactions({
  hits,
  onReveal,
}: {
  hits: ConsoleCallHitView[];
  onReveal: (location: ConsoleCallHitView['occurrences'][number], valueId: string) => void;
}) {
  const { t } = useLocale();
  if (hits.length === 0) {
    return <p className="text-sm text-muted">{t.call.noRedactions}</p>;
  }
  return (
    <div className="flex flex-col gap-2">
      <p className="flex items-center gap-1.5 text-xs text-muted">
        <ShieldAlert aria-hidden className="size-3.5" />
        {interpolate(t.call.redactionValuesCount, { count: hits.length })}
      </p>
      <ul className="flex flex-col gap-2">
        {hits.map((hit) => (
          <li key={hit.value_id} className="rounded-md border bg-surface">
            <details>
              <summary className="flex cursor-pointer flex-wrap items-center gap-2 px-3 py-2 text-xs">
                <span className="font-mono font-medium">{hit.label}</span>
                <Badge tone={hit.action === 'block' ? 'danger' : 'warning'}>
                  {t.redaction.actions[hit.action]}
                </Badge>
                {hit.secret && <Badge tone="danger">{t.call.secret}</Badge>}
                {hit.detector && (
                  <span className="font-mono text-[11px] text-faint">{hit.detector}</span>
                )}
                <span className="ml-auto text-muted">
                  {interpolate(t.call.occurrences, { count: hit.occurrences.length })}
                </span>
              </summary>
              <ul className="divide-y border-t">
                {hit.occurrences.map((occurrence, index) => (
                  <li
                    key={`${occurrence.section}:${occurrence.ordinal}:${index}`}
                    className="flex flex-wrap items-center gap-2 px-3 py-2 text-[11px]"
                  >
                    <span className="text-faint">
                      {t.classify.sections[
                        occurrence.section as keyof typeof t.classify.sections
                      ] ?? occurrence.section}{' '}
                      · #{occurrence.ordinal}
                    </span>
                    {occurrence.path && (
                      <span className="truncate font-mono text-muted">{occurrence.path}</span>
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      className="ml-auto"
                      onClick={() => onReveal(occurrence, hit.value_id)}
                    >
                      <Eye aria-hidden />
                      {t.call.showInTranscript}
                    </Button>
                  </li>
                ))}
              </ul>
            </details>
          </li>
        ))}
      </ul>
    </div>
  );
}
