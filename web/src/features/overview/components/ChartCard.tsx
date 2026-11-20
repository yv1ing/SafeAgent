import type { ReactNode } from 'react';
import { Card, CardHeader } from '../../../components/ui/card';
import { useLocale } from '../../../i18n';

interface ChartTable {
  columns: string[];
  rows: (string | number)[][];
}

export function ChartCard({
  title,
  legend,
  table,
  children,
}: {
  title: string;
  legend?: { label: string; color: string }[];
  table?: ChartTable;
  children: ReactNode;
}) {
  const { t } = useLocale();
  return (
    <Card className="flex flex-col overflow-visible">
      <CardHeader
        title={title}
        actions={
          legend && (
            <ul className="flex gap-3 text-xs text-muted">
              {legend.map((item) => (
                <li key={item.label} className="flex items-center gap-1.5">
                  <span
                    aria-hidden
                    className="size-2 rounded-full"
                    style={{ background: item.color }}
                  />
                  {item.label}
                </li>
              ))}
            </ul>
          )
        }
      />
      <div className="p-4">{children}</div>
      {table && (
        <details className="border-t px-4 py-2 text-xs">
          <summary className="cursor-pointer text-muted hover:text-ink">
            {t.overview.showTable}
          </summary>
          <div className="scroll-thin mt-2 max-h-60 overflow-auto">
            <table className="w-full text-left">
              <thead>
                <tr>
                  {table.columns.map((column) => (
                    <th key={column} scope="col" className="py-1 pr-3 font-medium text-muted">
                      {column}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="num">
                {table.rows.map((row, index) => (
                  <tr key={index}>
                    {row.map((cell, position) => (
                      <td key={position} className="py-0.5 pr-3">
                        {cell}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}
    </Card>
  );
}
