import type { ReactNode } from 'react';
import { Progress } from '../../../components/ui/progress';

interface RankItem {
  key: string;
  label: ReactNode;
  value: number;
  detail?: string;
}

export function RankList({
  items,
  formatValue,
  empty,
}: {
  items: RankItem[];
  formatValue: (value: number) => string;
  empty: string;
}) {
  if (items.length === 0) return <p className="px-4 py-6 text-sm text-muted">{empty}</p>;
  const maximum = Math.max(...items.map((item) => item.value), 1);
  return (
    <ol className="flex flex-col gap-3 p-4">
      {items.map((item) => (
        <li key={item.key} className="flex flex-col gap-1">
          <div className="flex items-baseline justify-between gap-3 text-sm">
            <span className="min-w-0 truncate">{item.label}</span>
            <span className="num shrink-0 font-medium">
              {formatValue(item.value)}
              {item.detail && (
                <span className="ml-2 text-xs font-normal text-muted">{item.detail}</span>
              )}
            </span>
          </div>
          <Progress value={item.value} max={maximum} />
        </li>
      ))}
    </ol>
  );
}
