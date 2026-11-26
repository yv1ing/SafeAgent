import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useDiffAnalyses } from '../../../api/generated/analyses/analyses';
import type { ReactNode } from 'react';
import { toast } from 'sonner';
import { getGetCallQueryKey, useReanalyzeCall } from '../../../api/generated/calls/calls';
import type {
  ConsoleAnalysisView,
  ConsoleCallDetailView,
  ConsoleFindingView,
} from '../../../api/generated/models';
import { JsonView } from '../../../components/feedback/JsonView';
import { SeverityBadge } from '../../../components/feedback/Badges';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { errorMessage, findingLabel, interpolate, useLocale } from '../../../i18n';
import { formatDate } from '../../../lib/format';

export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-semibold tracking-wide text-muted uppercase">{title}</h3>
      {children}
    </section>
  );
}

function Axes({ finding }: { finding: ConsoleFindingView }) {
  const parts = [finding.axes.source, finding.axes.mode, finding.axes.harm].filter(Boolean);
  if (parts.length === 0) return null;
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {parts.map((part) => (
        <Badge key={part} tone="neutral" className="font-mono text-[10px]">
          {part}
        </Badge>
      ))}
    </div>
  );
}

function Findings({ findings }: { findings: ConsoleFindingView[] }) {
  const { t } = useLocale();
  if (findings.length === 0) {
    return <p className="text-sm text-muted">{t.call.noFindings}</p>;
  }
  return (
    <ul className="flex flex-col gap-2">
      {findings.map((finding) => (
        <li key={finding.fingerprint} className="rounded-md border bg-surface px-3 py-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <SeverityBadge severity={finding.severity} />
            <span className="text-sm">{findingLabel(t, finding.code)}</span>
            <span className="font-mono text-[10px] text-faint">{finding.code}</span>
            <Badge tone="neutral" className="font-mono text-[10px]">
              {t.call.origins[finding.origin as keyof typeof t.call.origins] ?? finding.origin} →{' '}
              {t.call.sinks[finding.sink as keyof typeof t.call.sinks] ?? finding.sink}
            </Badge>
            <span className="ml-auto font-mono text-xs text-faint">
              {Math.round(finding.confidence * 100)}%
            </span>
          </div>
          <Axes finding={finding} />
          {finding.evidence.map((evidence, index) => (
            <blockquote
              key={index}
              className="mt-2 border-l-2 border-warning/50 pl-2 font-mono text-xs text-muted"
            >
              {evidence.quote || '—'}
              <span className="ml-1 text-faint">
                {evidence.section}
                {evidence.path ? ` · ${evidence.path}` : ''}
              </span>
            </blockquote>
          ))}
          <div className="mt-1.5 flex flex-wrap gap-x-3 text-[11px] text-faint">
            <span>
              {finding.detector_id}@{finding.detector_version}
            </span>
            {finding.attribution.map((attribution, index) => (
              <span key={index}>
                {interpolate(t.call.turnItem, {
                  turn: attribution.turn,
                  item: attribution.item,
                })}
                {attribution.tool_call_id ? ` / ${attribution.tool_call_id}` : ''}
              </span>
            ))}
          </div>
        </li>
      ))}
    </ul>
  );
}

