import { Link } from '@tanstack/react-router';
import { ShieldAlert } from 'lucide-react';
import { useState } from 'react';
import type { ConsoleRiskView, GetProjectRiskWindow } from '../../api/generated/models';
import { useListProjectFlows } from '../../api/generated/flows/flows';
import { useGetProjectRisk } from '../../api/generated/risk/risk';
import { useProjectId } from '../../app/context';
import { DataTable } from '../../components/data/DataTable';
import { QueryState } from '../../components/feedback/QueryState';
import { Segmented } from '../../components/feedback/Segmented';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Progress } from '../../components/ui/progress';
import { Select } from '../../components/ui/select';
import { Skeleton } from '../../components/ui/skeleton';
import { usePollingInterval } from '../../hooks/usePolling';
import { errorMessage, findingLabel, interpolate, useLocale } from '../../i18n';
import { formatDate, formatNumber } from '../../lib/format';

const windows: GetProjectRiskWindow[] = ['24h', '7d', '30d'];

function Bar({ value, max, tone }: { value: number; max: number; tone?: 'accent' | 'danger' }) {
  return <Progress value={value} max={max} tone={tone ?? 'accent'} />;
}

function Distribution({
  title,
  items,
  empty,
  labelFor,
}: {
  title: string;
  items: { name?: string; count: number }[];
  empty: string;
  labelFor?: (name?: string) => string;
}) {
  return (
    <Card>
      <CardHeader title={title} />
      <ul className="flex flex-col gap-2 p-3">
        {items.length === 0 && <li className="text-sm text-muted">{empty}</li>}
        {items.map((item) => (
          <li key={item.name}>
            <div className="mb-1 flex justify-between gap-2 text-xs">
              <span className="min-w-0 font-mono text-[11px] leading-4 break-all" title={item.name}>
                {labelFor ? labelFor(item.name) : item.name}
              </span>
              <span className="num shrink-0 text-muted">{formatNumber(item.count)}</span>
            </div>
            <Bar value={item.count} max={items[0]?.count ?? 1} />
          </li>
        ))}
      </ul>
    </Card>
  );
}

