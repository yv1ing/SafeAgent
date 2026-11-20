export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

type UnauthorizedHandler = (code: 'unauthenticated' | 'password_change_required') => void;

let unauthorizedHandler: UnauthorizedHandler = () => undefined;

export function onUnauthorized(handler: UnauthorizedHandler) {
  unauthorizedHandler = handler;
}

const sessionPath = '/console/api/session';

export async function request<T>(url: string, options: RequestInit = {}): Promise<T> {
  const method = (options.method ?? 'GET').toUpperCase();
  const headers = new Headers(options.headers);
  if (method !== 'GET') headers.set('X-SafeAgent-Request', '1');
  if (options.body !== undefined) headers.set('Content-Type', 'application/json');
  const response = await fetch(url, { ...options, headers, credentials: 'same-origin' });
  if (response.status === 204) return undefined as T;
  const body = await response.json().catch(() => null);
  if (response.ok) return body as T;
  const code: string = body?.code ?? 'internal_error';
  const error = new ApiError(response.status, code, body?.message ?? response.statusText);
  const loggingIn = url === sessionPath && method === 'POST';
  if (response.status === 401 && !loggingIn) unauthorizedHandler('unauthenticated');
  if (code === 'password_change_required') unauthorizedHandler(code);
  throw error;
}

export function errorCode(error: unknown) {
  return error instanceof ApiError ? error.code : 'network_error';
}
