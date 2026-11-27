import { Link } from '@tanstack/react-router';
import { useState } from 'react';
import type { ConsoleCallDetailView } from '../../../api/generated/models';
import { useGetCall } from '../../../api/generated/calls/calls';
import { QueryState } from '../../../components/feedback/QueryState';
import { SeverityBadge, StateBadge } from '../../../components/feedback/Badges';
import { DataTable } from '../../../components/data/DataTable';
import { Badge } from '../../../components/ui/badge';
import { Button } from '../../../components/ui/button';
import { Card, CardHeader } from '../../../components/ui/card';
import { Skeleton } from '../../../components/ui/skeleton';
import { errorMessage, interpolate, useLocale } from '../../../i18n';
import { formatDate, formatDuration, formatTokens, shortId } from '../../../lib/format';

export function CallDetail({ projectId, callId }: { projectId: string; callId: string }) {
  const { t, locale } = useLocale();
  const [showPayload, setShowPayload] = useState(false);
  const query = useGetCall(projectId, callId);
  return (
    <QueryState query={query} loading={<Skeleton className="m-4 h-64" />}>
      {(detail: ConsoleCallDetailView) => {
        const call = detail.call;
        return (
          <div className="flex flex-col gap-3 p-4">
            <div className="flex flex-wrap items-center gap-2">
              <StateBadge state={call.state} />
              <SeverityBadge severity={call.max_severity} />
              <span className="font-mono text-sm">{call.public_model || '—'}</span>
              <span className="ml-auto text-xs text-faint">
                {formatDate(call.created_at, locale)}
              </span>
            </div>
            <div className="grid grid-cols-2 gap-2 text-xs">
              <Metric label={t.call.duration} value={formatDuration(call.duration_ms)} />
              <Metric
                label={t.call.firstToken}
                value={call.first_token_ms ? `${call.first_token_ms} ms` : '—'}
              />
              <Metric label={t.call.inputTokens} value={formatTokens(call.input_tokens)} />
              <Metric label={t.call.outputTokens} value={formatTokens(call.output_tokens)} />
              <Metric label={t.call.clientFormat} value={call.client_format} />
              <Metric label={t.call.upstreamModel} value={call.upstream_model || '—'} />
              <Metric label={t.call.attempts} value={String(call.attempts)} />
              <Metric label={t.call.redactionValues} value={String(call.redaction_values)} />
            </div>
            <div className="flex flex-wrap gap-2 text-xs">
              <Badge>{call.lane}</Badge>
              <Badge>{call.operation}</Badge>
              <Badge>{call.purpose}</Badge>
              {call.outcome && <Badge>{call.outcome}</Badge>}
              {call.error_code && <Badge tone="danger">{call.error_code}</Badge>}
            </div>
            {call.trajectory_id && (
              <Link
                className="text-xs text-accent hover:underline"
                to="/projects/$projectId/trajectories/$trajectoryId"
                params={{ projectId, trajectoryId: call.trajectory_id }}
              >
                {t.trajectory.title.replace('{id}', shortId(call.trajectory_id))}
              </Link>
            )}
            <Card>
              <CardHeader title={t.call.integrity} />
              <div className="flex items-center gap-2 p-3 text-xs">
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
                <span className="text-muted">
                  {interpolate(t.call.eventsCount, { count: String(detail.integrity.events) })}
                </span>
                {detail.integrity.error && (
                  <span className="text-danger">{detail.integrity.error}</span>
                )}
              </div>
            </Card>
            <Card>
              <CardHeader title={t.call.attempts} />
              {detail.attempts.length === 0 ? (
                <p className="p-3 text-sm text-muted">{t.call.noAttempts}</p>
              ) : (
                <DataTable
                  rows={detail.attempts}
                  rowKey={(attempt) => String(attempt.index)}
                  columns={[
                    { key: 'index', header: '#', cell: (attempt) => attempt.index + 1 },
                    {
                      key: 'channel',
                      header: t.call.channel,
                      cell: (attempt) => attempt.channel_name || attempt.channel_id,
                    },
                    {
                      key: 'model',
                      header: t.call.upstreamModel,
                      cell: (attempt) => attempt.upstream_model,
                    },
                    {
                      key: 'result',
                      header: t.call.result,
                      cell: (attempt) => (
                        <Badge tone={attempt.result === 'succeeded' ? 'accent' : 'danger'}>
                          {attempt.result}
                        </Badge>
                      ),
                    },
                    {
                      key: 'duration',
                      header: t.call.duration,
                      align: 'right',
                      cell: (attempt) => formatDuration(attempt.duration_ms),
                    },
                  ]}
                />
              )}
            </Card>
            <Card>
              <CardHeader
                title={t.call.findings}
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
              {detail.findings.length === 0 ? (
                <p className="p-3 text-sm text-muted">{t.call.noFindings}</p>
              ) : (
                <ul className="divide-y text-xs">
                  {detail.findings.map((finding) => (
                    <li
                      key={finding.id || finding.fingerprint}
                      className="flex flex-wrap items-center gap-2 p-3"
                    >
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
                      <span className="text-muted">{finding.invariant || finding.origin}</span>
                      <span className="ml-auto text-faint">{finding.control}</span>
                    </li>
                  ))}
                </ul>
              )}
            </Card>
            <Card>
              <CardHeader title={t.call.events} description={t.call.eventsAdminOnly} />
              <ul className="divide-y text-xs">
                {detail.events.map((event) => (
                  <li key={event.id} className="flex flex-wrap items-center gap-2 p-2">
                    <span className="num text-faint">#{event.seq}</span>
                    <Badge>{event.kind}</Badge>
                    <span className="text-muted">{event.summary}</span>
                    {event.code && <span className="font-mono text-danger">{event.code}</span>}
                    <span className="ml-auto text-faint">
                      {formatDate(event.occurred_at, locale)}
                    </span>
                    {showPayload && event.payload != null && (
                      <pre className="w-full overflow-x-auto rounded bg-sunken p-2 font-mono text-[11px]">
                        {JSON.stringify(event.payload as Record<string, unknown>, null, 2)}
                      </pre>
                    )}
                  </li>
                ))}
              </ul>
            </Card>
            {query.isError && <p className="text-sm text-danger">{errorMessage(t, query.error)}</p>}
          </div>
        );
      }}
    </QueryState>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded bg-sunken px-2 py-1.5">
      <span className="text-[10px] uppercase tracking-wide text-faint">{label}</span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  );
}
