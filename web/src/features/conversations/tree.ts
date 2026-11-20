import type { ConsoleTreeNodeView } from '../../api/generated/models';

export interface TreeBranch {
  node: ConsoleTreeNodeView;
  children: TreeBranch[];
}

// A node whose parent is missing (outside the retained window) starts its own root.
export function buildTree(nodes: ConsoleTreeNodeView[]): TreeBranch[] {
  const branches = new Map(
    nodes.map((node) => [node.call.id, { node, children: [] as TreeBranch[] }]),
  );
  const roots: TreeBranch[] = [];
  for (const branch of branches.values()) {
    const parent = branches.get(branch.node.call.parent_call_id);
    if (parent && parent !== branch) parent.children.push(branch);
    else roots.push(branch);
  }
  return roots;
}
