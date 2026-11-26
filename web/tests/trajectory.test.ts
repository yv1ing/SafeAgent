import assert from 'node:assert/strict';
import test from 'node:test';
import type {
  ConsoleTrajectoryEdgeView,
  ConsoleTrajectoryNodeView,
  ConsoleTrajectoryStepView,
} from '../src/api/generated/models/index.ts';
import {
  buildForest,
  clampStepIndex,
  flattenForest,
  stepAfter,
  stepBefore,
  touchedNodeIds,
  visibleTrajectory,
} from '../src/features/conversations/trajectory.ts';

const node = (
  id: string,
  kind: ConsoleTrajectoryNodeView['kind'],
  extra: Record<string, unknown> = {},
) => ({ id, kind, ...extra }) as unknown as ConsoleTrajectoryNodeView;

const edge = (from: string, to: string, kind: ConsoleTrajectoryEdgeView['kind'] = 'contains') =>
  ({ id: `${kind}:${from}:${to}`, from, to, kind, inferred: false }) as ConsoleTrajectoryEdgeView;

const step = (sequence: number, nodeIds: string[]) =>
  ({
    sequence,
    call_id: 'call-a',
    kind: 'request',
    category: 'request',
    label: 'call_admitted',
    status: 'ok',
    code: '',
    node_ids: nodeIds,
    at: '2026-01-01T00:00:00Z',
  }) as ConsoleTrajectoryStepView;

test('folding side calls removes the subtree and dangling edges', () => {
  const nodes = [
    node('session', 'session'),
    node('call:a', 'call', { call_id: 'a', call: { call_id: 'a', lane: 'main' } }),
    node('call:b', 'call', { call_id: 'b', call: { call_id: 'b', lane: 'side' } }),
    node('turn:a:0', 'turn', { call_id: 'a', turn: { index: 0, role: 'user', call_id: 'a' } }),
    node('turn:b:0', 'turn', { call_id: 'b', turn: { index: 0, role: 'user', call_id: 'b' } }),
    node('redaction:v1', 'redaction', {
      call_id: 'b',
      redaction: { value_id: 'v1', label: 'EMAIL', occurrences: 2 },
    }),
  ];
  const edges = [
    edge('session', 'call:a'),
    edge('session', 'call:b'),
    edge('call:a', 'turn:a:0'),
    edge('call:b', 'turn:b:0'),
    edge('turn:b:0', 'redaction:v1'),
  ];
  const folded = visibleTrajectory({ nodes, edges }, true);
  assert.deepEqual(
    folded.nodes.map((entry) => entry.id),
    ['session', 'call:a', 'turn:a:0', 'redaction:v1'],
  );
  assert.deepEqual(
    folded.edges.map((entry) => entry.id),
    ['contains:session:call:a', 'contains:call:a:turn:a:0'],
  );
  assert.deepEqual(
    visibleTrajectory({ nodes, edges }, false).nodes.map((entry) => entry.id),
    nodes.map((entry) => entry.id),
  );
});

test('containment forest nests in DTO order and survives cycles', () => {
  const nodes = [
    node('session', 'session'),
    node('call:a', 'call', { call_id: 'a' }),
    node('turn:a:1', 'turn', { call_id: 'a' }),
    node('turn:a:0', 'turn', { call_id: 'a' }),
    node('orphan', 'finding'),
  ];
  const edges = [edge('session', 'call:a'), edge('call:a', 'turn:a:1'), edge('call:a', 'turn:a:0')];
  const forest = buildForest(nodes, edges);
  assert.deepEqual(
    forest.map((branch) => branch.node.id),
    ['session', 'orphan'],
  );
  assert.deepEqual(
    forest[0]!.children.map((branch) => branch.node.id),
    ['call:a'],
  );
  assert.deepEqual(
    forest[0]!.children[0]!.children.map((branch) => branch.node.id),
    ['turn:a:1', 'turn:a:0'],
  );
  const cyclic = buildForest(
    [node('x', 'turn'), node('y', 'turn')],
    [edge('x', 'y'), edge('y', 'x')],
  );
  assert.deepEqual(flattenForest(cyclic).length, 2);
});

test('flattening walks the containment forest depth first', () => {
  const nodes = [node('a', 'session'), node('b', 'call'), node('c', 'turn')];
  const edges = [edge('a', 'b'), edge('b', 'c')];
  assert.deepEqual(
    flattenForest(buildForest(nodes, edges)).map((entry) => entry.id),
    ['a', 'b', 'c'],
  );
});

test('replay indexes clamp between before-first and after-last', () => {
  assert.equal(clampStepIndex(-5, 3), -1);
  assert.equal(clampStepIndex(9, 3), 3);
  assert.equal(stepAfter(-1, 3), 0);
  assert.equal(stepAfter(3, 3), 3);
  assert.equal(stepBefore(0), -1);
  assert.equal(stepBefore(-1), -1);
});

test('replay touches only nodes present in the projection', () => {
  const steps = [step(1, ['session', 'missing'])];
  const touched = touchedNodeIds({ steps, nodes: [node('session', 'session')] }, 0);
  assert.deepEqual([...touched], ['session']);
  assert.equal(touchedNodeIds({ steps, nodes: [] }, 5).size, 0);
});
