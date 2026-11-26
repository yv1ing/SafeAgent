import type { ConsoleCatalogModelInputKind } from '../../api/generated/models';

export interface DiscoveryDefinition {
  Path?: string;
  Shape?: string;
}

export interface AuxiliaryDefinition {
  Kind?: string;
  Path?: string;
}

export interface ModelAliasDefinition {
  Prefix?: string;
  To?: string;
}

// The endpoint catalog is stored exactly as the gateway decodes it, so the console edits the same
// Go field names instead of translating between two representations.
export interface EndpointDefinition {
  Format?: string;
  BaseURL?: string;
  Path?: string;
  CountTokensPath?: string;
  Auth?: string;
  StaticHeaders?: Record<string, string>;
  Capability?: unknown;
  Compat?: unknown;
  Discovery?: DiscoveryDefinition;
  Auxiliary?: AuxiliaryDefinition[];
  Transport?: string;
  Aliases?: ModelAliasDefinition[];
}

export type EndpointCatalog = Record<string, EndpointDefinition>;

export const formatOrder = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'google_gemini',
];

export type EndpointProblem = 'json' | 'catalog' | 'empty' | 'format' | 'endpoint' | 'path';

export interface EndpointParse {
  catalog?: EndpointCatalog;
  problem?: EndpointProblem;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function formatRank(format: string) {
  const index = formatOrder.indexOf(format);
  return index === -1 ? formatOrder.length : index;
}

export function sortFormats(formats: readonly string[]) {
  return [...formats].sort(
    (left, right) => formatRank(left) - formatRank(right) || left.localeCompare(right),
  );
}

export function parseEndpointCatalog(raw: unknown): EndpointCatalog | null {
  let value = raw;
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  if (!isRecord(value)) return null;
  const catalog: EndpointCatalog = {};
  for (const [format, endpoint] of Object.entries(value)) {
    if (format.trim() === '' || !isRecord(endpoint)) return null;
    catalog[format] = endpoint as EndpointDefinition;
  }
  return Object.keys(catalog).length > 0 ? catalog : null;
}

export function providerFormats(catalog: EndpointCatalog) {
  return sortFormats(Object.keys(catalog));
}

export function endpointSupportsKind(endpoint: EndpointDefinition, kind: string) {
  if (kind === 'generation') return true;
  return (endpoint.Auxiliary ?? []).some((auxiliary) => auxiliary.Kind === kind);
}

export function formatsForKind(
  catalog: EndpointCatalog,
  kind: ConsoleCatalogModelInputKind | string,
) {
  return providerFormats(catalog).filter((format) => endpointSupportsKind(catalog[format]!, kind));
}

export function discoveryFormats(catalog: EndpointCatalog) {
  return providerFormats(catalog).filter(
    (format) => (catalog[format]?.Discovery?.Path ?? '') !== '',
  );
}

export function catalogNeedsHost(catalog: EndpointCatalog) {
  return providerFormats(catalog).some((format) => (catalog[format]?.BaseURL ?? '').trim() === '');
}

export function endpointHosts(catalog: EndpointCatalog) {
  const hosts = new Set<string>();
  for (const format of providerFormats(catalog)) {
    const base = (catalog[format]?.BaseURL ?? '').trim();
    if (base !== '') hosts.add(base);
  }
  return [...hosts];
}

function validBaseURL(value: string) {
  try {
    const parsed = new URL(value);
    return (
      (parsed.protocol === 'http:' || parsed.protocol === 'https:') &&
      parsed.hostname !== '' &&
      parsed.username === '' &&
      parsed.password === '' &&
      parsed.search === '' &&
      parsed.hash === ''
    );
  } catch {
    return false;
  }
}

function validPath(value: string) {
  return value.startsWith('/') && !/[\s]/.test(value);
}

export function validateEndpointCatalog(text: string): EndpointParse {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    return { problem: 'json' };
  }
  if (!isRecord(value)) return { problem: 'catalog' };
  const entries = Object.entries(value);
  if (entries.length === 0) return { problem: 'empty' };
  for (const [format, raw] of entries) {
    if (format.trim() === '') return { problem: 'format' };
    if (!isRecord(raw)) return { problem: 'endpoint' };
    const endpoint = raw as EndpointDefinition;
    if (endpoint.Format !== undefined && endpoint.Format !== '' && endpoint.Format !== format) {
      return { problem: 'format' };
    }
    if (
      endpoint.BaseURL !== undefined &&
      endpoint.BaseURL !== '' &&
      !validBaseURL(endpoint.BaseURL)
    ) {
      return { problem: 'endpoint' };
    }
    for (const path of [endpoint.Path, endpoint.CountTokensPath]) {
      if (path !== undefined && path !== '' && !validPath(path)) return { problem: 'path' };
    }
  }
  return { catalog: value as EndpointCatalog };
}

export function endpointsToText(raw: unknown): string {
  const catalog = parseEndpointCatalog(raw);
  return JSON.stringify(catalog ?? raw ?? {}, null, 2);
}
