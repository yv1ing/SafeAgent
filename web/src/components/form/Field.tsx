import { cloneElement, isValidElement, useId, type ReactNode } from 'react';
import { Label } from '../ui/label';

export function Field({
  label,
  hint,
  children,
  className,
}: {
  label: string;
  hint?: string;
  children: (id: string) => ReactNode;
  className?: string;
}) {
  const id = useId();
  const hintId = useId();
  const control = children(id);
  const described =
    hint && isValidElement(control)
      ? cloneElement(control, { 'aria-describedby': hintId } as Record<string, string>)
      : control;
  return (
    <div className={className}>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={id}>{label}</Label>
        {described}
      </div>
      {hint && (
        <p id={hintId} className="mt-1 text-xs text-faint">
          {hint}
        </p>
      )}
    </div>
  );
}
