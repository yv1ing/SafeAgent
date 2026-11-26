import * as DialogPrimitive from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import type { ReactNode } from 'react';
import { useLocale } from '../../i18n';
import { cn } from '../../lib/cn';

export function Sheet({
  open,
  onOpenChange,
  title,
  side = 'right',
  showHeader = true,
  className,
  overlayClassName,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  side?: 'left' | 'right';
  showHeader?: boolean;
  className?: string;
  overlayClassName?: string;
  children: ReactNode;
}) {
  const { t } = useLocale();
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay
          className={cn('fixed inset-0 z-40 animate-fade-in bg-overlay', overlayClassName)}
        />
        <DialogPrimitive.Content
          className={cn(
            'fixed inset-y-0 z-50 flex w-full flex-col bg-canvas shadow-pop',
            side === 'left' ? 'animate-slide-in-left border-r' : 'animate-slide-in border-l',
            side === 'left' ? 'left-0' : 'right-0',
            'max-w-3xl',
            className,
          )}
        >
          {showHeader ? (
            <header className="flex items-center justify-between gap-4 border-b bg-surface px-4 py-3">
              <DialogPrimitive.Title className="truncate text-base font-semibold">
                {title}
              </DialogPrimitive.Title>
              <DialogPrimitive.Description className="sr-only">{title}</DialogPrimitive.Description>
              <DialogPrimitive.Close
                aria-label={t.common.close}
                className="rounded-md p-1.5 text-muted transition-colors hover:bg-sunken hover:text-ink"
              >
                <X aria-hidden className="size-4" />
              </DialogPrimitive.Close>
            </header>
          ) : (
            <>
              <DialogPrimitive.Title className="sr-only">{title}</DialogPrimitive.Title>
              <DialogPrimitive.Description className="sr-only">{title}</DialogPrimitive.Description>
            </>
          )}
          <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">{children}</div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
