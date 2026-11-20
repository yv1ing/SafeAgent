import { Link } from '@tanstack/react-router';
import { ShieldAlert } from 'lucide-react';
import { useState } from 'react';
import type { ConsoleRiskView, GetProjectRiskWindow } from '../../api/generated/models';
import { useListProjectFlows } from '../../api/generated/flows/flows';
import { useGetProjectRisk } from '../../api/generated/risk/risk';
import { useProjectId } from '../../app/context';
import { Page } from '../../components/layout/Page';
import { usePollingInterval } from '../../hooks/usePolling';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Segmented } from '../../components/feedback/Segmented';
import { Card, CardHeader } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Select } from '../../components/ui/select';
import { Skeleton } from '../../components/ui/skeleton';
import { Table, Td, Th, Tr } from '../../components/ui/table';
import { errorMessage, findingLabel, interpolate, useLocale } from '../../i18n';
import { Button } from '../../components/ui/button';
import { formatDate, formatNumber } from '../../lib/format';

const windows: GetProjectRiskWindow[] = ['24h', '7d', '30d'];

function Bar({ value, max, tone }: { value: number; max: number; tone?: string }) {
  const width = max > 0 ? Math.max((value / max) * 100, 2) : 0;
  return (
    <div className="h-1.5 w-full overflow-hidden rounded bg-sunken">
      <div
        className="h-full rounded"
        style={{ width: `${width}%`, background: tone ?? 'var(--chart-1)' }}
      />
    </div>
  );
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
                tone={code.severity === 'high' ? 'var(--danger)' : 'var(--chart-2)'}
              />
            </li>
          ))}
        </ul>
      </Card>
      <Card>
        <CardHeader title={t.risk.agents} />
        <Table>
          <thead>
            <tr>
              <Th>{t.monitor.agent}</Th>
              <Th className="text-right">{t.risk.findings}</Th>
              <Th className="text-right">{t.risk.high}</Th>
              <Th className="text-right">{t.risk.flows}</Th>
              <Th className="text-right">{t.monitor.lastSeen}</Th>
            </tr>
          </thead>
          <tbody>
            {report.agents.map((agent) => (
              <Tr key={agent.agent_key_id}>
                <Td>
                  <button
                    type="button"
                    aria-pressed={selected === agent.agent_key_id}
                    className="font-mono text-xs hover:text-accent"
                    onClick={() => setAgentKey(agent.agent_key_id)}
                  >
                    {agent.label}
                  </button>
                </Td>
                <Td className="num text-right">{formatNumber(agent.findings)}</Td>
                <Td className="num text-right text-danger">{formatNumber(agent.high)}</Td>
                <Td className="num text-right">{formatNumber(agent.flows)}</Td>
                <Td className="text-right text-xs text-faint">
                  {formatDate(agent.last_at, locale)}
                </Td>
              </Tr>
            ))}
          </tbody>
        </Table>
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
          <Table>
            <thead>
              <tr>
                <Th>{t.risk.source}</Th>
                <Th>{t.risk.relation}</Th>
                <Th>{t.risk.sink}</Th>
                <Th className="text-right">{t.risk.confidence}</Th>
              </tr>
            </thead>
            <tbody>
              {flows.data.items.map((flow, index) => (
                <Tr key={`${flow.content_hash}:${index}`} className="align-top">
                  <Td>
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
                  </Td>
                  <Td className="font-mono text-xs">
                    {t.call.relations[flow.relation as keyof typeof t.call.relations] ??
                      flow.relation}
                  </Td>
                  <Td>
                    <div className="font-mono text-xs">
                      {t.call.sinks[flow.sink as keyof typeof t.call.sinks] ?? flow.sink}
                    </div>
                    <div className="text-[11px] text-faint">{flow.sink_tool_call_id}</div>
                  </Td>
                  <Td className="num text-right text-xs">{Math.round(flow.confidence * 100)}%</Td>
                </Tr>
              ))}
            </tbody>
          </Table>
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
                  to="/projects/$projectId/calls"
                  params={{ projectId }}
                  search={{ call: call.call_id }}
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
