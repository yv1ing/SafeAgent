import assert from 'node:assert/strict';
import test from 'node:test';
import type { ConsoleImpactView } from '../src/api/generated/models/index.ts';
import {
  impactReport,
  projectNameMap,
  replacementOptions,
} from '../src/features/providers/impact.ts';

const impact: ConsoleImpactView = {
  routes: [
    {
      route_id: 'route-1',
      route_name: 'gpt-5',
      project_id: 'project-1',
      target_count: 2,
    },
    {
      route_id: 'route-2',
      route_name: 'claude',
      project_id: 'project-missing',
      target_count: 1,
    },
  ],
  projects: ['project-1', 'project-missing'],
};

test('an impact report names routes and projects, falling back to the id', () => {
  const report = impactReport(impact, projectNameMap([{ id: 'project-1', name: 'Assistants' }]), [
    { id: 'channel-2', label: 'backup' },
  ]);
  assert.deepEqual(report.routes, ['gpt-5 (Assistants)', 'claude (project-missing)']);
  assert.deepEqual(report.projects, ['Assistants', 'project-missing']);
  assert.deepEqual(report.replacements, ['backup']);
  assert.equal(report.liveTraffic, 0);
});

test('replacement candidates exclude the record being removed', () => {
  const items = [
    { id: 'channel-1', name: 'primary' },
    { id: 'channel-2', name: 'backup' },
  ];
  assert.deepEqual(
    replacementOptions(items, 'channel-1', (item) => item.name),
    [{ id: 'channel-2', label: 'backup' }],
  );
});
