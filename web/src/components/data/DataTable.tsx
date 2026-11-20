import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';
import { Skeleton } from '../ui/skeleton';
import { Table, Td, Th, Tr } from '../ui/table';

interface Column<T> {
  key: string;
  header: string;
  cell: (row: T) => ReactNode;
  className?: string;
}

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  empty,
}: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  empty: ReactNode;
}) {
  if (rows.length === 0) return <>{empty}</>;
  return (
    <Table>
      <thead>
        <tr>
          {columns.map((column) => (
            <Th key={column.key} className={column.className}>
              {column.header}
            </Th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <Tr key={rowKey(row)}>
            {columns.map((column) => (
              <Td key={column.key} className={column.className}>
                {column.cell(row)}
              </Td>
            ))}
          </Tr>
        ))}
      </tbody>
    </Table>
  );
}

export function TableSkeleton({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-3 p-4', className)}>
      <Skeleton className="h-5 w-1/3" />
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className="h-8" />
      ))}
    </div>
  );
}
