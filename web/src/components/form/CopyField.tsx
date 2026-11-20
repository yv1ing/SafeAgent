import { Check, Copy } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';
import { useLocale } from '../../i18n';
import { Button } from '../ui/button';
import { Input } from '../ui/input';

export function useCopy() {
  const { t } = useLocale();
  const [copied, setCopied] = useState(false);
  const copy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error(t.common.copyFailed);
    }
  };
  return { copied, copy };
}

export function CopyField({ value, label }: { value: string; label: string }) {
  const { t } = useLocale();
  const { copied, copy } = useCopy();
  return (
    <div className="flex gap-2">
      <Input
        readOnly
        aria-label={label}
        value={value}
        className="font-mono"
        onFocus={(event) => event.currentTarget.select()}
      />
      <Button aria-label={t.common.copy} size="icon" onClick={() => void copy(value)}>
        {copied ? <Check aria-hidden className="text-success" /> : <Copy aria-hidden />}
      </Button>
    </div>
  );
}