function RiskContent({
  report,
  timeWindow,
}: {
  report: ConsoleRiskView;
  timeWindow: GetProjectRiskWindow;
}) {
  const { t, locale } = useLocale();
  const projectId = useProjectId();
  const [agentKey, setAgentKey] = useState('');
  const known = report.agents.some((agent) => agent.agent_key_id === agentKey);
  const selected = known ? agentKey : (report.agents[0]?.agent_key_id ?? '');
  const flows = useListProjectFlows(
    projectId,
    { agent_key_id: selected, window: timeWindow },
    { query: { enabled: Boolean(selected), refetchInterval: usePollingInterval(15_000) } },
  );
  const maxCode = report.codes[0]?.count ?? 1;
  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 xl:grid-cols-4">
        <Distribution title={t.risk.sources} items={report.sources} empty={t.risk.noFindings} />
        <Distribution title={t.risk.modes} items={report.modes} empty={t.risk.noFindings} />
        <Distribution title={t.risk.harms} items={report.harms} empty={t.risk.noFindings} />
        <Distribution
          title={t.risk.severities}
          items={report.severities}
          empty={t.risk.noFindings}
          labelFor={(name) =>
            name ? (t.severities[name as keyof typeof t.severities] ?? name) : '—'
          }
        />
      </div>
      <Card>
        <CardHeader
          title={t.risk.codes}
          description={`${formatNumber(report.total_findings)} ${
            report.total_findings === 1 ? t.risk.totalOne : t.risk.total
          }`}
        />
        <ul className="flex flex-col gap-2 p-3">
          {report.codes.length === 0 && <li className="text-sm text-muted">{t.risk.noFindings}</li>}
          {report.codes.map((code) => (
            <li key={`${code.code}:${code.severity}`}>
              <div className="mb-0.5 flex justify-between gap-2 text-xs">
                <span className="truncate" title={code.code}>
                  {findingLabel(t, code.code)}{' '}
                  <span className="font-mono text-[10px] text-faint">{code.code}</span>
                </span>
                <span className="num shrink-0 text-muted">{formatNumber(code.count)}</span>
              </div>
              <Bar
                value={code.count}
                max={maxCode}
                tone={code.severity === 'high' ? 'danger' : undefined}
              />
            </li>
          ))}
        </ul>
      </Card>
      <Card>
        <CardHeader title={t.risk.agents} />
        <DataTable
          rows={report.agents}
          rowKey={(agent) => agent.agent_key_id}
          columns={[
            {
              key: 'agent',
              header: t.monitor.agent,
              cell: (agent) => (
                <button
                  type="button"
                  aria-pressed={selected === agent.agent_key_id}
                  className="font-mono text-xs hover:text-accent"
                  onClick={() => setAgentKey(agent.agent_key_id)}
                >
                  {agent.label}
                </button>
              ),
            },
            {
              key: 'findings',
              header: t.risk.findings,
              align: 'right',
              className: 'num',
              cell: (agent) => formatNumber(agent.findings),
            },
            {
              key: 'high',
              header: t.risk.high,
              align: 'right',
              className: 'num text-danger',
              cell: (agent) => formatNumber(agent.high),
            },
            {
              key: 'flows',
              header: t.risk.flows,
              align: 'right',
              className: 'num',
              cell: (agent) => formatNumber(agent.flows),
            },
            {
              key: 'last',
              header: t.monitor.lastSeen,
              align: 'right',
              className: 'text-xs text-faint',
              cell: (agent) => formatDate(agent.last_at, locale),
            },
          ]}
        />
      </Card>
      <Card>
        <CardHeader
          title={t.risk.flows}
          description={t.risk.flowsHint}
          actions={
            report.agents.length > 0 ? (
              <Select
                aria-label={t.risk.agentKey}
                className="w-56"
                value={selected}
                onValueChange={setAgentKey}
                options={report.agents.map((agent) => ({
                  value: agent.agent_key_id,
                  label: agent.label,
                }))}
              />
            ) : undefined
          }
        />
        {flows.isLoading ? (
          <Skeleton className="m-4 h-48" />
        ) : flows.isError ? (
          <div role="alert" className="px-4 py-6 text-sm text-danger">
            <p>{errorMessage(t, flows.error)}</p>
            <Button size="sm" className="mt-2" onClick={() => void flows.refetch()}>
              {t.common.retry}
            </Button>
          </div>
        ) : flows.data && flows.data.items.length > 0 ? (
          <DataTable
            rows={flows.data.items}
            rowKey={(flow, index) => `${flow.content_hash}:${index}`}
            columns={[
              {
                key: 'source',
                header: t.risk.source,
                cell: (flow) => (
                  <>
                    <div className="font-mono text-xs">
                      {t.call.origins[flow.source_origin as keyof typeof t.call.origins] ??
                        flow.source_origin}
                    </div>
                    <div className="text-[11px] text-faint">
                      {interpolate(t.call.turnItem, {
                        turn: flow.source_turn,
                        item: flow.source_item,
                      })}
                    </div>
                  </>
                ),
              },
              {
                key: 'relation',
                header: t.risk.relation,
                className: 'font-mono text-xs',
                cell: (flow) =>
                  t.call.relations[flow.relation as keyof typeof t.call.relations] ?? flow.relation,
              },
              {
                key: 'sink',
                header: t.risk.sink,
                cell: (flow) => (
                  <>
                    <div className="font-mono text-xs">
                      {t.call.sinks[flow.sink as keyof typeof t.call.sinks] ?? flow.sink}
                    </div>
                    <div className="text-[11px] text-faint">{flow.sink_tool_call_id}</div>
                  </>
                ),
              },
              {
                key: 'confidence',
                header: t.risk.confidence,
                align: 'right',
                className: 'num text-xs',
                cell: (flow) => `${Math.round(flow.confidence * 100)}%`,
              },
            ]}
          />
        ) : (
          <p className="px-4 py-6 text-sm text-muted">{t.risk.noFlows}</p>
        )}
      </Card>
      <Card>
        <CardHeader title={t.risk.recent} />
        {report.high_recent.length === 0 ? (
          <p className="px-4 py-6 text-sm text-muted">{t.risk.noHighRisk}</p>
        ) : (
          <ul className="divide-y">
            {report.high_recent.map((call) => (
              <li
                key={call.call_id}
                className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm"
              >
                <Link
                  to="/projects/$projectId/conversations/node/$nodeId"
                  params={{ projectId, nodeId: call.node_id || `call:${call.call_id}` }}
                  className="font-mono text-xs text-accent hover:underline"
                >
                  {call.call_id}
                </Link>
                <span className="text-muted">{call.model}</span>
                <span className="ml-auto text-xs text-faint">
                  {formatDate(call.created_at, locale)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

export function RiskPage() {
  const { t } = useLocale();
  const projectId = useProjectId();
  const [timeWindow, setTimeWindow] = useState<GetProjectRiskWindow>('24h');
  const risk = useGetProjectRisk(
    projectId,
    { window: timeWindow },
    { query: { refetchInterval: usePollingInterval(15_000) } },
  );
  return (
    <Page>
      <PageHeader
        title={t.risk.title}
        description={t.risk.description}
        actions={
          <Segmented
            label={t.risk.window}
            value={timeWindow}
            options={windows.map((value) => ({ value, label: t.risk.windows[value] }))}
            onChange={setTimeWindow}
          />
        }
      />
      <p className="text-[11px] text-faint">{t.risk.mainLaneNote}</p>
      <QueryState query={risk} loading={<Skeleton className="h-96" />}>
        {(report) =>
          report.total_findings === 0 && report.agents.length === 0 ? (
            <Card>
              <Empty icon={ShieldAlert} title={t.risk.noFindings} />
            </Card>
          ) : (
            <RiskContent report={report} timeWindow={timeWindow} />
          )
        }
      </QueryState>
    </Page>
  );
}
