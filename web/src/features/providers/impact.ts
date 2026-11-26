import type { ConsoleImpactView } from '../../api/generated/models';
import type { ImpactReport } from '../../components/form/ImpactDialog';

export interface ReplacementOption {
  id: string;
  label: string;
}

export function projectNameMap(
  projects: readonly { id: string; name: string }[] | undefined,
): Map<string, string> {
  return new Map((projects ?? []).map((project) => [project.id, project.name]));
}

// The impact endpoint reports route and project ids only; the console resolves the project names it
// already knows and falls back to the id so an unknown project is still visible.
export function impactReport(
  impact: ConsoleImpactView,
  projectNames: Map<string, string>,
  replacements: readonly ReplacementOption[],
): ImpactReport {
  const projectName = (projectId: string) => projectNames.get(projectId) ?? projectId;
  return {
    routes: impact.routes.map((route) => `${route.route_name} (${projectName(route.project_id)})`),
    projects: impact.projects.map(projectName),
    liveTraffic: 0,
    replacements: replacements.map((replacement) => replacement.label),
  };
}

export function replacementOptions<T extends { id: string }>(
  items: readonly T[],
  excludeId: string,
  label: (item: T) => string,
): ReplacementOption[] {
  return items
    .filter((item) => item.id !== excludeId)
    .map((item) => ({ id: item.id, label: label(item) }));
}
