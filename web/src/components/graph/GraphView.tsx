import {
  Background,
  BackgroundVariant,
  Handle,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type Node,
  type NodeProps,
  type NodeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { graphlib, layout } from 'dagre';
import { Maximize2, Minus, Network, Plus } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, type CSSProperties } from 'react';
import { useLocale } from '../../i18n';
import { cn } from '../../lib/cn';
import { Badge, type Tone } from '../ui/badge';
import { Button } from '../ui/button';
import { Empty } from '../ui/empty';

export interface GraphNodeBadge {
  label: string;
  tone?: Tone;
}

export interface GraphNodeMeta {
  label: string;
  value: string;
}

export interface GraphNode {
  id: string;
  title: string;
  subtitle?: string;
  badges?: GraphNodeBadge[];
  meta?: GraphNodeMeta[];
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  label?: string;
}

export type GraphDirection = 'TB' | 'BT' | 'LR' | 'RL';

type GraphNodeData = {
  node: GraphNode;
  selected: boolean;
  direction: GraphDirection;
  onSelect: (id: string) => void;
};

const nodeWidth = 224;
const nodePaddingY = 16;
const titleHeight = 20;
const subtitleHeight = 16;
const badgeRowHeight = 22;
const metaRowHeight = 18;

function estimateNodeHeight(node: GraphNode) {
  let height = nodePaddingY + titleHeight;
  if (node.subtitle) height += subtitleHeight;
  if (node.badges?.length) height += badgeRowHeight;
  height += (node.meta?.length ?? 0) * metaRowHeight;
  return Math.max(height, 52);
}

function handlePositions(direction: GraphDirection) {
  switch (direction) {
    case 'TB':
      return { target: Position.Top, source: Position.Bottom };
    case 'BT':
      return { target: Position.Bottom, source: Position.Top };
    case 'LR':
      return { target: Position.Left, source: Position.Right };
    case 'RL':
      return { target: Position.Right, source: Position.Left };
  }
}

function GraphCardNode({ data }: NodeProps<Node<GraphNodeData>>) {
  const { node, selected, direction, onSelect } = data;
  const position = handlePositions(direction);
  return (
    <div
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key !== 'Enter' && event.key !== ' ') return;
        event.preventDefault();
        onSelect(node.id);
      }}
      className={cn(
        'w-56 rounded-lg border bg-surface px-3 py-2 text-left shadow-card transition-colors',
        'hover:border-line-strong focus-visible:border-accent',
        selected && 'border-accent ring-2 ring-accent/40',
      )}
    >
      <Handle
        type="target"
        position={position.target}
        className="size-1.5! border-0! bg-line-strong!"
      />
      <p className="truncate text-sm font-medium">{node.title}</p>
      {node.subtitle && <p className="truncate text-xs text-muted">{node.subtitle}</p>}
      {node.badges && node.badges.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-1">
          {node.badges.map((badge) => (
            <Badge key={badge.label} tone={badge.tone}>
              {badge.label}
            </Badge>
          ))}
        </div>
      )}
      {node.meta && node.meta.length > 0 && (
        <dl className="mt-1.5 flex flex-col gap-0.5">
          {node.meta.map((row) => (
            <div key={row.label} className="flex justify-between gap-2 text-[11px]">
              <dt className="shrink-0 text-muted">{row.label}</dt>
              <dd className="truncate font-mono">{row.value}</dd>
            </div>
          ))}
        </dl>
      )}
      <Handle
        type="source"
        position={position.source}
        className="size-1.5! border-0! bg-line-strong!"
      />
    </div>
  );
}

const nodeTypes = { card: GraphCardNode } as NodeTypes;

function layoutGraph(nodes: GraphNode[], edges: GraphEdge[], direction: GraphDirection) {
  const graph = new graphlib.Graph();
  graph.setGraph({
    rankdir: direction,
    nodesep: 32,
    ranksep: 64,
    marginx: 24,
    marginy: 24,
  });
  graph.setDefaultEdgeLabel(() => ({}));
  const known = new Set(nodes.map((node) => node.id));
  for (const node of nodes) {
    graph.setNode(node.id, { width: nodeWidth, height: estimateNodeHeight(node) });
  }
  for (const edge of edges) {
    if (known.has(edge.source) && known.has(edge.target)) {
      graph.setEdge(edge.source, edge.target);
    }
  }
  layout(graph);
  return nodes.map((node) => {
    const point = graph.node(node.id);
    return {
      id: node.id,
      type: 'card',
      ariaLabel: node.title,
      position: { x: point.x - point.width / 2, y: point.y - point.height / 2 },
      data: { node, selected: false, direction, onSelect: () => undefined },
    } satisfies Node<GraphNodeData>;
  });
}

