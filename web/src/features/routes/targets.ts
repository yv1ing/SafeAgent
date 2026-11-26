import type {
  ConsoleChannelOptionView,
  ConsoleRouteTargetInput,
  ConsoleRouteTargetView,
  ConsoleRouteView,
} from '../../api/generated/models';

export type RouteTargetField = 'channel_id' | 'model_id';

export interface DraftTarget {
  channel_id: string;
  model_id: string;
  format?: string;
  priority: number;
  weight: number;
  max_output_tokens?: number;
  enabled?: boolean | null;
}

export type TargetProblem =
  | 'empty'
  | 'incomplete'
  | 'duplicate'
  | 'provider_mismatch'
  | 'kind_mismatch'
  | 'format_mismatch';

export function manualAliases(route: ConsoleRouteView): string[] {
  return route.aliases.filter((alias) => alias.source === 'manual').map((alias) => alias.alias);
}

export function targetInput(target: ConsoleRouteTargetView): ConsoleRouteTargetInput {
  return {
    channel_id: target.channel_id,
    model_id: target.model_id,
    format: target.format || undefined,
    priority: target.priority,
    weight: target.weight,
    max_output_tokens: target.max_output_tokens || undefined,
    enabled: target.enabled ?? true,
  };
}

export function routeTargetsInput(route: ConsoleRouteView): ConsoleRouteTargetInput[] {
  return route.targets.map(targetInput);
}

function targetKey(target: { channel_id: string; model_id: string; format?: string }) {
  return `${target.channel_id}/${target.model_id}/${target.format ?? ''}`;
}

// Rewiring replaces one channel or catalog model inside every target and drops a target that would
// become an exact duplicate of another, which the API rejects as invalid targets.
export function rewireTargets(
  targets: readonly ConsoleRouteTargetView[],
  field: RouteTargetField,
  from: string,
  to: string,
): ConsoleRouteTargetInput[] {
  const seen = new Set<string>();
  const rewired: ConsoleRouteTargetInput[] = [];
  for (const target of targets) {
    const next =
      target[field] === from
        ? targetInput({ ...target, [field]: to } as ConsoleRouteTargetView)
        : targetInput(target);
    const key = targetKey(next);
    if (seen.has(key)) continue;
    seen.add(key);
    rewired.push(next);
  }
  return rewired;
}

export function targetChannel(
  target: Pick<DraftTarget, 'channel_id'>,
  channels: readonly ConsoleChannelOptionView[],
) {
  return channels.find((channel) => channel.id === target.channel_id);
}

export function targetModel(
  target: Pick<DraftTarget, 'channel_id' | 'model_id'>,
  channels: readonly ConsoleChannelOptionView[],
) {
  return targetChannel(target, channels)?.models?.find((model) => model.id === target.model_id);
}

export function targetKind(
  target: Pick<DraftTarget, 'channel_id' | 'model_id'>,
  channels: readonly ConsoleChannelOptionView[],
): string | undefined {
  return targetModel(target, channels)?.kind;
}

// One route serves one model kind; every target must name a channel and a catalog model of the same
// provider with at least one shared format. The gateway enforces this too; checking here keeps the
// editor from submitting a request that can only fail.
export function validateTargets(
  targets: readonly DraftTarget[],
  channels: readonly ConsoleChannelOptionView[],
): TargetProblem | null {
  if (targets.length === 0) return 'empty';
  let kind: string | undefined;
  const seen = new Set<string>();
  for (const target of targets) {
    if (!target.channel_id || !target.model_id) return 'incomplete';
    const channel = targetChannel(target, channels);
    const model = targetModel(target, channels);
    if (!channel || !model) return 'provider_mismatch';
    if (target.format && !(model.formats ?? []).includes(target.format)) return 'format_mismatch';
    if (kind === undefined) {
      kind = model.kind;
    } else if (kind !== model.kind) {
      return 'kind_mismatch';
    }
    const key = targetKey(target);
    if (seen.has(key)) return 'duplicate';
    seen.add(key);
  }
  return null;
}

export function eligibleModels(
  channel: ConsoleChannelOptionView | undefined,
  kind: string | undefined,
) {
  const models = channel?.models ?? [];
  return kind ? models.filter((model) => model.kind === kind) : models;
}

export function firstTarget(
  channels: readonly ConsoleChannelOptionView[],
  kind?: string,
): DraftTarget | null {
  for (const channel of channels) {
    const model = eligibleModels(channel, kind)[0];
    if (model) {
      return {
        channel_id: channel.id,
        model_id: model.id,
        priority: 0,
        weight: 1,
        enabled: true,
      };
    }
  }
  return null;
}
