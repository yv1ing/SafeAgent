import { Search, X } from 'lucide-react';
import type { ReactNode } from 'react';
import { useLocale } from '../../i18n';
import { cn } from '../../lib/cn';
import { slashFocusAttribute } from '../../lib/shortcuts';
import { Badge } from '../ui/badge';
import { Button } from '../ui/button';
import { Input } from '../ui/input';
import { Kbd } from '../ui/kbd';
import { Select } from '../ui/select';

export interface FilterSelect {
  key: string;
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
  width?: string;
}

export interface FilterChip {
  key: string;
  label: ReactNode;
  removeLabel: string;
  onRemove: () => void;
}

export function FilterBar({
  searchLabel,
  searchPlaceholder,
  search,
  onSearchChange,
  searchClassName,
  selects = [],
  chips = [],
  onClear,
  actions,
  className,
}: {
  searchLabel: string;
  searchPlaceholder?: string;
  search?: string;
  onSearchChange?: (value: string) => void;
  searchClassName?: string;
  selects?: FilterSelect[];
  chips?: FilterChip[];
  onClear?: () => void;
  actions?: ReactNode;
  className?: string;
}) {
  const { t } = useLocale();
  const filtered =
    (search?.trim() ?? '') !== '' ||
    chips.length > 0 ||
    selects.some((select) => select.value !== '');
  return (
    <div
      role="search"
      aria-label={searchLabel}
      className={cn('flex flex-wrap items-center gap-2', className)}
    >
      {onSearchChange && (
        <div className={cn('relative w-full sm:w-60', searchClassName)}>
          <Search
            aria-hidden
            className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint"
          />
          <Input
            {...{ [slashFocusAttribute]: '' }}
            aria-label={searchLabel}
            placeholder={searchPlaceholder}
            className="pr-8 pl-8"
            value={search ?? ''}
            onChange={(event) => onSearchChange(event.target.value)}
          />
          <Kbd className="absolute top-1.5 right-2">/</Kbd>
        </div>
      )}
      {selects.map((select) => (
        <Select
          key={select.key}
          aria-label={select.label}
          className={select.width}
          value={select.value}
          options={select.options}
          onValueChange={select.onChange}
        />
      ))}
      {chips.map((chip) => (
        <Badge key={chip.key} tone="accent" className="h-7 gap-1.5 pr-0.5">
          {chip.label}
          <button
            type="button"
            aria-label={chip.removeLabel}
            className="rounded-sm p-0.5 hover:bg-accent/15"
            onClick={chip.onRemove}
          >
            <X aria-hidden />
          </button>
        </Badge>
      ))}
      {filtered && onClear && (
        <Button size="sm" variant="ghost" onClick={onClear}>
          {t.common.clearFilters}
        </Button>
      )}
      {actions && <div className="ml-auto flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}
