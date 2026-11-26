import { useMemo } from 'react';
import type {
  ConsoleTrajectoryEdgeView,
  ConsoleTrajectoryNodeView,
} from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { useLocale } from '../../../i18n';
import { cn } from '../../../lib/cn';
import { buildForest, flattenForest } from '../trajectory';
import { nodeKindLabel, summarizeNode } from './nodeView';

export function TrajectoryFlat({
  nodes,
  edges,
  selectedId,
  touched,
  onSelect,
}: {
  nodes: ConsoleTrajectoryNodeView[];
  edges: ConsoleTrajectoryEdgeView[];
  selectedId?: string;
  touched: Set<string>;
  onSelect: (id: string) => void;
}) {
  const { t } = useLocale();
  const ordered = useMemo(() => flattenForest(buildForest(nodes, edges)), [nodes, edges]);
  return (
    <ol className="divide-y">
      {ordered.map((node) => {
        const summary = summarizeNode(t, node);
        const selected = node.id === selectedId;
        const replayed = touched.has(node.id);
        return (
          <li key={node.id}>
            <button
              type="button"
              aria-current={selected ? 'true' : undefined}
              onClick={() => onSelect(node.id)}
              className={cn(
                'flex w-full flex-col gap-0.5 px-3 py-2 text-left hover:bg-sunken',
                selected && 'bg-accent-soft hover:bg-accent-soft',
                replayed && 'ring-1 ring-accent ring-inset',
              )}
            >
              <span className="flex flex-wrap items-center gap-1.5">
                <Badge className="font-mono text-[10px]">{nodeKindLabel(t, node.kind)}</Badge>
                <span className="min-w-0 flex-1 truncate text-sm font-medium">{summary.title}</span>
                {replayed && <Badge tone="accent">{t.trajectory.replayTouched}</Badge>}
              </span>
              {summary.subtitle && (
                <span className="truncate text-xs text-muted">{summary.subtitle}</span>
              )}
              {(summary.badges.length > 0 || summary.meta.length > 0) && (
                <span className="flex flex-wrap items-center gap-1.5">
                  {summary.badges.map((badge) => (
                    <Badge key={badge.label} tone={badge.tone}>
                      {badge.label}
                    </Badge>
                  ))}
                  {summary.meta.map((entry) => (
                    <span key={entry.label} className="text-[11px] text-faint">
                      {entry.label} {entry.value}
                    </span>
                  ))}
                </span>
              )}
            </button>
          </li>
        );
      })}
    </ol>
  );
}