function GraphToolbar() {
  const { t } = useLocale();
  const flow = useReactFlow();
  return (
    <div className="absolute top-2 right-2 z-10 flex flex-col gap-0.5 rounded-md border bg-raised p-1 shadow-card">
      <Button
        size="icon"
        variant="ghost"
        aria-label={t.graph.zoomIn}
        onClick={() => void flow.zoomIn()}
      >
        <Plus aria-hidden />
      </Button>
      <Button
        size="icon"
        variant="ghost"
        aria-label={t.graph.zoomOut}
        onClick={() => void flow.zoomOut()}
      >
        <Minus aria-hidden />
      </Button>
      <Button
        size="icon"
        variant="ghost"
        aria-label={t.graph.fitView}
        onClick={() => void flow.fitView({ padding: 0.2 })}
      >
        <Maximize2 aria-hidden />
      </Button>
    </div>
  );
}

function FitOnLayout({ layoutToken }: { layoutToken: object }) {
  const { fitView } = useReactFlow();
  useEffect(() => {
    const frame = requestAnimationFrame(() => void fitView({ padding: 0.2 }));
    return () => cancelAnimationFrame(frame);
  }, [fitView, layoutToken]);
  return null;
}

function GraphCanvas({
  nodes,
  edges,
  direction,
  onSelect,
  selectedId,
  height,
}: {
  nodes: GraphNode[];
  edges: GraphEdge[];
  direction: GraphDirection;
  onSelect?: (id: string) => void;
  selectedId?: string;
  height: number;
}) {
  const { t } = useLocale();
  const selectRef = useRef(onSelect);
  useEffect(() => {
    selectRef.current = onSelect;
  }, [onSelect]);
  const handleSelect = useCallback((id: string) => selectRef.current?.(id), []);
  // Dagre is the expensive step, so compare graph content rather than array identity: callers that
  // rebuild nodes inline on every render still get one layout per distinct graph.
  const graphKey = useMemo(
    () => JSON.stringify({ nodes, edges, direction }),
    [nodes, edges, direction],
  );
  const laidOut = useMemo(() => layoutGraph(nodes, edges, direction), [graphKey]);
  const flowNodes = useMemo(
    () =>
      laidOut.map((node) => ({
        ...node,
        selected: node.id === selectedId,
        data: { ...node.data, selected: node.id === selectedId, onSelect: handleSelect },
      })),
    [laidOut, selectedId, handleSelect],
  );
  const flowEdges = useMemo(
    () =>
      edges.map(
        (edge) =>
          ({
            id: edge.id,
            source: edge.source,
            target: edge.target,
            type: 'smoothstep',
            label: edge.label,
            labelStyle: { fill: 'var(--muted)', fontSize: 11 },
            labelBgStyle: { fill: 'var(--surface)' },
            style: { stroke: 'var(--line-strong)', strokeWidth: 1.5 },
          }) satisfies Edge,
      ),
    [edges],
  );
  const theme = {
    height,
    '--xy-background-color': 'transparent',
    '--xy-edge-label-background-color': 'var(--surface)',
    '--xy-attribution-background-color': 'transparent',
    '--xy-selection-background-color': 'var(--accent-soft)',
    '--xy-selection-border': '1px dashed var(--accent)',
  } as CSSProperties;
  return (
    <div
      role="group"
      aria-label={t.graph.view}
      className="overflow-hidden rounded-lg border bg-surface"
      style={theme}
    >
      <ReactFlow
        nodes={flowNodes}
        edges={flowEdges}
        nodeTypes={nodeTypes}
        fitView
        minZoom={0.2}
        maxZoom={1.6}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        nodesFocusable={false}
        onlyRenderVisibleElements={nodes.length > 80}
        onNodeClick={(_, node) => handleSelect(node.id)}
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1.2} color="var(--line)" />
        <GraphToolbar />
        <FitOnLayout layoutToken={laidOut} />
      </ReactFlow>
    </div>
  );
}

export function GraphView({
  nodes,
  edges,
  direction = 'TB',
  onSelect,
  selectedId,
  height = 520,
}: {
  nodes: GraphNode[];
  edges: GraphEdge[];
  direction?: GraphDirection;
  onSelect?: (id: string) => void;
  selectedId?: string;
  height?: number;
}) {
  const { t } = useLocale();
  if (nodes.length === 0) {
    return (
      <div
        className="flex items-center justify-center overflow-hidden rounded-lg border bg-surface"
        style={{ height }}
      >
        <Empty icon={Network} title={t.graph.empty} />
      </div>
    );
  }
  return (
    <ReactFlowProvider>
      <GraphCanvas
        nodes={nodes}
        edges={edges}
        direction={direction}
        onSelect={onSelect}
        selectedId={selectedId}
        height={height}
      />
    </ReactFlowProvider>
  );
}
