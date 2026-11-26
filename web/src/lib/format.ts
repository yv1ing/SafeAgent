import type { Tone } from '../components/ui/badge';

export const placeholder = '—';

export function formatNumber(value: number) {
  return value.toLocaleString('en-US');
}

export function formatLimit(value: number | undefined, unlimited: string) {
  return value ? formatNumber(value) : unlimited;
}

export function formatTokens(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return String(value);
}

export function formatPercent(part: number, whole: number) {
  if (whole === 0) return placeholder;
  return `${((part / whole) * 100).toFixed(1)}%`;
}

const currencySymbols: Record<string, string> = {
  USD: '$',
  CNY: '¥',
  EUR: '€',
  JPY: '¥',
  GBP: '£',
};

const microsPerUnit = 1_000_000;

export function priceToMicros(units: number) {
  return Math.round(units * microsPerUnit) || 0;
}

export function priceFromMicros(micros: number | null | undefined) {
  return (micros ?? 0) / microsPerUnit;
}

export function formatCost(micros: number | null | undefined, currency = 'USD') {
  if (micros === undefined || micros === null) return placeholder;
  const amount = priceFromMicros(micros).toFixed(4);
  const symbol = currencySymbols[currency || 'USD'];
  return symbol ? `${symbol}${amount}` : `${amount} ${currency}`;
}

export function formatDuration(milliseconds: number | undefined) {
  if (!milliseconds) return placeholder;
  if (milliseconds < 1000) return `${milliseconds} ms`;
  return `${(milliseconds / 1000).toFixed(1)} s`;
}

export function formatDate(value: string | undefined, locale: string) {
  if (!value) return placeholder;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(locale, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

export function formatBucket(value: string, locale: string, withDate: boolean) {
  const date = new Date(value);
  return date.toLocaleString(locale, {
    ...(withDate ? { month: 'short', day: 'numeric' } : {}),
    ...(withDate && value.endsWith('T00:00:00Z') ? {} : { hour: '2-digit', minute: '2-digit' }),
  });
}

export function shortId(id: string) {
  return id.length > 12 ? `${id.slice(0, 12)}…` : id;
}

export function stateTone(state: string | undefined): Tone {
  switch (state) {
    case 'completed':
      return 'success';
    case 'running':
      return 'info';
    case 'failed':
    case 'cancelled':
    case 'interrupted':
      return 'danger';
    default:
      return 'neutral';
  }
}

export function severityTone(severity: string | undefined): Tone {
  switch (severity) {
    case 'high':
      return 'danger';
    case 'medium':
      return 'warning';
    case 'info':
      return 'info';
    default:
      return 'neutral';
  }
}
