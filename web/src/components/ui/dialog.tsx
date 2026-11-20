import * as DialogPrimitive from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import type { ReactNode } from 'react';
import { useLocale } from '../../i18n';
import { cn } from '../../lib/cn';

export const Dialog = DialogPrimitive.Root;

export function DialogContent({
  title,
  description,
  children,
  footer,
  className,
  dismissible = true,
}: {
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  className?: string;
  dismissible?: boolean;
}) {
  const { t } = useLocale();
  const block = dismissible ? undefined : (event: Event) => event.preventDefault();
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-40 animate-fade-in bg-overlay" />
      <DialogPrimitive.Content
        onEscapeKeyDown={block}
        onInteractOutside={block}
        className={cn(
          'fixed top-1/2 left-1/2 z-50 flex max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 animate-pop-in flex-col rounded-lg border bg-raised shadow-pop',
          className,
        )}
      >
        <header className="flex items-start justify-between gap-4 border-b px-5 py-4">
          <div className="min-w-0">
            <DialogPrimitive.Title className="text-lg font-semibold">{title}</DialogPrimitive.Title>
            <DialogPrimitive.Description
              className={description ? 'mt-0.5 text-sm text-muted' : 'sr-only'}
            >
              {description ?? title}
            </DialogPrimitive.Description>
          </div>
          {dismissible && (
            <DialogPrimitive.Close
              aria-label={t.common.close}
              className="rounded-md p-1.5 text-muted transition-colors hover:bg-sunken hover:text-ink"
            >
              <X aria-hidden className="size-4" />
            </DialogPrimitive.Close>
          )}
        </header>
        <div className="scroll-thin overflow-y-auto px-5 py-4">{children}</div>
        {footer && (
          <footer className="flex justify-end gap-2 border-t bg-sunken/40 px-5 py-3">
            {footer}
          </footer>
        )}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}
