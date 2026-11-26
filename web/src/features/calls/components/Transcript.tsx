import { useEffect } from 'react';
import type { ConsoleItemView, ConsoleTranscriptView } from '../../../api/generated/models';
import { useLocale } from '../../../i18n';
import { itemAnchorId, type ItemReveal } from '../reveal';
import { ItemView } from './ItemView';

function matchesReveal(item: ConsoleItemView, reveal?: ItemReveal) {
  return (
    reveal !== undefined &&
    item.call_id === reveal.callId &&
    item.section === reveal.section &&
    item.ordinal === reveal.ordinal
  );
}

export function Transcript({
  transcript,
  showRaw,
  reveal,
  valueFilter,
}: {
  transcript: ConsoleTranscriptView;
  showRaw: boolean;
  reveal?: ItemReveal;
  valueFilter?: string | null;
}) {
  const { t } = useLocale();
  useEffect(() => {
    if (!reveal) return;
    const target = document.getElementById(
      itemAnchorId({ call_id: reveal.callId, section: reveal.section, ordinal: reveal.ordinal }),
    );
    if (!target) return;
    target.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }, [reveal]);
  return (
    <div className="flex flex-col gap-3">
      {transcript.truncated && <p className="text-xs text-faint">{t.call.truncated}</p>}
      {transcript.turns.map((turn) => (
        <div key={turn.index} className="grid gap-2 sm:grid-cols-[7rem_minmax(0,1fr)]">
          <div className="flex items-start gap-2 sm:justify-end">
            <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-medium text-muted">
              {t.call.roles[turn.role as keyof typeof t.call.roles] ?? turn.role}
            </span>
          </div>
          <div className="flex min-w-0 flex-col gap-2 rounded-md border bg-surface px-3 py-2.5">
            {turn.items.map((item, index) => (
              <ItemView
                key={`${item.call_id}:${item.section}:${item.ordinal}:${index}`}
                item={item}
                showRaw={showRaw}
                valueFilter={valueFilter}
                highlighted={matchesReveal(item, reveal)}
              />
            ))}
          </div>
        </div>
      ))}
      {transcript.tools.length > 0 && (
        <details className="rounded-md border bg-surface px-3 py-2">
          <summary className="cursor-pointer text-xs font-medium text-muted">
            {t.call.tools} ({transcript.tools.length})
          </summary>
          <ul className="mt-2 flex flex-col gap-1.5">
            {transcript.tools.map((tool) => (
              <li key={tool.name} className="flex flex-wrap items-center gap-2 text-xs">
                <span className="font-mono font-medium">{tool.name}</span>
                <span className="text-faint">{tool.kind}</span>
                {tool.description && (
                  <span className="truncate text-muted">{tool.description}</span>
                )}
                {tool.boundary && <span className="text-muted">{tool.boundary}</span>}
                {tool.integrity && <span className="text-muted">{tool.integrity}</span>}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
