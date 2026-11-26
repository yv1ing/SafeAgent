import { useMemo } from 'react';
import type {
  ConsoleTrajectoryEdgeView,
  ConsoleTrajectoryNodeView,
} from '../../../api/generated/models';
import { GraphView, type GraphEdge, type GraphNode } from '../../../components/graph/GraphView';
import { useLocale } from '../../../i18n';
import { useMediaQuery } from '../../../lib/media';
import { summarizeNode } from './nodeView';

const flowKinds = new Set<ConsoleTrajectoryNodeView['kind']>([
  'session',
  'call',
  'turn',
  'tool_call',
  'tool_result',
  'attempt',
  'decision',
  'finding',
  'redaction',
]);

export function TrajectoryFlow({
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
  onSelect: (nodeId: string) => void;
}) {
  const { t } = useLocale();
  const wide = useMediaQuery('(min-width: 80rem)');
  const graph = useMemo(() => {
    const byId = new Map(nodes.map((node) => [node.id, node]));
    const included = new Set(
      nodes.filter((node) => flowKinds.has(node.kind)).map((node) => node.id),
    );
    // Keep the request/response items that findings and redactions anchor to so those nodes stay
    // connected instead of floating alone in the graph.
    for (const edge of edges) {
      if (edge.kind !== 'contains') continue;
      const from = byId.get(edge.from);
      const to = byId.get(edge.to);
      if (!from || !to) continue;
      if (
        (from.kind === 'request' || from.kind === 'response') &&
        (to.kind === 'finding' || to.kind === 'redaction')
      ) {
        included.add(from.id);
      }
    }
    const graphNodes: GraphNode[] = nodes
      .filter((node) => included.has(node.id))
      .map((node) => {
        const summary = summarizeNode(t, node);
        return {
          id: node.id,
          title: summary.title,
          subtitle: summary.subtitle,
          badges: [
            ...summary.badges,
            ...(touched.has(node.id)
              ? [{ label: t.trajectory.replayTouched, tone: 'accent' as const }]
              : []),
          ],
          meta: summary.meta,
        };
      });
    const graphEdges: GraphEdge[] = edges
      .filter((edge) => included.has(edge.from) && included.has(edge.to))
      .map((edge) => ({
        // Backend edge ids contain NUL separators, which are unsafe as DOM identifiers.
        id: `${edge.kind}:${edge.from}=>${edge.to}`,
        source: edge.from,
        target: edge.to,
        label:
          edge.kind === 'contains'
            ? undefined
            : `${t.trajectory.edgeKinds[edge.kind]}${edge.inferred ? ` · ${t.trajectory.inferred}` : ''}`,
      }));
    return { nodes: graphNodes, edges: graphEdges };
  }, [nodes, edges, t, touched]);
  return (
    <GraphView
      nodes={graph.nodes}
      edges={graph.edges}
      direction="LR"
      selectedId={selectedId}
      onSelect={onSelect}
      height={wide ? 560 : 420}
    />
  );
}
