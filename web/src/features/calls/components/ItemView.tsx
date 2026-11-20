import type { ConsoleItemView } from '../../../api/generated/models';
import { Badge } from '../../../components/ui/badge';
import { interpolate, useLocale } from '../../../i18n';
import { cn } from '../../../lib/cn';

function Target({ target }: { target: NonNullable<ConsoleItemView['tool_call']>['target'] }) {
  const label = target.value || target.command || target.host || target.kind;
  return <span className="font-mono text-xs text-muted">{label}</span>;
}

export function ItemView({ item }: { item: ConsoleItemView }) {
  const { t } = useLocale();
  if (item.tool_call) {
    return (
      <div className="rounded-md border border-warning/40 bg-warning-soft/40 px-2.5 py-2">
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone="warning">
            {interpolate(t.call.toolCall, { name: item.tool_call.name })}
          </Badge>
          <Target target={item.tool_call.target} />
          {item.tool_call.capabilities.map((capability) => (
            <Badge key={capability} tone="neutral">
              {capability}
            </Badge>
          ))}
        </div>
        {item.tool_call.arguments && (
          <pre className="mt-1.5 overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs text-muted">
            {item.tool_call.arguments}
          </pre>
        )}
      </div>
    );
  }
  if (item.tool_result) {
    return (
      <div
        className={cn(
          'rounded-md border px-2.5 py-2',
          item.tool_result.is_error ? 'border-danger/40 bg-danger-soft/40' : 'bg-sunken/60',
        )}
      >
        <div className="mb-1 flex items-center gap-2 text-xs">
          <Badge tone={item.tool_result.is_error ? 'danger' : 'success'}>
            {item.tool_result.is_error ? t.call.toolError : t.call.toolResult}
          </Badge>
          <span className="font-mono text-muted">{item.tool_result.call_id}</span>
        </div>
        <div className="flex flex-col gap-1.5">
          {item.tool_result.content.map((child, index) => (
            <ItemView key={index} item={child} />
          ))}
        </div>
      </div>
    );
  }
  if (item.media) {
    return (
      <p className="text-xs text-muted">
        {interpolate(t.call.media, { kind: item.media.kind })} · {item.media.mime_type}{' '}
        {item.media.filename}
      </p>
    );
  }
  if (!item.text) {
    return (
      <p className="text-xs text-faint">
        {item.kind === 'native' ? t.call.native : `[${item.kind}]`}
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-1">
      {item.kind === 'reasoning' && (
        <span className="text-[11px] font-medium text-muted">{t.call.reasoning}</span>
      )}
      {item.kind === 'refusal' && (
        <span className="text-[11px] font-medium text-danger">{t.call.refusal}</span>
      )}
      <pre
        className={cn(
          'whitespace-pre-wrap break-words font-sans text-sm',
          item.kind === 'reasoning' && 'text-muted italic',
          item.kind === 'refusal' && 'text-danger',
        )}
      >
        {item.text}
      </pre>
    </div>
  );
}
