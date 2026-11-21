import { Link } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { useGetCall } from '../../../api/generated/calls/calls';
import type { ConsoleCallView } from '../../../api/generated/models';
import { TableSkeleton } from '../../../components/data/DataTable';
import { QueryState } from '../../../components/feedback/QueryState';
import { SeverityBadge, StateBadge } from '../../../components/feedback/StatusBadge';
import { Badge } from '../../../components/ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../components/ui/tabs';
import { usePollingInterval } from '../../../hooks/usePolling';
import { useLocale } from '../../../i18n';
import {
  formatCost,
  formatDate,
  formatDuration,
  formatNumber,
  placeholder,
} from '../../../lib/format';
import { AttemptWaterfall } from './AttemptWaterfall';
import { AnalysesSection, DecisionsSection, EventsSection, Section } from './CallSections';
import { Timeline } from './Timeline';
import { Transcript } from './Transcript';

function Metrics({ call }: { call: ConsoleCallView }) {
  const { t } = useLocale();
  const items = [
    [t.call.duration, formatDuration(call.duration_ms)],
    [t.call.firstToken, formatDuration(call.first_token_ms)],
    [t.call.inputTokens, formatNumber(call.input_tokens)],
    [t.call.outputTokens, formatNumber(call.output_tokens)],
    [
      t.call.cacheTokens,
      `${formatNumber(call.cache_read_tokens)} / ${formatNumber(call.cache_write_tokens)}`,
    ],
    [t.call.reasoningTokens, formatNumber(call.reasoning_tokens)],
    [t.call.cost, formatCost(call.cost_micros, call.currency)],
    [t.call.upstreamModel, call.upstream_model || placeholder],
    [t.call.clientFormat, call.client_format],
    [t.call.stream, call.stream ? t.common.enabled : t.common.disabled],
  ];
  return (
    <Section title={t.call.metrics}>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-3 2xl:grid-cols-5">
        {items.map(([label, value]) => (
          <div key={label} className="min-w-0">
            <dt className="text-xs text-muted">{label}</dt>
            <dd className="num truncate text-sm font-medium">{value}</dd>
          </div>
        ))}
      </dl>
      {!call.usage_known && <p className="text-xs text-faint">{t.call.usageUnknown}</p>}
    </Section>
  );
}

export function CallDetail({ projectId, callId }: { projectId: string; callId: string }) {
  const { t, locale } = useLocale();
  const [pending, setPending] = useState(true);
  const interval = usePollingInterval(5_000, !pending);
  const detail = useGetCall(projectId, callId, { query: { refetchInterval: interval } });
  useEffect(() => {
    if (detail.data) {
      setPending(['pending', 'running'].includes(detail.data.call.analysis_state));
    }
  }, [detail.data]);
  return (
    <QueryState query={detail} loading={<TableSkeleton rows={8} />}>
      {(loaded) => {
        const call = loaded.call;
        return (
          <div className="flex flex-col gap-5 p-4">
            <header className="flex flex-col gap-2">
              <div className="flex flex-wrap items-center gap-2">
                <StateBadge state={call.state} />
                <SeverityBadge severity={call.max_severity} />
                <Badge tone={loaded.integrity.status === 'verified' ? 'success' : 'neutral'}>
                  {t.call.integrityStatuses[
                    loaded.integrity.status as keyof typeof t.call.integrityStatuses
                  ] ?? loaded.integrity.status}
                </Badge>
                <h2 className="font-mono text-base font-semibold">{call.public_model}</h2>
                <span className="ml-auto text-xs text-muted">
                  {call.finding_count}{' '}
                  {call.finding_count === 1 ? t.call.findingOne : t.call.findings} ·{' '}
                  {t.call.analysisStates[
                    call.analysis_state as keyof typeof t.call.analysisStates
                  ] ?? call.analysis_state}
                </span>
              </div>
              <p className="flex flex-wrap gap-x-3 font-mono text-xs text-muted">
                <span>{call.id}</span>
                <span>{formatDate(call.created_at, locale)}</span>
                {call.error_code && (
                  <span className="text-danger">
                    {t.call.errorCode}: {call.error_code}
                  </span>
                )}
              </p>
              {call.session_id && (
                <Link
                  to="/projects/$projectId/conversations/$sessionId"
                  params={{ projectId, sessionId: call.session_id }}
                  className="self-start text-xs text-accent hover:underline"
                >
                  {t.call.openConversation}
                </Link>
              )}
            </header>
            <Metrics call={call} />
            <Tabs defaultValue="analysis">
              <TabsList>
                <TabsTrigger value="analysis">{t.call.analysis}</TabsTrigger>
                <TabsTrigger value="transcript">{t.call.transcript}</TabsTrigger>
                <TabsTrigger value="attempts">{t.call.attempts}</TabsTrigger>
                <TabsTrigger value="timeline">{t.call.timeline}</TabsTrigger>
                <TabsTrigger value="events">{t.call.events}</TabsTrigger>
              </TabsList>
              <TabsContent value="analysis" className="flex flex-col gap-5 pt-3">
                <DecisionsSection detail={loaded} />
                <AnalysesSection detail={loaded} projectId={projectId} />
              </TabsContent>
              <TabsContent value="transcript" className="pt-3">
                <Transcript transcript={loaded.transcript} />
              </TabsContent>
              <TabsContent value="attempts" className="pt-3">
                {loaded.attempts.length > 0 ? (
                  <AttemptWaterfall attempts={loaded.attempts} />
                ) : (
                  <p className="text-sm text-muted">{t.call.noAttempts}</p>
                )}
              </TabsContent>
              <TabsContent value="timeline" className="pt-3">
                <Timeline detail={loaded} />
              </TabsContent>
              <TabsContent value="events" className="pt-3">
                <EventsSection detail={loaded} />
              </TabsContent>
            </Tabs>
          </div>
        );
      }}
    </QueryState>
  );
}
