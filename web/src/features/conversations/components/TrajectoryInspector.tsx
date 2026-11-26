import { X } from 'lucide-react';
import type {
  ConsoleRedactionIndexView,
  ConsoleTrajectoryNodeView,
} from '../../../api/generated/models';
import { useCurrentUser } from '../../../app/context';
import { HighlightedText } from '../../../components/data/HighlightedText';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { useLocale } from '../../../i18n';
import { formatDate, formatDuration, shortId } from '../../../lib/format';
import { CallDetail } from '../../calls/components/CallDetail';
import type { ItemReveal } from '../../calls/reveal';
import { RedactionOccurrences } from '../../redaction/components/RedactionPanel';
import type { RevealTarget } from '../../redaction/redaction';
import { callNodeId } from '../trajectory';
import { nodeKindLabel, summarizeNode } from './nodeView';

function Fields({ items }: { items: [string, string | number | undefined][] }) {
  return (
    <dl className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
      {items
        .filter(([, value]) => value !== undefined && value !== '')
        .map(([label, value]) => (
          <div key={label} className="min-w-0">
            <dt className="text-muted">{label}</dt>
            <dd className="truncate font-mono">{value}</dd>
          </div>
        ))}
    </dl>
  );
}

function Preview({ node }: { node: ConsoleTrajectoryNodeView }) {
  const { t } = useLocale();
  const user = useCurrentUser();
  const item = node.item;
  if (!item) return null;
  const spans = item.shown_redactions ?? [];
  return (
    <div className="flex flex-col gap-2">
      <pre className="rounded-md border bg-sunken p-2.5 font-sans text-sm break-words whitespace-pre-wrap">
        {user.administrator || spans.length === 0 ? (
          item.preview
        ) : (
          <HighlightedText
            text={item.preview}
            sets={[
              {
                id: `preview:${node.id}`,
                ranges: spans.map((span) => ({ start: span.start, end: span.end })),
                tone: 'warning',
                tooltip: (_range, index) => {
                  const span = spans[index];
                  if (!span) return undefined;
                  const action =
                    t.redaction.actions[span.action as keyof typeof t.redaction.actions] ??
                    span.action;
                  return `${span.label} · ${action}`;
                },
              },
            ]}
          />
        )}
      </pre>
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge>{t.classify.sections[item.locator.section] ?? item.locator.section}</Badge>
        <Badge className="font-mono">#{item.locator.ordinal}</Badge>
        <Badge tone={item.trust === 'untrusted' ? 'warning' : 'neutral'}>
          {t.call.origins[item.origin as keyof typeof t.call.origins] ?? item.origin}
        </Badge>
        <Badge>
          {t.classify.trusts[item.trust as keyof typeof t.classify.trusts] ?? item.trust}
        </Badge>
      </div>
      {item.locator.path && (
        <p className="truncate font-mono text-[11px] text-faint">{item.locator.path}</p>
      )}
    </div>
  );
}

