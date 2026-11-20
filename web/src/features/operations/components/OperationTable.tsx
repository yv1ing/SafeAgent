import type { ReactNode } from 'react';
import type { ConsoleOperationView } from '../../../api/generated/models';
import { DataTable } from '../../../components/data/DataTable';
import { useLocale } from '../../../i18n';
import { formatDate, placeholder } from '../../../lib/format';

export function OperationTable({
  rows,
  showProject = false,
  empty,
}: {
  rows: ConsoleOperationView[];
  showProject?: boolean;
  empty: ReactNode;
}) {
  const { t, locale } = useLocale();
  return (
    <DataTable
      rows={rows}
      rowKey={(row) => row.id}
      empty={empty}
      columns={[
        {
          key: 'time',
          header: t.operations.time,
          className: 'whitespace-nowrap text-muted',
          cell: (row) => formatDate(row.created_at, locale),
        },
        {
          key: 'user',
          header: t.operations.user,
          className: 'font-medium',
          cell: (row) => row.username,
        },
        {
          key: 'action',
          header: t.operations.action,
          className: 'font-mono text-xs',
          cell: (row) => row.action,
        },
        ...(showProject
          ? [
              {
                key: 'project',
                header: t.operations.project,
                className: 'font-mono text-xs text-muted',
                cell: (row: ConsoleOperationView) => row.project_id || placeholder,
              },
            ]
          : []),
        {
          key: 'detail',
          header: t.operations.detail,
          className: 'font-mono text-xs text-muted',
          cell: (row) => {
            const detail = Object.entries(row.detail)
              .map(([name, value]) => `${name}=${value}`)
              .join(' ');
            return detail || placeholder;
          },
        },
      ]}
    />
  );
}
