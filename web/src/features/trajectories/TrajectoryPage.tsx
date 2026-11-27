import { Link, useParams } from '@tanstack/react-router';
import { useState } from 'react';
import {
  useGetTrajectory,
  useGrantApproval,
  useDenyApproval,
} from '../../api/generated/trajectories/trajectories';
import { useCurrentUser, useProjectId } from '../../app/context';
import { DataTable } from '../../components/data/DataTable';
import { QueryState } from '../../components/feedback/QueryState';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { Progress } from '../../components/ui/progress';
import { Skeleton } from '../../components/ui/skeleton';
import { interpolate, useLocale } from '../../i18n';
import { formatDate, formatNumber, shortId } from '../../lib/format';

export function TrajectoryPage() {
  const { trajectoryId } = useParams({
    from: '/app/projects/$projectId/trajectories/$trajectoryId',
  });
  const projectId = useProjectId();
  const user = useCurrentUser();
  const { t, locale } = useLocale();
  const [showPayload, setShowPayload] = useState(false);
  const query = useGetTrajectory(projectId, trajectoryId);
  const grant = useGrantApproval();
  const deny = useDenyApproval();
  return (
    <Page>
      <PageHeader
        title={t.trajectory.title.replace('{id}', shortId(trajectoryId))}
        description={interpolate(t.trajectory.eventsCalls, {
          events: formatNumber(query.data?.summary.event_count ?? 0),
          calls: formatNumber(query.data?.summary.call_count ?? 0),
        })}
        actions={
          <Link
            className="text-xs text-accent hover:underline"
            to="/projects/$projectId/calls"
            params={{ projectId }}
            search={{ session_id: query.data?.summary.session_id || undefined }}
          >
            {t.trajectory.viewCalls}
          </Link>
        }
      />
      <QueryState query={query} loading={<Skeleton className="h-96" />}>
        {(detail) => (
          <div className="flex flex-col gap-4">
            <Card>
              <CardHeader
                title={t.call.coverage}
                description={interpolate(t.trajectory.boundaries, {
                  observed: formatNumber(detail.state.observed_boundaries),
                  expected: formatNumber(detail.state.expected_boundaries),
                })}
                actions={
                  <div className="flex items-center gap-2">
                    <Badge
                      tone={
                        detail.verdict.value === 'safe'
                          ? 'accent'
                          : detail.verdict.value === 'unsafe'
                            ? 'danger'
                            : 'warning'
                      }
                    >
                      {t.trajectory.verdicts[detail.verdict.value]}
                    </Badge>
                    <Badge
                      tone={
                        detail.state.coverage >= detail.state.coverage_threshold
                          ? 'accent'
                          : 'warning'
                      }
                    >
                      {Math.round(detail.state.coverage * 100)}%
                    </Badge>
                  </div>
                }
              />
              <div className="flex flex-col gap-2 p-4">
                <Progress
                  value={detail.state.coverage * 100}
                  max={100}
                  tone={
                    detail.state.coverage >= detail.state.coverage_threshold ? 'accent' : 'danger'
                  }
                />
                {detail.state.coverage < detail.state.coverage_threshold && (
                  <p className="text-xs text-danger">{t.trajectory.coverageBelow}</p>
                )}
                <div className="flex flex-wrap gap-4 text-xs text-muted">
                  <span>
                    {t.trajectory.goal}: {detail.state.goal || '—'}
                  </span>
                  <span>
                    {t.trajectory.risk}: {(detail.state.risk.score ?? 0).toFixed(2)}
                  </span>
                  <span>
                    {t.trajectory.policy}: {detail.state.policy_version}
                  </span>
                  <span>
                    {t.trajectory.gaps}: {detail.state.capture_gaps}
                  </span>
                </div>
              </div>
            </Card>

            <div className="grid gap-4 xl:grid-cols-2">
              <Card>
                <CardHeader title={t.call.integrity} />
                <div className="flex items-center gap-2 p-4 text-xs">
                  <Badge
                    tone={
                      detail.integrity.status === 'verified' ||
                      detail.integrity.status === 'in_progress'
                        ? 'accent'
                        : 'warning'
                    }
                  >
                    {t.call.integrityStatuses[
                      detail.integrity.status as keyof typeof t.call.integrityStatuses
                    ] ?? detail.integrity.status}
                  </Badge>
                  <span>
                    {interpolate(t.trajectory.eventsCount, {
                      count: String(detail.integrity.events),
                    })}
                  </span>
                  <span className="ml-auto font-mono text-faint">
                    {shortId(detail.integrity.head)}
                  </span>
                </div>
              </Card>
              <Card>
                <CardHeader title={t.call.decisions} />
                <div className="p-4 text-xs">
                  {detail.state.obligations.length === 0 ? (
                    <span className="text-muted">{t.trajectory.noObligations}</span>
                  ) : (
                    <ul className="flex flex-col gap-1">
                      {detail.state.obligations.map((obligation) => (
                        <li key={obligation.id}>
                          <Badge tone="warning">{obligation.state}</Badge> {obligation.kind}
                        </li>
                      ))}
                    </ul>
                  )}
                  {detail.state.approvals.length > 0 && (
                    <ul className="mt-3 flex flex-col gap-2">
                      {detail.state.approvals.map((approval) =>
                        approval.id ? (
                          <li key={approval.id} className="flex items-center gap-2">
                            <span className="font-mono">{shortId(approval.id)}</span>
                            <Badge>{approval.state}</Badge>
                            {user.administrator && approval.state === 'pending' && (
                              <>
                                <Button
                                  size="sm"
                                  variant="primary"
                                  onClick={() =>
                                    grant.mutate({
                                      projectId,
                                      trajectoryId,
                                      approvalId: approval.id ?? '',
                                    })
                                  }
                                >
                                  {t.trajectory.grant}
                                </Button>
                                <Button
                                  size="sm"
                                  variant="ghost"
                                  onClick={() =>
                                    deny.mutate({
                                      projectId,
                                      trajectoryId,
                                      approvalId: approval.id ?? '',
                                    })
                                  }
                                >
                                  {t.trajectory.deny}
                                </Button>
                              </>
                            )}
                          </li>
                        ) : null,
                      )}
                    </ul>
                  )}
                </div>
              </Card>
            </div>

            <Card>
              <CardHeader title={t.calls.title} />
              <DataTable
                rows={detail.calls}
                rowKey={(call) => call.id}
                columns={[
                  {
                    key: 'id',
                    header: t.trajectory.call,
                    cell: (call) => <span className="font-mono text-xs">{shortId(call.id)}</span>,
                  },
                  {
                    key: 'state',
                    header: t.trajectory.state,
                    cell: (call) => <Badge>{call.state}</Badge>,
                  },
                  {
                    key: 'model',
                    header: t.call.upstreamModel,
                    cell: (call) => call.upstream_model || call.public_model,
                  },
                  {
                    key: 'outcome',
                    header: t.trajectory.outcome,
                    cell: (call) => call.outcome || '—',
                  },
                  {
                    key: 'duration',
                    header: t.call.duration,
                    align: 'right',
                    cell: (call) => `${call.duration_ms} ms`,
                  },
                  {
                    key: 'tokens',
                    header: t.call.inputTokens,
                    align: 'right',
                    cell: (call) => formatNumber(call.input_tokens),
                  },
                ]}
              />
            </Card>

            <Card>
              <CardHeader
                title={t.call.findings}
                description={interpolate(t.trajectory.findingsCount, {
                  count: String(detail.findings.length),
                })}
              />
              {detail.findings.length === 0 ? (
                <p className="p-4 text-sm text-muted">{t.call.noFindings}</p>
              ) : (
                <ul className="divide-y text-xs">
                  {detail.findings.map((finding) => (
                    <li key={finding.id} className="flex flex-wrap items-center gap-2 p-3">
                      <Badge
                        tone={
                          finding.severity === 'high'
                            ? 'danger'
                            : finding.severity === 'medium'
                              ? 'warning'
                              : 'neutral'
                        }
                      >
                        {finding.severity}
                      </Badge>
                      <span className="font-mono">{finding.code}</span>
                      <span className="text-muted">
                        {finding.invariant} · {finding.transfer}
                      </span>
                      <span className="ml-auto text-faint">{finding.control}</span>
                    </li>
                  ))}
                </ul>
              )}
            </Card>

            <Card>
              <CardHeader
                title={t.call.events}
                actions={
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setShowPayload((value) => !value)}
                  >
                    {showPayload ? t.call.shownView : t.call.rawView}
                  </Button>
                }
              />
              <ul className="divide-y text-xs">
                {detail.events.map((event) => (
                  <li key={event.id} className="flex flex-col gap-1 p-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="num text-faint">#{event.seq}</span>
                      <Badge>{event.kind}</Badge>
                      <span className="text-muted">
                        {event.boundary} · {event.actor}
                      </span>
                      <span className="ml-auto text-faint">
                        {formatDate(event.occurred_at, locale)}
                      </span>
                    </div>
                    {showPayload && event.payload ? (
                      <pre className="overflow-x-auto rounded bg-sunken p-2 font-mono text-[11px]">
                        {JSON.stringify(event.payload, null, 2)}
                      </pre>
                    ) : null}
                  </li>
                ))}
              </ul>
            </Card>

            <Card>
              <CardHeader
                title={t.trajectory.segments}
                description={interpolate(t.trajectory.segmentsHint, {
                  count: String(detail.segments.length),
                })}
              />
              <ul className="grid gap-2 p-4 text-xs md:grid-cols-2">
                {detail.segments.map((segment) => (
                  <li
                    key={segment.id}
                    className="flex items-center gap-2 rounded bg-sunken px-2 py-1.5"
                  >
                    <span className="font-mono text-[10px]">{shortId(segment.id)}</span>
                    <Badge>{segment.integrity}</Badge>
                    <Badge>{segment.confidentiality}</Badge>
                    <span className="ml-auto text-faint">{segment.size} B</span>
                  </li>
                ))}
              </ul>
            </Card>
          </div>
        )}
      </QueryState>
    </Page>
  );
}
