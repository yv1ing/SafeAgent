import type { ConsoleTrajectoryNodeView } from '../../../api/generated/models';
import type { Tone } from '../../../components/ui/badge';
import { findingLabel, interpolate } from '../../../i18n';
import type { Messages } from '../../../i18n/messages';
import { formatDuration, formatTokens, shortId } from '../../../lib/format';

export interface NodeBadge {
  label: string;
  tone?: Tone;
}

export interface NodeSummary {
  title: string;
  subtitle?: string;
  badges: NodeBadge[];
  meta: { label: string; value: string }[];
}

const laneTone: Record<string, Tone> = { main: 'accent', side: 'info', auxiliary: 'neutral' };
const outcomeTone: Record<string, Tone> = {
  ok: 'success',
  blocked: 'warning',
  rejected: 'danger',
  client_error: 'danger',
  upstream_error: 'danger',
  unsupported: 'neutral',
  not_found: 'neutral',
  rate_limited: 'warning',
  cancelled: 'neutral',
};
const severityTone: Record<string, Tone> = { high: 'danger', medium: 'warning', info: 'info' };
const resultTone: Record<string, Tone> = {
  running: 'info',
  succeeded: 'success',
  failed: 'danger',
  cancelled: 'neutral',
};

function summary(title: string, extra: Partial<NodeSummary> = {}): NodeSummary {
  return { title, badges: [], meta: [], ...extra };
}

export function nodeKindLabel(t: Messages, kind: ConsoleTrajectoryNodeView['kind']) {
  return t.trajectory.nodeKinds[kind] ?? kind;
}

export function summarizeNode(t: Messages, node: ConsoleTrajectoryNodeView): NodeSummary {
  const kind = nodeKindLabel(t, node.kind);
  switch (node.kind) {
    case 'session': {
      const session = node.session;
      return summary(t.trajectory.sessionNode, {
        subtitle: session ? shortId(session.session_id) : undefined,
      });
    }
    case 'call': {
      const call = node.call;
      if (!call) return summary(kind);
      const badges: NodeBadge[] = [];
      if (call.lane !== 'main')
        badges.push({ label: t.lanes[call.lane], tone: laneTone[call.lane] });
      if (call.outcome !== 'ok')
        badges.push({ label: t.outcomes[call.outcome], tone: outcomeTone[call.outcome] });
      if (call.resolution === 'fallback')
        badges.push({ label: t.classify.resolutions.fallback, tone: 'warning' });
      if (call.finding_count > 0)
        badges.push({ label: String(call.finding_count), tone: 'danger' });
      if (call.redaction_values > 0)
        badges.push({ label: String(call.redaction_values), tone: 'warning' });
      return summary(call.model || call.upstream_model || shortId(call.call_id), {
        subtitle: `${t.classify.purposes[call.purpose] ?? call.purpose} · ${t.classify.operations[call.operation] ?? call.operation}`,
        badges,
        meta: [
          { label: t.call.duration, value: formatDuration(call.duration_ms) },
          {
            label: t.overview.tokens,
            value: `${formatTokens(call.input_tokens)} / ${formatTokens(call.output_tokens)}`,
          },
          { label: t.call.attempts, value: String(call.attempts) },
        ],
      });
    }
    case 'turn': {
      const turn = node.turn;
      if (!turn) return summary(kind);
      return summary(t.call.roles[turn.role] ?? turn.role, {
        subtitle: interpolate(t.trajectory.turnIndex, { index: turn.index }),
      });
    }
    case 'request':
    case 'response': {
      const item = node.item;
      if (!item) return summary(kind);
      const badges: NodeBadge[] = [];
      if (item.trust === 'untrusted')
        badges.push({ label: t.classify.trusts.untrusted, tone: 'warning' });
      return summary(t.classify.sections[item.locator.section] ?? kind, {
        subtitle: item.preview,
        badges,
      });
    }
    case 'tool_call': {
      const tool = node.tool_call;
      if (!tool) return summary(kind);
      const target =
        tool.target.value || tool.target.command || tool.target.host || tool.target.kind;
      return summary(tool.name || kind, {
        subtitle: target || tool.preview,
        badges: tool.sink !== 'none' ? [{ label: tool.sink, tone: 'warning' }] : [],
      });
    }
    case 'tool_result': {
      const result = node.tool_result;
      if (!result) return summary(kind);
      return summary(result.name || kind, {
        subtitle: result.preview,
        badges: result.is_error ? [{ label: t.call.toolError, tone: 'danger' }] : [],
      });
    }
    case 'attempt': {
      const attempt = node.attempt;
      if (!attempt) return summary(kind);
      const badges: NodeBadge[] = [
        { label: t.call.attemptResults[attempt.result], tone: resultTone[attempt.result] },
      ];
      if (attempt.recovered) badges.push({ label: t.call.recovered, tone: 'warning' });
      return summary(interpolate(t.call.attemptIndex, { index: attempt.index }), {
        subtitle: interpolate(t.call.attemptLabel, {
          channel: attempt.channel_name,
          model: attempt.upstream_model,
        }),
        badges,
        meta: [{ label: t.call.duration, value: formatDuration(attempt.duration_ms) }],
      });
    }
    case 'decision': {
      const decision = node.decision;
      if (!decision) return summary(kind);
      return summary(t.call.decisionActions[decision.action] ?? decision.action, {
        subtitle: t.call.phases[decision.phase] ?? decision.phase,
        badges: decision.blocked ? [{ label: t.states.blocked, tone: 'danger' }] : [],
        meta: decision.rule_id ? [{ label: t.call.rule, value: decision.rule_id }] : [],
      });
    }
    case 'finding': {
      const finding = node.finding;
      if (!finding) return summary(kind);
      return summary(findingLabel(t, finding.code), {
        subtitle: `${t.call.origins[finding.origin as keyof typeof t.call.origins] ?? finding.origin} → ${t.call.sinks[finding.sink as keyof typeof t.call.sinks] ?? finding.sink}`,
        badges: [{ label: t.severities[finding.severity], tone: severityTone[finding.severity] }],
        meta: [{ label: t.call.confidence, value: `${Math.round(finding.confidence * 100)}%` }],
      });
    }
    case 'redaction': {
      const value = node.redaction;
      if (!value) return summary(kind);
      return summary(value.label, {
        subtitle: interpolate(t.redaction.occurrences, { count: value.occurrences }),
        badges: [
          {
            label: t.redaction.actions[value.action],
            tone: value.action === 'block' ? 'danger' : 'warning',
          },
          ...(value.secret ? [{ label: t.call.secret, tone: 'danger' as Tone }] : []),
        ],
      });
    }
  }
}
