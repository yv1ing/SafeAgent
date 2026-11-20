import { forwardRef, useEffect, useState, type InputHTMLAttributes } from 'react';
import { cn } from '../../lib/cn';

export const fieldClass =
  'w-full rounded-md border bg-surface px-2.5 text-sm text-ink placeholder:text-faint transition-colors hover:border-line-strong focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60 aria-invalid:border-danger';

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function Input({ className, ...props }, ref) {
    return <input ref={ref} className={cn(fieldClass, 'h-8', className)} {...props} />;
  },
);

export function IntegerInput({
  value,
  onValueChange,
  min = 0,
  ...props
}: Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange' | 'type'> & {
  value: number;
  onValueChange: (value: number) => void;
}) {
  return (
    <Input
      type="number"
      inputMode="numeric"
      min={min}
      step={1}
      value={value}
      onChange={(event) => onValueChange(Math.trunc(Number(event.target.value)) || 0)}
      className="num"
      {...props}
    />
  );
}

// DecimalInput keeps the text the user is typing. Deriving a controlled value from a number on
// every keystroke turns "2." into "2" and loses the fraction before the next digit arrives.
export function DecimalInput({
  value,
  onValueChange,
  className,
  onFocus,
  onBlur,
  ...props
}: Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange' | 'type'> & {
  value: number;
  onValueChange: (value: number) => void;
}) {
  const [text, setText] = useState(() => String(value));
  const [editing, setEditing] = useState(false);
  useEffect(() => {
    if (!editing) setText(String(value));
  }, [editing, value]);
  return (
    <Input
      type="text"
      inputMode="decimal"
      value={text}
      onFocus={(event) => {
        setEditing(true);
        onFocus?.(event);
      }}
      onChange={(event) => {
        const next = event.target.value;
        setText(next);
        if (next.trim() === '') {
          onValueChange(0);
          return;
        }
        const number = Number(next);
        if (Number.isFinite(number)) onValueChange(number);
      }}
      onBlur={(event) => {
        setEditing(false);
        setText(String(value));
        onBlur?.(event);
      }}
      className={cn('num', className)}
      {...props}
    />
  );
}
