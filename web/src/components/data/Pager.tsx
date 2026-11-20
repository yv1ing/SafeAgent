import { ChevronLeft, ChevronRight } from 'lucide-react';
import { interpolate, useLocale } from '../../i18n';
import { Button } from '../ui/button';

export function Pager({
  page,
  hasPrevious,
  hasNext,
  onPrevious,
  onNext,
}: {
  page: number;
  hasPrevious: boolean;
  hasNext: boolean;
  onPrevious: () => void;
  onNext: () => void;
}) {
  const { t } = useLocale();
  if (!hasPrevious && !hasNext) return null;
  return (
    <nav
      aria-label={t.common.pagination}
      className="flex items-center justify-between gap-2 border-t px-4 py-2.5"
    >
      <span className="text-xs text-muted">{interpolate(t.common.page, { page })}</span>
      <div className="flex gap-1">
        <Button size="sm" disabled={!hasPrevious} onClick={onPrevious}>
          <ChevronLeft aria-hidden />
          {t.common.newer}
        </Button>
        <Button size="sm" disabled={!hasNext} onClick={onNext}>
          {t.common.older}
          <ChevronRight aria-hidden />
        </Button>
      </div>
    </nav>
  );
}
