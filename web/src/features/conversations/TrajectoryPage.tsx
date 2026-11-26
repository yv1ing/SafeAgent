import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import { ChevronLeft, List, Network } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import {
  useGetConversationRedactions,
  useGetConversationTrajectory,
} from '../../api/generated/conversations/conversations';
import { Callout } from '../../components/feedback/Callout';
import { QueryState } from '../../components/feedback/QueryState';
import { Segmented } from '../../components/feedback/Segmented';
import { StatTile } from '../../components/data/StatTile';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { buttonClass } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Sheet } from '../../components/ui/sheet';
import { Skeleton } from '../../components/ui/skeleton';
import { Switch } from '../../components/ui/switch';
import { interpolate, useLocale } from '../../i18n';
import { formatCost, formatDuration, formatNumber, formatTokens, shortId } from '../../lib/format';
import { useMediaQuery } from '../../lib/media';
import type { ItemReveal } from '../calls/reveal';
import { RedactionPanel, RedactionSummaryChips } from '../redaction/components/RedactionPanel';
import type { RevealTarget } from '../redaction/redaction';
import { TrajectoryFlat } from './components/TrajectoryFlat';
import { TrajectoryFlow } from './components/TrajectoryFlow';
import { TrajectoryInspector } from './components/TrajectoryInspector';
import { TrajectoryReplay } from './components/TrajectoryReplay';
import { TrajectoryTree } from './components/TrajectoryTree';
import type { TrajectorySearch } from './search';
import {
  callNodeId,
  findNode,
  replayIntervalMs,
  stepAfter,
  touchedNodeIds,
  visibleTrajectory,
} from './trajectory';

const route = getRouteApi('/app/projects/$projectId/conversations/$sessionId');
const views = ['flow', 'tree', 'flat'] as const;
type TrajectoryView = (typeof views)[number];

