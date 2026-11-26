import type { ConsoleItemView } from '../../../api/generated/models';
import { HighlightedText, type HighlightSet } from '../../../components/data/HighlightedText';
import { Badge } from '../../../components/ui/badge';
import { interpolate, useLocale } from '../../../i18n';
import { cn } from '../../../lib/cn';
import { itemAnchorId } from '../reveal';

function Target({ target }: { target: NonNullable<ConsoleItemView['tool_call']>['target'] }) {
  const label = target.value || target.command || target.host || target.kind;
  return <span className="font-mono text-xs text-muted">{label}</span>;
}

function highlightSets(
  item: ConsoleItemView,
  actionLabel: (action: string) => string,
  valueFilter?: string | null,
): HighlightSet[] {
  const spans = (item.shown_redactions ?? []).filter(
    (span) => !valueFilter || span.value_id === valueFilter,
  );
  if (spans.length === 0) return [];
  return [
    {
      id: `redactions:${item.call_id}:${item.section}:${item.ordinal}`,
      ranges: spans.map((span) => ({ start: span.start, end: span.end })),
      tone: 'warning',
      tooltip: (_range, index) => {
        const span = spans[index];
        return span ? `${span.label} · ${actionLabel(span.action)}` : undefined;
      },
    },
  ];
}

function Content({
  item,
  showRaw,
  valueFilter,
}: {
  item: ConsoleItemView;
  showRaw: boolean;
  valueFilter?: string | null;
}) {
  const { t } = useLocale();
  // shown_redactions offsets always address the shown representation, so the raw view renders
  // plain text and the shown view highlights only the spans that belong to the shown text.
  const actionLabel = (action: string) =>
    t.redaction.actions[action as keyof typeof t.redaction.actions] ?? action;
  const sets = showRaw ? [] : highlightSets(item, actionLabel, valueFilter);
  const argumentsText = showRaw
    ? item.tool_call?.arguments
    : (item.tool_call?.shown_arguments ?? item.tool_call?.arguments);
  if (argumentsText) {
    return (
      <pre className="mt-1.5 overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs text-muted">
        {showRaw ? argumentsText : <HighlightedText text={argumentsText} sets={sets} />}
      </pre>
    );
  }
  const text = showRaw ? item.text : (item.shown_text ?? item.text);
  return (
    <pre
      className={cn(
        'whitespace-pre-wrap break-words font-sans text-sm',
        item.kind === 'reasoning' && 'text-muted italic',
        item.kind === 'refusal' && 'text-danger',
      )}
    >
      {showRaw ? text : <HighlightedText text={text} sets={sets} />}
    </pre>
  );
}

export function ItemView({
  item,
  showRaw,
  valueFilter,
  highlighted = false,
}: {
  item: ConsoleItemView;
  showRaw: boolean;
  valueFilter?: string | null;
  highlighted?: boolean;
}) {
  const marker = highlighted ? ' ring-2 ring-accent ring-offset-1 ring-offset-canvas' : '';
  const { t } = useLocale();
  const anchor = itemAnchorId(item);
  if (item.tool_call) {
    return (
      <div
        id={anchor}
        className={`rounded-md border border-warning/40 bg-warning-soft/40 px-2.5 py-2${marker}`}
      >
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone="warning">
            {interpolate(t.call.toolCall, { name: item.tool_call.name })}
          </Badge>
          <Target target={item.tool_call.target} />
          {item.tool_call.capabilities.map((capability) => (
            <Badge key={capability} tone="neutral">
              {capability}
            </Badge>
          ))}
          {item.tool_call.sink && item.tool_call.sink !== 'none' && (
            <Badge tone="neutral" className="font-mono text-[10px]">
              {item.tool_call.sink}
            </Badge>
          )}
        </div>
        {(item.tool_call.arguments || item.tool_call.shown_arguments) && (
          <Content item={item} showRaw={showRaw} valueFilter={valueFilter} />
        )}
      </div>
    );
  }
  if (item.tool_result) {
    return (
      <div
        id={anchor}
        className={cn(
          'rounded-md border px-2.5 py-2',
          item.tool_result.is_error ? 'border-danger/40 bg-danger-soft/40' : 'bg-sunken/60',
          marker,
        )}
      >
        <div className="mb-1 flex items-center gap-2 text-xs">
          <Badge tone={item.tool_result.is_error ? 'danger' : 'success'}>
            {item.tool_result.is_error ? t.call.toolError : t.call.toolResult}
          </Badge>
          <span className="font-mono text-muted">{item.tool_result.call_id}</span>
        </div>
        <div className="flex flex-col gap-1.5">
          {item.tool_result.content.map((child, index) => (
            <ItemView
              key={`${child.call_id}:${child.section}:${child.ordinal}:${index}`}
              item={child}
              showRaw={showRaw}
              valueFilter={valueFilter}
              highlighted={highlighted}
            />
          ))}
        </div>
      </div>
    );
  }
  if (item.media) {
    return (
      <p id={anchor} className={cn('text-xs text-muted', marker)}>
        {interpolate(t.call.media, { kind: item.media.kind })} · {item.media.mime_type}{' '}
        {item.media.filename}
      </p>
    );
  }
  if (!item.text) {
    return (
      <p id={anchor} className={cn('text-xs text-faint', marker)}>
        {item.kind === 'native' ? t.call.native : `[${item.kind}]`}
      </p>
    );
  }
  return (
    <div id={anchor} className={cn('flex flex-col gap-1', marker)}>
      {item.kind === 'reasoning' && (
        <span className="text-[11px] font-medium text-muted">{t.call.reasoning}</span>
      )}
      {item.kind === 'refusal' && (
        <span className="text-[11px] font-medium text-danger">{t.call.refusal}</span>
      )}
      <Content item={item} showRaw={showRaw} valueFilter={valueFilter} />
    </div>
  );
}
