import type { KeyboardEvent } from 'react';
import { splitBySpanSets, type ByteRange } from '../../lib/highlight';
import { cn } from '../../lib/cn';
import type { Tone } from '../ui/badge';

export interface HighlightSet {
  id: string;
  ranges: ByteRange[];
  tone?: Tone;
  tooltip?: (range: ByteRange, index: number) => string | undefined;
}

export interface HighlightSelection {
  setId: string;
  range: number;
}

const toneClass: Record<Tone, string> = {
  neutral: 'bg-sunken text-ink',
  success: 'bg-success-soft text-success',
  warning: 'bg-warning-soft text-warning',
  danger: 'bg-danger-soft text-danger',
  info: 'bg-info-soft text-info',
  accent: 'bg-accent-soft text-accent',
};

export function HighlightedText({
  text,
  sets,
  selected,
  onSelect,
  preserveWhitespace = true,
  className,
}: {
  text: string;
  sets: HighlightSet[];
  selected?: HighlightSelection;
  onSelect?: (selection: HighlightSelection) => void;
  preserveWhitespace?: boolean;
  className?: string;
}) {
  const segments = splitBySpanSets(
    text,
    sets.map((set) => set.ranges),
  );
  return (
    <span className={cn(preserveWhitespace && 'whitespace-pre-wrap', className)}>
      {segments.map((segment, index) => {
        const span = segment.spans.at(-1);
        if (!span) return segment.text;
        const set = sets[span.set];
        if (!set) return segment.text;
        const active = selected?.setId === set.id && selected.range === span.range;
        const tooltip = set.tooltip?.(set.ranges[span.range]!, span.range);
        const activate = () => onSelect?.({ setId: set.id, range: span.range });
        const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
          if (event.key !== 'Enter' && event.key !== ' ') return;
          event.preventDefault();
          activate();
        };
        return (
          <mark
            key={index}
            title={tooltip}
            role={onSelect ? 'button' : undefined}
            tabIndex={onSelect ? 0 : undefined}
            aria-pressed={onSelect ? active : undefined}
            onClick={onSelect ? activate : undefined}
            onKeyDown={onSelect ? onKeyDown : undefined}
            className={cn(
              'rounded-sm',
              toneClass[set.tone ?? 'warning'],
              onSelect && 'cursor-pointer',
              active && 'ring-2 ring-accent ring-offset-1 ring-offset-canvas outline-none',
            )}
          >
            {segment.text}
          </mark>
        );
      })}
    </span>
  );
}