export function TrajectoryPage() {
  const { t } = useLocale();
  const { projectId, sessionId } = route.useParams();
  const search = route.useSearch() as TrajectorySearch;
  const navigate = useNavigate();
  const wide = useMediaQuery('(min-width: 80rem)');
  const trajectory = useGetConversationTrajectory(projectId, sessionId);
  const redactions = useGetConversationRedactions(projectId, sessionId);
  const [view, setView] = useState<TrajectoryView>('flow');
  const [foldSide, setFoldSide] = useState(true);
  const [onlyValueId, setOnlyValueId] = useState<string | null>(null);
  const [panelOpen, setPanelOpen] = useState(false);
  const [replayIndex, setReplayIndex] = useState(-1);
  const [playing, setPlaying] = useState(false);
  const [reveal, setReveal] = useState<ItemReveal | undefined>();
  const selectedId = search.node;

  useEffect(() => {
    setReplayIndex(-1);
    setPlaying(false);
    setOnlyValueId(null);
    setPanelOpen(false);
    setReveal(undefined);
    setView('flow');
  }, [sessionId]);

  const steps = trajectory.data?.steps ?? [];

  useEffect(() => {
    if (!playing) return;
    if (replayIndex >= steps.length) {
      setPlaying(false);
      return;
    }
    const timer = setTimeout(
      () => setReplayIndex(stepAfter(replayIndex, steps.length)),
      replayIntervalMs,
    );
    return () => clearTimeout(timer);
  }, [playing, replayIndex, steps.length]);

  const selected = trajectory.data ? findNode(trajectory.data.nodes, selectedId ?? '') : undefined;
  const selectedCall = trajectory.data?.nodes.find(
    (node) => node.id === callNodeId(selected?.call_id ?? ''),
  );
  const selectedIsSide = selected?.call?.lane === 'side' || selectedCall?.call?.lane === 'side';
  const effectiveFold = foldSide && !selectedIsSide;
  const slice = useMemo(
    () =>
      trajectory.data
        ? visibleTrajectory(trajectory.data, effectiveFold)
        : { nodes: [], edges: [] },
    [trajectory.data, effectiveFold],
  );
  const touched = useMemo(
    () => (trajectory.data ? touchedNodeIds(trajectory.data, replayIndex) : new Set<string>()),
    [trajectory.data, replayIndex],
  );

  const selectNode = (nodeId?: string) =>
    void navigate({
      to: '/projects/$projectId/conversations/$sessionId',
      params: { projectId, sessionId },
      search: { node: nodeId },
    });
  const revealTarget = (target: RevealTarget) => {
    setReveal({
      callId: target.call_id,
      section: target.section,
      ordinal: target.ordinal,
      valueId: target.value_id,
    });
    setPanelOpen(false);
    selectNode(callNodeId(target.call_id));
  };
  const inspector = (
    <TrajectoryInspector
      node={selected}
      redactions={redactions.data}
      projectId={projectId}
      reveal={reveal}
      onlyValueId={onlyValueId}
      onSelect={selectNode}
      onReveal={revealTarget}
      onOnlyValue={setOnlyValueId}
      onClose={() => selectNode(undefined)}
    />
  );

  return (
    <Page>
      <Link
        to="/projects/$projectId/conversations"
        params={{ projectId }}
        className="inline-flex items-center gap-1 self-start text-sm text-muted hover:text-ink"
      >
        <ChevronLeft aria-hidden className="size-4" />
        {t.nav.conversations}
      </Link>
      <QueryState
        query={trajectory}
        loading={
          <div className="flex flex-col gap-4">
            <Skeleton className="h-24" />
            <Skeleton className="h-96" />
          </div>
        }
      >
        {(data) => (
          <>
            <PageHeader
              title={interpolate(t.trajectory.title, { id: shortId(sessionId) })}
              description={interpolate(t.trajectory.description, {
                calls: formatNumber(data.stats.calls),
                turns: formatNumber(data.stats.turns),
                items: formatNumber(data.stats.items),
              })}
              actions={
                <>
                  <Link
                    to="/projects/$projectId/calls"
                    params={{ projectId }}
                    search={{ session_id: sessionId }}
                    className={buttonClass('secondary', 'sm')}
                  >
                    <List aria-hidden />
                    {t.trajectory.viewCalls}
                  </Link>
                  {redactions.data && (
                    <RedactionSummaryChips
                      index={redactions.data}
                      onlyValueId={onlyValueId}
                      onOpen={() => setPanelOpen(true)}
                      onClearValue={() => setOnlyValueId(null)}
                    />
                  )}
                </>
              }
            />
            {data.truncated && <Callout tone="warning">{t.trajectory.truncated}</Callout>}
            {data.warnings.length > 0 && (
              <Callout tone="warning">
                <p className="font-medium">{t.trajectory.warnings}</p>
                <ul className="mt-1 list-disc pl-4 text-xs">
                  {data.warnings.map((warning) => (
                    <li key={warning}>{warning}</li>
                  ))}
                </ul>
              </Callout>
            )}
            {selectedId && data.nodes.length > 0 && !selected && (
              <Callout tone="warning">{t.trajectory.nodeNotFound}</Callout>
            )}
            <Card className="p-0">
              <div className="grid grid-cols-2 gap-px bg-line md:grid-cols-3 xl:grid-cols-6">
                <StatTile label={t.trajectory.statCalls} value={formatNumber(data.stats.calls)} />
                <StatTile label={t.trajectory.statTurns} value={formatNumber(data.stats.turns)} />
                <StatTile label={t.trajectory.statItems} value={formatNumber(data.stats.items)} />
                <StatTile
                  label={t.trajectory.statTools}
                  value={formatNumber(data.stats.tool_calls)}
                />
                <StatTile
                  label={t.trajectory.statFindings}
                  value={formatNumber(data.stats.findings)}
                />
                <StatTile
                  label={t.trajectory.statRedactions}
                  value={formatNumber(data.stats.redactions)}
                />
                <StatTile
                  label={t.trajectory.statTokens}
                  value={formatTokens(data.stats.input_tokens + data.stats.output_tokens)}
                  detail={interpolate(t.trajectory.tokensDetail, {
                    input: formatTokens(data.stats.input_tokens),
                    output: formatTokens(data.stats.output_tokens),
                  })}
                />
                <StatTile
                  label={t.trajectory.statAttempts}
                  value={formatNumber(data.stats.attempts)}
                />
                <StatTile
                  label={t.trajectory.statDuration}
                  value={formatDuration(data.stats.duration_ms)}
                />
                <StatTile
                  label={t.trajectory.statCost}
                  value={
                    data.stats.cost_micros === undefined || data.stats.cost_micros === null
                      ? t.overview.noSpend
                      : formatCost(data.stats.cost_micros, data.stats.currency)
                  }
                />
              </div>
            </Card>
            <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
              <div className="flex min-w-0 flex-col gap-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Segmented
                    label={t.trajectory.view}
                    value={view}
                    options={views.map((value) => ({
                      value,
                      label: t.trajectory.views[value],
                    }))}
                    onChange={setView}
                  />
                  <label className="flex items-center gap-2 text-xs text-muted">
                    <Switch
                      aria-label={t.trajectory.foldSide}
                      checked={foldSide}
                      onCheckedChange={setFoldSide}
                    />
                    <span>{t.trajectory.foldSide}</span>
                  </label>
                </div>
                <Card className="overflow-hidden">
                  <TrajectoryReplay
                    steps={steps}
                    index={replayIndex}
                    playing={playing}
                    touched={touched}
                    nodes={slice.nodes}
                    onStep={(index) => {
                      setPlaying(false);
                      setReplayIndex(index);
                    }}
                    onTogglePlay={() => {
                      if (replayIndex >= steps.length) setReplayIndex(-1);
                      setPlaying((value) => !value);
                    }}
                    onSelect={selectNode}
                  />
                  {slice.nodes.length === 0 ? (
                    <Empty icon={Network} title={t.trajectory.empty} />
                  ) : view === 'flow' ? (
                    <div className="p-2">
                      <TrajectoryFlow
                        nodes={slice.nodes}
                        edges={slice.edges}
                        selectedId={selectedId}
                        touched={touched}
                        onSelect={selectNode}
                      />
                    </div>
                  ) : view === 'tree' ? (
                    <TrajectoryTree
                      nodes={slice.nodes}
                      edges={slice.edges}
                      selectedId={selectedId}
                      touched={touched}
                      onSelect={selectNode}
                    />
                  ) : (
                    <TrajectoryFlat
                      nodes={slice.nodes}
                      edges={slice.edges}
                      selectedId={selectedId}
                      touched={touched}
                      onSelect={selectNode}
                    />
                  )}
                </Card>
              </div>
              <Card className="sticky top-20 hidden max-h-[calc(100dvh-6rem)] overflow-y-auto xl:block">
                {inspector}
              </Card>
            </div>
            {!wide && (
              <Sheet
                open={Boolean(selected)}
                onOpenChange={(open) => !open && selectNode(undefined)}
                title={t.trajectory.inspectorTitle}
              >
                {inspector}
              </Sheet>
            )}
            <Sheet open={panelOpen} onOpenChange={setPanelOpen} title={t.redaction.sessionTitle}>
              {redactions.data ? (
                <RedactionPanel
                  index={redactions.data}
                  onlyValueId={onlyValueId}
                  onOnlyValue={setOnlyValueId}
                  onReveal={(occurrence, valueId) =>
                    revealTarget({
                      call_id: occurrence.call_id,
                      section: occurrence.section,
                      ordinal: occurrence.ordinal,
                      value_id: valueId,
                    })
                  }
                />
              ) : redactions.isError ? (
                <p className="p-4 text-sm text-danger">{t.common.loadFailed}</p>
              ) : (
                <Skeleton className="m-4 h-40" />
              )}
            </Sheet>
          </>
        )}
      </QueryState>
    </Page>
  );
}