export function TrajectoryInspector({
  node,
  redactions,
  projectId,
  reveal,
  onlyValueId,
  onSelect,
  onReveal,
  onOnlyValue,
  onClose,
}: {
  node?: ConsoleTrajectoryNodeView;
  redactions?: ConsoleRedactionIndexView;
  projectId: string;
  reveal?: ItemReveal;
  onlyValueId: string | null;
  onSelect: (nodeId: string) => void;
  onReveal: (target: RevealTarget) => void;
  onOnlyValue: (valueId: string | null) => void;
  onClose: () => void;
}) {
  const { t, locale } = useLocale();
  if (!node) {
    return <p className="p-4 text-sm text-muted">{t.trajectory.selectHint}</p>;
  }
  const summary = summarizeNode(t, node);
  const openCall = node.call_id ? callNodeId(node.call_id) : undefined;
  return (
    <div className="flex min-h-0 flex-col">
      <header className="flex items-start justify-between gap-2 border-b bg-surface px-4 py-2.5">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge className="font-mono text-[10px]">{nodeKindLabel(t, node.kind)}</Badge>
            <h2 className="truncate text-sm font-semibold">{summary.title}</h2>
          </div>
          {summary.subtitle && (
            <p className="mt-0.5 truncate text-xs text-muted">{summary.subtitle}</p>
          )}
        </div>
        <Button
          size="icon"
          variant="ghost"
          aria-label={t.trajectory.closeInspector}
          onClick={onClose}
        >
          <X aria-hidden />
        </Button>
      </header>
      <div className="min-h-0 flex-1">
        {node.kind === 'call' && node.call && (
          <CallDetail
            key={node.call.call_id}
            projectId={projectId}
            callId={node.call.call_id}
            reveal={reveal?.callId === node.call.call_id ? reveal : undefined}
            onlyValueId={onlyValueId}
          />
        )}
        {(node.kind === 'request' || node.kind === 'response') && (
          <div className="flex flex-col gap-3 p-4">
            <Preview node={node} />
            {openCall && (
              <Button size="sm" variant="secondary" onClick={() => onSelect(openCall)}>
                {t.trajectory.openCall}
              </Button>
            )}
          </div>
        )}
        {node.kind === 'tool_call' && node.tool_call && (
          <div className="flex flex-col gap-3 p-4">
            <pre className="rounded-md border bg-sunken p-2.5 font-mono text-xs break-words whitespace-pre-wrap">
              {node.tool_call.preview}
            </pre>
            <Fields
              items={[
                [t.call.name, node.tool_call.name],
                [t.call.kind, node.tool_call.kind],
                [t.call.sink, node.tool_call.sink],
                [t.call.target, node.tool_call.target.value || node.tool_call.target.host],
                [t.call.command, node.tool_call.target.command],
              ]}
            />
            {openCall && (
              <Button size="sm" variant="secondary" onClick={() => onSelect(openCall)}>
                {t.trajectory.openCall}
              </Button>
            )}
          </div>
        )}
        {node.kind === 'tool_result' && node.tool_result && (
          <div className="flex flex-col gap-3 p-4">
            <pre className="rounded-md border bg-sunken p-2.5 font-mono text-xs break-words whitespace-pre-wrap">
              {node.tool_result.preview}
            </pre>
            <Fields
              items={[
                [t.call.name, node.tool_result.name],
                [t.call.result, node.tool_result.is_error ? t.call.toolError : t.call.toolResult],
              ]}
            />
            {openCall && (
              <Button size="sm" variant="secondary" onClick={() => onSelect(openCall)}>
                {t.trajectory.openCall}
              </Button>
            )}
          </div>
        )}
        {node.kind === 'attempt' && node.attempt && (
          <div className="p-4">
            <Fields
              items={[
                [t.call.attempt, String(node.attempt.index)],
                [t.call.channel, node.attempt.channel_name],
                [t.call.upstreamModel, node.attempt.upstream_model],
                [t.call.format, node.attempt.format],
                [t.call.result, t.call.attemptResults[node.attempt.result]],
                [t.call.duration, formatDuration(node.attempt.duration_ms)],
                [t.call.errorCode, node.attempt.code],
                [t.call.errorClass, node.attempt.class],
                [t.call.recovered, node.attempt.recovered ? t.common.enabled : t.common.disabled],
              ]}
            />
          </div>
        )}
        {node.kind === 'decision' && node.decision && (
          <div className="flex flex-col gap-3 p-4">
            <Fields
              items={[
                [t.call.action, t.call.decisionActions[node.decision.action]],
                [t.call.phase, t.call.phases[node.decision.phase]],
                [t.call.policyVersion, String(node.decision.policy_version)],
                [t.call.rule, node.decision.rule_id],
                [t.call.code, node.decision.code],
                [t.table.severity, node.decision.severity],
                [t.common.createdAt, formatDate(node.decision.created_at, locale)],
              ]}
            />
            {node.decision.reason && <p className="text-sm text-muted">{node.decision.reason}</p>}
            {openCall && (
              <Button size="sm" variant="secondary" onClick={() => onSelect(openCall)}>
                {t.trajectory.openCall}
              </Button>
            )}
          </div>
        )}
        {node.kind === 'finding' && node.finding && (
          <div className="flex flex-col gap-3 p-4">
            <Fields
              items={[
                [t.call.findingCode, node.finding.code],
                [t.table.severity, t.severities[node.finding.severity]],
                [t.call.confidence, `${Math.round(node.finding.confidence * 100)}%`],
                [
                  t.call.origin,
                  t.call.origins[node.finding.origin as keyof typeof t.call.origins] ??
                    node.finding.origin,
                ],
                [
                  t.call.trust,
                  t.classify.trusts[node.finding.trust as keyof typeof t.classify.trusts] ??
                    node.finding.trust,
                ],
                [
                  t.call.sink,
                  t.call.sinks[node.finding.sink as keyof typeof t.call.sinks] ?? node.finding.sink,
                ],
                [t.call.relation, node.finding.relation],
                [t.call.sinkTool, node.finding.sink_tool_call_id],
              ]}
            />
            <h3 className="text-xs font-semibold text-muted">{t.trajectory.evidence}</h3>
            {node.finding.evidence.length === 0 ? (
              <p className="text-xs text-muted">{t.trajectory.noEvidence}</p>
            ) : (
              <ul className="flex flex-col gap-1.5">
                {node.finding.evidence.map((locator, index) => (
                  <li
                    key={`${locator.call_id}:${locator.section}:${locator.ordinal}:${index}`}
                    className="flex flex-wrap items-center gap-2 rounded-md border px-2.5 py-1.5 text-[11px]"
                  >
                    <span className="font-mono text-faint">{shortId(locator.call_id)}</span>
                    <span>
                      {t.classify.sections[locator.section] ?? locator.section} · #{locator.ordinal}
                    </span>
                    {locator.path && (
                      <span className="truncate font-mono text-muted">{locator.path}</span>
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      className="ml-auto"
                      onClick={() =>
                        onReveal({
                          call_id: locator.call_id,
                          section: locator.section,
                          ordinal: locator.ordinal,
                        })
                      }
                    >
                      {t.redaction.showInOriginal}
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
        {node.kind === 'redaction' && node.redaction && (
          <RedactionNode
            node={node}
            redactions={redactions}
            onlyValueId={onlyValueId}
            onOnlyValue={onOnlyValue}
            onReveal={onReveal}
          />
        )}
        {node.kind === 'turn' && node.turn && (
          <div className="flex flex-col gap-3 p-4">
            <Fields
              items={[
                [t.call.role, t.call.roles[node.turn.role] ?? node.turn.role],
                [t.call.turnIndex, String(node.turn.index)],
              ]}
            />
            {openCall && (
              <Button size="sm" variant="secondary" onClick={() => onSelect(openCall)}>
                {t.trajectory.openCall}
              </Button>
            )}
          </div>
        )}
        {node.kind === 'session' && node.session && (
          <div className="flex flex-col gap-3 p-4">
            <Fields
              items={[
                [t.calls.session, shortId(node.session.session_id)],
                [t.call.project, shortId(node.session.project_id)],
              ]}
            />
          </div>
        )}
      </div>
    </div>
  );
}

function RedactionNode({
  node,
  redactions,
  onlyValueId,
  onOnlyValue,
  onReveal,
}: {
  node: ConsoleTrajectoryNodeView;
  redactions?: ConsoleRedactionIndexView;
  onlyValueId: string | null;
  onOnlyValue: (valueId: string | null) => void;
  onReveal: (target: RevealTarget) => void;
}) {
  const { t } = useLocale();
  const value = redactions?.values.find((entry) => entry.value_id === node.redaction?.value_id);
  if (!value) return <p className="p-4 text-sm text-muted">{t.redaction.noValues}</p>;
  return (
    <div className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge tone={value.action === 'block' ? 'danger' : 'warning'}>
          {t.redaction.actions[value.action]}
        </Badge>
        {value.secret && <Badge tone="danger">{t.call.secret}</Badge>}
        {value.detector && (
          <span className="font-mono text-[11px] text-faint">{value.detector}</span>
        )}
      </div>
      <Fields
        items={[
          [t.redaction.occurrencesLabel, String(value.count)],
          [t.redaction.firstCallShort, shortId(value.first_call_id)],
          [t.redaction.lastCallShort, shortId(value.last_call_id)],
        ]}
      />
      <Button
        size="sm"
        variant={onlyValueId === value.value_id ? 'secondary' : 'ghost'}
        onClick={() => onOnlyValue(onlyValueId === value.value_id ? null : value.value_id)}
      >
        {onlyValueId === value.value_id ? t.redaction.allValues : t.redaction.onlyThisValue}
      </Button>
      <RedactionOccurrences
        value={value}
        onReveal={(occurrence, valueId) =>
          onReveal({
            call_id: occurrence.call_id,
            section: occurrence.section,
            ordinal: occurrence.ordinal,
            value_id: valueId,
          })
        }
      />
    </div>
  );
}
