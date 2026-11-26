export interface ItemReveal {
  callId: string;
  section: string;
  ordinal: number;
  valueId?: string;
}

export function itemAnchorId(item: { call_id: string; section: string; ordinal: number }) {
  return `transcript-item-${item.call_id}-${item.section}-${item.ordinal}`;
}
