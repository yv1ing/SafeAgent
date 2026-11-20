import * as SelectPrimitive from '@radix-ui/react-select';
import { Check, ChevronDown } from 'lucide-react';
import { cn } from '../../lib/cn';
import { fieldClass } from './input';

interface SelectOption {
  value: string;
  label: string;
}

// Radix reserves the empty string for "no selection", so an option meaning "none" or "all" travels
// under this sentinel and is translated back before reaching the caller.
const emptyValue = '__empty__';

export function Select({
  value,
  onValueChange,
  options,
  placeholder,
  disabled,
  className,
  id,
  'aria-label': ariaLabel,
}: {
  value: string;
  onValueChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  id?: string;
  'aria-label'?: string;
}) {
  const wire = (raw: string) => (raw === '' ? emptyValue : raw);
  const known = options.some((option) => option.value === value);
  return (
    <SelectPrimitive.Root
      value={known ? wire(value) : undefined}
      onValueChange={(next) => onValueChange(next === emptyValue ? '' : next)}
      disabled={disabled}
    >
      <SelectPrimitive.Trigger
        id={id}
        aria-label={ariaLabel}
        className={cn(
          fieldClass,
          'inline-flex h-8 items-center justify-between gap-2 text-left',
          className,
        )}
      >
        <span className="truncate">
          <SelectPrimitive.Value placeholder={placeholder} />
        </span>
        <SelectPrimitive.Icon>
          <ChevronDown aria-hidden className="size-3.5 text-muted" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={4}
          className="z-50 max-h-72 min-w-(--radix-select-trigger-width) animate-pop-in overflow-hidden rounded-lg border bg-raised shadow-pop"
        >
          <SelectPrimitive.Viewport className="p-1">
            {options.map((option) => (
              <SelectPrimitive.Item
                key={option.value}
                value={wire(option.value)}
                className="relative flex cursor-pointer items-center rounded-md py-1.5 pr-2 pl-7 text-sm outline-none data-highlighted:bg-sunken"
              >
                <SelectPrimitive.ItemIndicator className="absolute left-2">
                  <Check aria-hidden className="size-3.5 text-accent" />
                </SelectPrimitive.ItemIndicator>
                <SelectPrimitive.ItemText>{option.label}</SelectPrimitive.ItemText>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}
