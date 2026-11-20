import { Check, ChevronRight, Copy } from 'lucide-react';
import { useLocale } from '../../i18n';
import { useCopy } from '../form/CopyField';
import { Button } from '../ui/button';

export function JsonView({ summary, value }: { summary: string; value: unknown }) {
  const { t } = useLocale();
  const { copied, copy } = useCopy();
  const text = JSON.stringify(value, null, 2) ?? 'null';
  return (
    <details className="group rounded-md border bg-surface">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm">
        <ChevronRight
          aria-hidden
          className="size-3.5 text-muted transition-transform group-open:rotate-90"
        />
        <span className="min-w-0 flex-1 truncate">{summary}</span>
      </summary>
      <div className="relative border-t">
        <Button
          size="icon"
          variant="ghost"
          aria-label={t.common.copy}
          className="absolute top-1 right-1"
          onClick={() => void copy(text)}
        >
          {copied ? <Check aria-hidden className="text-success" /> : <Copy aria-hidden />}
        </Button>
        <pre className="scroll-thin max-h-96 overflow-auto p-3 font-mono text-xs">{text}</pre>
      </div>
    </details>
  );
}
