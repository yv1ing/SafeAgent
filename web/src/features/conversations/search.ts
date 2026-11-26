export interface TrajectorySearch {
  node?: string;
}

export function validateTrajectorySearch(search: Record<string, unknown>): TrajectorySearch {
  const node = typeof search.node === 'string' && search.node !== '' ? search.node : undefined;
  return { node };
}
