import { Plus, X } from 'lucide-react';
import type { ConsoleChannelModelView } from '../../../api/generated/models';
import { Button } from '../../../components/ui/button';
import { DecimalInput, Input } from '../../../components/ui/input';
import { Table, Td, Th } from '../../../components/ui/table';
import { useLocale } from '../../../i18n';

const priceFields = [
  'input_price',
  'output_price',
  'cache_read_price',
  'cache_write_price',
] as const;
const microsPerUnit = 1_000_000;

export function ModelPrices({
  models,
  onChange,
}: {
  models: ConsoleChannelModelView[];
  onChange: (models: ConsoleChannelModelView[]) => void;
}) {
  const { t } = useLocale();
  const headers = {
    input_price: t.channels.inputPrice,
    output_price: t.channels.outputPrice,
    cache_read_price: t.channels.cacheReadPrice,
    cache_write_price: t.channels.cacheWritePrice,
  };
  const update = (index: number, patch: Partial<ConsoleChannelModelView>) =>
    onChange(
      models.map((model, position) => (position === index ? { ...model, ...patch } : model)),
    );
  return (
    <div className="flex flex-col gap-2">
      {models.length > 0 && (
        <div className="flex flex-col gap-3 border-b p-4 md:hidden">
          {models.map((model, index) => (
            <div key={index} className="flex flex-col gap-2 rounded-md border p-3">
              <div className="flex items-start gap-2">
                <Input
                  aria-label={t.channels.model}
                  required
                  className="font-mono"
                  value={model.name}
                  onChange={(event) => update(index, { name: event.target.value })}
                />
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={t.channels.removeModel}
                  onClick={() => onChange(models.filter((_, position) => position !== index))}
                >
                  <X aria-hidden />
                </Button>
              </div>
              <div className="grid grid-cols-2 gap-2">
                {priceFields.map((field) => (
                  <label key={field} className="flex flex-col gap-1 text-xs text-muted">
                    {headers[field]}
                    <DecimalInput
                      aria-label={`${model.name} ${headers[field]}`}
                      className="text-right"
                      value={(model[field] ?? 0) / microsPerUnit}
                      onValueChange={(price) =>
                        update(index, {
                          [field]: Math.round(price * microsPerUnit) || 0,
                        })
                      }
                    />
                  </label>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
      {models.length > 0 && (
        <div className="hidden min-w-0 overflow-x-auto [contain:paint] md:block">
          <Table>
            <thead>
              <tr>
                <Th>{t.channels.model}</Th>
                {priceFields.map((field) => (
                  <Th key={field} className="text-right">
                    {headers[field]}
                  </Th>
                ))}
                <Th>
                  <span className="sr-only">{t.common.actions}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {models.map((model, index) => (
                <tr key={index}>
                  <Td>
                    <Input
                      aria-label={t.channels.model}
                      required
                      className="font-mono"
                      value={model.name}
                      onChange={(event) => update(index, { name: event.target.value })}
                    />
                  </Td>
                  {priceFields.map((field) => (
                    <Td key={field}>
                      <DecimalInput
                        aria-label={`${model.name} ${headers[field]}`}
                        className="w-24 text-right"
                        value={(model[field] ?? 0) / microsPerUnit}
                        onValueChange={(price) =>
                          update(index, {
                            [field]: Math.round(price * microsPerUnit) || 0,
                          })
                        }
                      />
                    </Td>
                  ))}
                  <Td>
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={t.channels.removeModel}
                      onClick={() => onChange(models.filter((_, position) => position !== index))}
                    >
                      <X aria-hidden />
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      )}
      <div className="px-4 pb-4">
        <Button size="sm" onClick={() => onChange([...models, { name: '' }])}>
          <Plus aria-hidden />
          {t.channels.addModel}
        </Button>
      </div>
    </div>
  );
}
