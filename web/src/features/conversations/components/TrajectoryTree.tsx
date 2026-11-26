import { ChevronRight } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import type {
  ConsoleTrajectoryEdgeView,
  ConsoleTrajectoryNodeView,
} from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { useLocale } from '../../../i18n';
import { cn } from '../../../lib/cn';
import { buildForest, countBranches, type TrajectoryBranch } from '../trajectory';
import { nodeKindLabel, summarizeNode } from './nodeView';

function parentIndex(branches: TrajectoryBranch[]) {
  const parents = new Map<string, string>();
  const walk = (branch: TrajectoryBranch) => {
    for (const child of branch.children) {
      parents.set(child.node.id, branch.node.id);
      walk(child);
    }
  };
  for (const branch of branches) walk(branch);
  return parents;
}

function ancestorsOf(id: string | undefined, parents: Map<string, string>) {
  const chain: string[] = [];
  let current = id;
  while (current) {
    const parent = parents.get(current);
    if (!parent || chain.includes(parent)) break;
    chain.push(parent);
    current = parent;
  }
  return chain;
}

function TreeRow({
  branch,
  depth,
  selectedId,
  touched,
  expanded,
  onToggle,
  onSelect,
}: {
  branch: TrajectoryBranch;
  depth: number;
  selectedId?: string;
  touched: Set<string>;
  expanded: Set<string>;
  onToggle: (id: string) => void;
  onSelect: (id: string) => void;
}) {
  const { t } = useLocale();
  const { node, children } = branch;
  const summary = summarizeNode(t, node);
  const hasChildren = children.length > 0;
  const open = expanded.has(node.id);
  const selected = node.id === selectedId;
  const replayed = touched.has(node.id);
  return (
    <li>
      <div className="flex items-start gap-1" style={{ paddingLeft: `${depth * 16}px` }}>
        {hasChildren ? (
          <button
            type="button"
            aria-expanded={open}
            aria-label={`${open ? t.trajectory.collapse : t.trajectory.expand}: ${summary.title}`}
            onClick={() => onToggle(node.id)}
            className="mt-1.5 rounded-sm p-0.5 text-muted hover:bg-sunken hover:text-ink"
          >
            <ChevronRight
              aria-hidden
              className={cn('size-3.5 transition-transform', open && 'rotate-90')}
            />
          </button>
        ) : (
          <span aria-hidden className="mt-1.5 inline-block size-4 shrink-0" />
        )}
        <button
          type="button"
          aria-current={selected ? 'true' : undefined}
          onClick={() => onSelect(node.id)}
          className={cn(
            'flex min-w-0 flex-1 flex-col gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-sunken',
            selected && 'bg-accent-soft hover:bg-accent-soft',
            replayed && 'ring-1 ring-accent',
          )}
        >
          <span className="flex flex-wrap items-center gap-1.5">
            <Badge className="font-mono text-[10px]">{nodeKindLabel(t, node.kind)}</Badge>
            <span className="min-w-0 flex-1 truncate text-sm font-medium">{summary.title}</span>
            {hasChildren && (
              <span className="num text-[11px] text-faint">{countBranches(branch) - 1}</span>
            )}
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
      </div>
      {hasChildren && open && (
        <ul className="mt-0.5 flex flex-col gap-0.5">
          {children.map((child) => (
            <TreeRow
              key={child.node.id}
              branch={child}
              depth={depth + 1}
              selectedId={selectedId}
              touched={touched}
              expanded={expanded}
              onToggle={onToggle}
              onSelect={onSelect}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

export function TrajectoryTree({
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
  const forest = useMemo(() => buildForest(nodes, edges), [nodes, edges]);
  const parents = useMemo(() => parentIndex(forest), [forest]);
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    const initial = new Set<string>(forest.map((branch) => branch.node.id));
    for (const id of ancestorsOf(selectedId, parents)) initial.add(id);
    return initial;
  });
  useEffect(() => {
    if (!selectedId) return;
    const chain = ancestorsOf(selectedId, parents);
    if (chain.length === 0) return;
    setExpanded((previous) => {
      if (chain.every((id) => previous.has(id))) return previous;
      return new Set([...previous, ...chain]);
    });
  }, [selectedId, parents]);
  const toggle = (id: string) =>
    setExpanded((previous) => {
      const next = new Set(previous);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  return (
    <ul className="flex flex-col gap-0.5 p-2">
      {forest.map((branch) => (
        <TreeRow
          key={branch.node.id}
          branch={branch}
          depth={0}
          selectedId={selectedId}
          touched={touched}
          expanded={expanded}
          onToggle={toggle}
          onSelect={onSelect}
        />
      ))}
    </ul>
  );
}
