import type {
  ConsoleTrajectoryEdgeView,
  ConsoleTrajectoryNodeView,
  ConsoleTrajectoryView,
} from '../../api/generated/models';

export interface TrajectoryBranch {
  node: ConsoleTrajectoryNodeView;
  children: TrajectoryBranch[];
}

export interface TrajectorySlice {
  nodes: ConsoleTrajectoryNodeView[];
  edges: ConsoleTrajectoryEdgeView[];
}

export function callNodeId(callId: string) {
  return `call:${callId}`;
}

function isSideCall(node: ConsoleTrajectoryNodeView) {
  return node.kind === 'call' && node.call?.lane === 'side';
}

function sideCallIds(nodes: ConsoleTrajectoryNodeView[]) {
  const hidden = new Set<string>();
  for (const node of nodes) {
    if (isSideCall(node) && node.call) hidden.add(node.call.call_id);
  }
  return hidden;
}

// Side calls are background work: they share the conversation but not its main thread. Folding
// drops the call and everything anchored to it while keeping aggregate redaction values, whose
// occurrences may span folded and unfolded calls alike.
export function visibleTrajectory(
  trajectory: Pick<ConsoleTrajectoryView, 'nodes' | 'edges'>,
  foldSideCalls: boolean,
): TrajectorySlice {
  if (!foldSideCalls) return { nodes: trajectory.nodes, edges: trajectory.edges };
  const hidden = sideCallIds(trajectory.nodes);
  const nodes = trajectory.nodes.filter(
    (node) => node.kind === 'redaction' || !node.call_id || !hidden.has(node.call_id),
  );
  const visible = new Set(nodes.map((node) => node.id));
  return {
    nodes,
    edges: trajectory.edges.filter((edge) => visible.has(edge.from) && visible.has(edge.to)),
  };
}

export function findNode(nodes: ConsoleTrajectoryNodeView[], nodeId: string) {
  return nodes.find((node) => node.id === nodeId);
}

// Builds the containment forest. Order follows the DTO node order so two renders of the same
// projection always nest identically; a visited set makes a malformed cycle harmless.
export function buildForest(
  nodes: ConsoleTrajectoryNodeView[],
  edges: ConsoleTrajectoryEdgeView[],
): TrajectoryBranch[] {
  const byId = new Map(nodes.map((node) => [node.id, node]));
  const order = new Map(nodes.map((node, index) => [node.id, index]));
  const children = new Map<string, string[]>();
  const hasParent = new Set<string>();
  for (const edge of edges) {
    if (edge.kind !== 'contains' || !byId.has(edge.from) || !byId.has(edge.to)) continue;
    const list = children.get(edge.from) ?? [];
    list.push(edge.to);
    children.set(edge.from, list);
    hasParent.add(edge.to);
  }
  const sortChildren = (ids: string[]) =>
    [...ids].sort((left, right) => (order.get(left) ?? 0) - (order.get(right) ?? 0));
  const visited = new Set<string>();
  const build = (node: ConsoleTrajectoryNodeView): TrajectoryBranch => {
    visited.add(node.id);
    const nested = sortChildren(children.get(node.id) ?? [])
      .filter((id) => !visited.has(id))
      .map((id) => build(byId.get(id)!));
    return { node, children: nested };
  };
  const roots = nodes.filter((node) => !hasParent.has(node.id)).map(build);
  for (const node of nodes) {
    if (!visited.has(node.id)) roots.push(build(node));
  }
  return roots;
}

export function flattenForest(branches: TrajectoryBranch[]): ConsoleTrajectoryNodeView[] {
  const flat: ConsoleTrajectoryNodeView[] = [];
  const walk = (branch: TrajectoryBranch) => {
    flat.push(branch.node);
    for (const child of branch.children) walk(child);
  };
  for (const branch of branches) walk(branch);
  return flat;
}

export function countBranches(branch: TrajectoryBranch): number {
  return 1 + branch.children.reduce((total, child) => total + countBranches(child), 0);
}

export const replayIntervalMs = 1400;

// Replay index -1 means "before the first audit event"; total means "after the last event".
export function clampStepIndex(index: number, total: number) {
  return Math.min(Math.max(index, -1), total);
}

export function stepAfter(index: number, total: number) {
  return clampStepIndex(index + 1, total);
}

export function stepBefore(index: number) {
  return Math.max(index - 1, -1);
}

export function touchedNodeIds(
  trajectory: Pick<ConsoleTrajectoryView, 'steps' | 'nodes'>,
  stepIndex: number,
) {
  const visible = new Set(trajectory.nodes.map((node) => node.id));
  return new Set((trajectory.steps[stepIndex]?.node_ids ?? []).filter((id) => visible.has(id)));
}
