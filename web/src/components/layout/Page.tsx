import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

export function Page({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('flex flex-col gap-5', className)}>{children}</div>;
}
