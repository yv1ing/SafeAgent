import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';
import { Skeleton } from '../ui/skeleton';
import { Table, Td, Th, Tr } from '../ui/table';

export interface DataTableColumn<T> {
  key: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  width?: string;
  align?: 'left' | 'right' | 'center';
  className?: string;
}

const alignClass = {
  left: 'text-left',
  right: 'text-right',
  center: 'text-center',
} as const;

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  loading = false,
  skeletonRows = 5,
  empty,
  onRowClick,
  selectedRowId,
  stickyHeader = false,
  ariaLabel,
}: {
  columns: DataTableColumn<T>[];
  rows: T[];
  rowKey: (row: T, index: number) => string;
  loading?: boolean;
  skeletonRows?: number;
  empty?: ReactNode;
  onRowClick?: (row: T) => void;
  selectedRowId?: string;
  stickyHeader?: boolean;
  ariaLabel?: string;
}) {
  if (loading) return <TableSkeleton rows={skeletonRows} />;
  if (rows.length === 0) return <>{empty}</>;
  const handleRowClick = onRowClick;
  const interactive = Boolean(handleRowClick);
  return (
    <Table aria-label={ariaLabel}>
      <thead>
        <tr>
          {columns.map((column) => (
            <Th
              key={column.key}
              style={column.width ? { width: column.width } : undefined}
              className={cn(
                alignClass[column.align ?? 'left'],
                stickyHeader && 'sticky top-0 z-10 bg-sunken',
              )}
            >
              {column.header}
            </Th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, index) => {
          const key = rowKey(row, index);
          const selected = selectedRowId !== undefined && selectedRowId === key;
          return (
            <Tr
              key={key}
              aria-selected={selected || undefined}
              tabIndex={interactive ? 0 : undefined}
              onClick={handleRowClick ? () => handleRowClick(row) : undefined}
              onKeyDown={
                handleRowClick
                  ? (event) => {
                      if (event.key !== 'Enter' && event.key !== ' ') return;
                      event.preventDefault();
                      handleRowClick(row);
                    }
                  : undefined
              }
              className={cn(
                interactive && 'cursor-pointer',
                selected && 'bg-accent-soft hover:bg-accent-soft',
              )}
            >
              {columns.map((column) => (
                <Td
                  key={column.key}
                  className={cn(alignClass[column.align ?? 'left'], column.className)}
                >
                  {column.cell(row)}
                </Td>
              ))}
            </Tr>
          );
        })}
      </tbody>
    </Table>
  );
}

export function TableSkeleton({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div aria-hidden className={cn('flex flex-col gap-3 p-4', className)}>
      <Skeleton className="h-5 w-1/3" />
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className="h-8" />
      ))}
    </div>
  );
}
