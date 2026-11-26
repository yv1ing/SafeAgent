import { Link } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { useGetCall } from '../../../api/generated/calls/calls';
import type { ConsoleCallView } from '../../../api/generated/models';
import { useCurrentUser } from '../../../app/context';
import { TableSkeleton } from '../../../components/data/DataTable';
import { Callout } from '../../../components/feedback/Callout';
import { QueryState } from '../../../components/feedback/QueryState';
import {
  LaneBadge,
  OutcomeBadge,
  SeverityBadge,
  StateBadge,
} from '../../../components/feedback/Badges';
import { Badge } from '../../../components/ui/badge';
import { Segmented } from '../../../components/feedback/Segmented';
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
import type { ItemReveal } from '../reveal';
import { AttemptWaterfall } from './AttemptWaterfall';
import { OperationBadge, PurposeBadge, ResolutionBadge } from './CallBadges';
import { CallRedactions } from './CallRedactions';
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
    [t.call.attempts, formatNumber(call.attempts)],
    [t.call.redactionValues, formatNumber(call.redaction_values)],
  ];
  return (
    <Section title={t.call.metrics}>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-3 2xl:grid-cols-6">
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

export function CallDetail({
  projectId,
  callId,
  reveal,
  onlyValueId,
}: {
  projectId: string;
  callId: string;
  reveal?: ItemReveal;
  onlyValueId?: string | null;
}) {
  const { t, locale } = useLocale();
  const user = useCurrentUser();
  const [pending, setPending] = useState(true);
  const [tab, setTab] = useState(reveal || onlyValueId ? 'transcript' : 'analysis');
  const [rawView, setRawView] = useState(user.administrator);
  const [localReveal, setLocalReveal] = useState<ItemReveal | undefined>(reveal);
  const activeReveal = localReveal ?? reveal;
  const interval = usePollingInterval(5_000, !pending);
  const detail = useGetCall(projectId, callId, { query: { refetchInterval: interval } });
  useEffect(() => {
    if (detail.data) {
      setPending(detail.data.call.analysis_state === 'pending');
    }
  }, [detail.data]);
  useEffect(() => {
    if (!reveal) return;
    setLocalReveal(reveal);
    setTab('transcript');
    setRawView(false);
  }, [reveal]);
  useEffect(() => {
    if (!onlyValueId) return;
    setTab('transcript');
    setRawView(false);
  }, [onlyValueId]);
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
                {call.lane !== 'main' && <LaneBadge lane={call.lane} />}
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
              <div className="flex flex-wrap items-center gap-1.5">
                <OperationBadge operation={call.operation} />
                <PurposeBadge purpose={call.purpose} source={call.purpose_source} />
                <OutcomeBadge outcome={call.outcome} />
                <ResolutionBadge resolution={call.resolution} />
                {call.error_code && (
                  <Badge tone="danger" className="font-mono">
                    {t.call.errorCode}: {call.error_code}
                  </Badge>
                )}
              </div>
              <p className="flex flex-wrap gap-x-3 font-mono text-xs text-muted">
                <span>{call.id}</span>
                <span>{formatDate(call.created_at, locale)}</span>
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
            {call.resolution === 'fallback' && (
              <Callout tone="warning">{t.call.fallbackHint}</Callout>
            )}
            <Metrics call={call} />
            <Tabs value={tab} onValueChange={setTab}>
              <TabsList>
                <TabsTrigger value="analysis">{t.call.analysis}</TabsTrigger>
                <TabsTrigger value="transcript">{t.call.transcript}</TabsTrigger>
                <TabsTrigger value="redactions">
                  {t.call.redactions}
                  {loaded.hits.length > 0 && (
                    <span className="num ml-1 text-faint">{loaded.hits.length}</span>
                  )}
                </TabsTrigger>
                <TabsTrigger value="attempts">{t.call.attempts}</TabsTrigger>
                <TabsTrigger value="timeline">{t.call.timeline}</TabsTrigger>
                <TabsTrigger value="events">{t.call.events}</TabsTrigger>
              </TabsList>
              <TabsContent value="analysis" className="flex flex-col gap-5 pt-3">
                <DecisionsSection detail={loaded} />
                <AnalysesSection detail={loaded} projectId={projectId} />
              </TabsContent>
              <TabsContent value="transcript" className="pt-3">
                <div className="flex flex-col gap-3">
                  {user.administrator && (
                    <div className="flex justify-end">
                      <Segmented
                        label={t.call.transcriptView}
                        value={rawView ? 'raw' : 'shown'}
                        options={[
                          { value: 'shown', label: t.call.shownView },
                          { value: 'raw', label: t.call.rawView },
                        ]}
                        onChange={(view) => setRawView(view === 'raw')}
                      />
                    </div>
                  )}
                  <Transcript
                    transcript={loaded.transcript}
                    showRaw={user.administrator && rawView}
                    reveal={activeReveal}
                    valueFilter={onlyValueId ?? activeReveal?.valueId ?? null}
                  />
                </div>
              </TabsContent>
              <TabsContent value="redactions" className="pt-3">
                <CallRedactions
                  hits={loaded.hits}
                  onReveal={(occurrence, valueId) => {
                    setLocalReveal({
                      callId: call.id,
                      section: occurrence.section,
                      ordinal: occurrence.ordinal,
                      valueId,
                    });
                    setTab('transcript');
                    setRawView(false);
                  }}
                />
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
