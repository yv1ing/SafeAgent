import { Plus, X } from 'lucide-react';
import { Button } from '../../../components/ui/button';
import { Input } from '../../../components/ui/input';
import { useLocale } from '../../../i18n';

export type HeaderRow = [name: string, value: string];

export function HeaderEditor({
  rows,
  onChange,
}: {
  rows: HeaderRow[];
  onChange: (rows: HeaderRow[]) => void;
}) {
  const { t } = useLocale();
  const update = (index: number, row: HeaderRow) =>
    onChange(rows.map((current, position) => (position === index ? row : current)));
  return (
    <div className="flex flex-col gap-2 p-4">
      {rows.map(([name, value], index) => (
        <div key={index} className="flex gap-2">
          <Input
            aria-label={t.channels.headerName}
            placeholder={t.channels.headerName}
            className="w-1/3 font-mono"
            value={name}
            onChange={(event) => update(index, [event.target.value, value])}
          />
          <Input
            aria-label={t.channels.headerValue}
            placeholder={t.channels.headerValue}
            className="font-mono"
            value={value}
            onChange={(event) => update(index, [name, event.target.value])}
          />
          <Button
            size="icon"
            variant="ghost"
            aria-label={t.channels.removeHeader}
            onClick={() => onChange(rows.filter((_, position) => position !== index))}
          >
            <X aria-hidden />
          </Button>
        </div>
      ))}
      <div>
        <Button size="sm" onClick={() => onChange([...rows, ['', '']])}>
          <Plus aria-hidden />
          {t.channels.addHeader}
        </Button>
      </div>
    </div>
  );
}