function FlowList({ flows }: { flows: ConsoleAnalysisView['flows'] }) {
  const { t } = useLocale();
  if (flows.length === 0) return null;
  return (
    <div className="flex flex-col gap-1 border-t pt-2">
      <h4 className="text-xs font-semibold text-muted">{t.call.flows}</h4>
      <ul className="flex flex-col gap-1">
        {flows.map((flow, index) => (
          <li
            key={`${flow.content_hash}:${index}`}
            className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px]"
          >
            <span className="font-mono">
              {t.call.origins[flow.source_origin as keyof typeof t.call.origins] ??
                flow.source_origin}
            </span>
            <span className="text-faint">
              {interpolate(t.call.turnItem, {
                turn: flow.source_turn,
                item: flow.source_item,
              })}
            </span>
            <span className="text-faint">→</span>
            <span className="font-mono">
              {t.call.sinks[flow.sink as keyof typeof t.call.sinks] ?? flow.sink}
            </span>
            {flow.sink_tool_call_id && (
              <span className="font-mono text-faint">{flow.sink_tool_call_id}</span>
            )}
            <Badge tone="neutral">
              {t.call.relations[flow.relation as keyof typeof t.call.relations] ?? flow.relation}
            </Badge>
            <span className="ml-auto text-faint">{Math.round(flow.confidence * 100)}%</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function AnalysisCard({ analysis }: { analysis: ConsoleAnalysisView }) {
  const { t, locale } = useLocale();
  const verdict = analysis.verdict;
  const model = analysis.model;
  return (
    <li className="flex flex-col gap-2 rounded-md border bg-surface px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={verdict.value === 'unsafe' ? 'danger' : 'neutral'}>
          {t.call.verdicts[verdict.value]}
        </Badge>
        <span className="font-mono text-xs text-faint">{analysis.id}</span>
        <span className="text-xs text-muted">
          {t.call.states[analysis.state as keyof typeof t.call.states] ?? analysis.state}
        </span>
        <span className="ml-auto text-xs text-faint">
          {formatDate(analysis.started_at, locale)}
        </span>
      </div>
      <div className="flex flex-wrap gap-x-3 text-[11px] text-faint">
        <span>
          {analysis.findings.length}{' '}
          {analysis.findings.length === 1 ? t.call.findingOne : t.call.findings}
        </span>
        <span>
          {analysis.flows.length} {analysis.flows.length === 1 ? t.call.flowOne : t.call.flows}
        </span>
        <span>
          {t.call.coverage}:{' '}
          {analysis.coverage.complete ? t.call.coverageComplete : t.call.coverageIncomplete}
        </span>
        <span>
          {t.call.modelState}:{' '}
          {t.call.modelStates[analysis.model_state as keyof typeof t.call.modelStates] ??
            analysis.model_state}
        </span>
        {verdict.reason && (
          <span>
            {t.call.verdictReasons[verdict.reason as keyof typeof t.call.verdictReasons] ??
              verdict.reason}
          </span>
        )}
        <span className="font-mono">
          {t.call.versions}: {analysis.versions.detectors} · {analysis.versions.catalogue} ·{' '}
          {analysis.versions.taxonomy}
          {analysis.versions.decision_model ? ` · ${analysis.versions.decision_model}` : ''}
        </span>
      </div>
      {analysis.coverage.errors.length > 0 && (
        <p className="font-mono text-[11px] text-warning">{analysis.coverage.errors.join(' · ')}</p>
      )}
      {analysis.error && (
        <p className="text-xs text-danger">
          {t.call.analysisError}: {analysis.error}
        </p>
      )}
      {model && (
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <Badge tone={model.unsafe ? 'danger' : 'neutral'}>
            {model.unsafe ? t.call.verdicts.unsafe : t.call.verdicts.safe}:{' '}
            {Math.round(model.probability * 100)}%
          </Badge>
          <span className="text-faint">
            {model.confidence_known
              ? `${t.call.confidence} ${Math.round(model.confidence * 100)}%`
              : `${t.call.confidence}: ${t.call.verdicts.unknown}`}
          </span>
          {model.severity && (
            <Badge tone="neutral" className="font-mono">
              {t.call.modelSeverity}: {model.severity}
            </Badge>
          )}
          {model.source && (
            <Badge tone="neutral" className="font-mono">
              {model.source}
            </Badge>
          )}
          {model.mode && (
            <Badge tone="neutral" className="font-mono">
              {model.mode}
            </Badge>
          )}
          {model.harm && (
            <Badge tone="neutral" className="font-mono">
              {model.harm}
            </Badge>
          )}
        </div>
      )}
      <Findings findings={analysis.findings} />
      <FlowList flows={analysis.flows} />
    </li>
  );
}

export function AnalysesSection({
  detail,
  projectId,
}: {
  detail: ConsoleCallDetailView;
  projectId: string;
}) {
  const { t } = useLocale();
  const reanalyze = useReanalyzeCall();
  const queryClient = useQueryClient();
  const [compare, setCompare] = useState(false);
  const diff = useDiffAnalyses(
    projectId,
    { before: detail.analyses[1]?.id ?? '', after: detail.analyses[0]?.id ?? '' },
    { query: { enabled: compare && detail.analyses.length > 1 } },
  );
  return (
    <Section title={t.call.analyses}>
      <div className="flex justify-end gap-2">
        {detail.analyses.length > 1 && (
          <Button size="sm" variant="ghost" onClick={() => setCompare((value) => !value)}>
            {compare ? t.call.hideComparison : t.call.compare}
          </Button>
        )}
        <Button
          size="sm"
          variant="secondary"
          loading={reanalyze.isPending}
          onClick={async () => {
            try {
              await reanalyze.mutateAsync({ projectId, callId: detail.call.id });
              await queryClient.invalidateQueries({
                queryKey: getGetCallQueryKey(projectId, detail.call.id),
              });
              toast.success(t.call.reanalyzed);
            } catch (error) {
              toast.error(errorMessage(t, error));
            }
          }}
        >
          {t.call.reanalyze}
        </Button>
      </div>
      {compare && diff.data && (
        <div className="rounded-md border bg-surface px-3 py-2 text-xs">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={diff.data.comparable ? 'success' : 'warning'}>
              {diff.data.comparable
                ? t.call.comparable
                : (t.call.diffReasons[diff.data.reason as keyof typeof t.call.diffReasons] ??
                  diff.data.reason)}
            </Badge>
            <span className="text-muted">
              {t.call.comparisonCounts}: +{diff.data.added.length} / −{diff.data.removed.length} / ~
              {diff.data.changed.length}
            </span>
          </div>
          {(diff.data.added.length > 0 || diff.data.changed.length > 0) && (
            <div className="mt-1 flex flex-wrap gap-1">
              {diff.data.added.map((finding) => (
                <Badge key={finding.fingerprint} tone="danger" className="font-mono text-[10px]">
                  + {finding.code}
                </Badge>
              ))}
              {diff.data.changed.map((change) => (
                <Badge key={change.fingerprint} tone="warning" className="font-mono text-[10px]">
                  ~ {change.code}
                </Badge>
              ))}
            </div>
          )}
        </div>
      )}
      {detail.analyses.length === 0 ? (
        <p className="text-sm text-muted">{t.call.noAnalyses}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {detail.analyses.map((analysis) => (
            <AnalysisCard key={analysis.id} analysis={analysis} />
          ))}
        </ul>
      )}
    </Section>
  );
}

export function DecisionsSection({ detail }: { detail: ConsoleCallDetailView }) {
  const { t, locale } = useLocale();
  return (
    <Section title={t.call.decisions}>
      {detail.decisions.length === 0 ? (
        <p className="text-sm text-muted">{t.call.noDecisions}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {detail.decisions.map((decision) => (
            <li
              key={decision.id}
              className="flex flex-wrap items-center gap-2 rounded-md border bg-surface px-3 py-2 text-xs"
            >
              <Badge tone={decision.blocked ? 'danger' : 'neutral'}>
                {t.call.decisionActions[decision.action as keyof typeof t.call.decisionActions] ??
                  decision.action}
              </Badge>
              <span className="font-mono">
                {t.call.phases[decision.phase as keyof typeof t.call.phases] ?? decision.phase}
              </span>
              {decision.rule_id && <span className="font-mono text-faint">{decision.rule_id}</span>}
              <span className="text-muted">{decision.reason}</span>
              <span className="ml-auto text-faint">{formatDate(decision.created_at, locale)}</span>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

export function EventsSection({ detail }: { detail: ConsoleCallDetailView }) {
  const { t } = useLocale();
  return (
    <div className="flex flex-col gap-2">
      {detail.call.losses.length > 0 && (
        <JsonView summary={t.call.losses} value={detail.call.losses} />
      )}
      {detail.events.length === 0 ? (
        <p className="text-sm text-muted">{t.call.eventsAdminOnly}</p>
      ) : (
        detail.events.map((event) => (
          <JsonView
            key={event.sequence}
            summary={interpolate(t.call.eventSummary, {
              sequence: event.sequence,
              kind: event.kind,
            })}
            value={event.payload}
          />
        ))
      )}
    </div>
  );
}
