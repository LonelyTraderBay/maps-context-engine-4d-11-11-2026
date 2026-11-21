import { decodeHTTPRequest, decodeHTTPResponse, explorerBasePath, httpLimits } from '../http.ts';
import type { HTTPRequest, HTTPResponse } from '../http.ts';
import { utf8 } from '../json.ts';

type Route = 'POST /session/bootstrap' | 'GET /workspace' | 'POST /graph/query' | 'POST /source/read';
type Body<R extends Route> = R extends 'GET /workspace' ? undefined : HTTPRequest<R>;

export class ExplorerRequestError extends Error {
  constructor(readonly code: string, message: string, readonly status: number) {
    super(message);
  }
}

async function responseBytes(response: Response, limit: number): Promise<Uint8Array> {
  if (!response.body || !/^application\/json(?:;|$)/i.test(response.headers.get('Content-Type') ?? '')) {
    throw new Error('API Explorer trả phản hồi không hợp lệ.');
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.length;
      if (size > limit) {
        await reader.cancel();
        throw new Error('Phản hồi vượt giới hạn Explorer.');
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.length;
  }
  return bytes;
}

export function createExplorerClient() {
  let session: { bearer: string; expires: number; deadline: number } | undefined;

  async function exchange<R extends Route>(route: R, input: Body<R>, signal: AbortSignal): Promise<HTTPResponse<R>> {
    const method = route.startsWith('GET ') ? 'GET' : 'POST';
    const pathname = route.slice(route.indexOf(' ') + 1);
    const headers = new Headers({ Accept: 'application/json' });
    if (route !== 'POST /session/bootstrap') {
      if (!session || Date.now() >= session.expires || performance.now() >= session.deadline) {
        session = undefined;
        throw new ExplorerRequestError('AUTH_EXPIRED', 'Phiên Explorer đã hết hạn.', 401);
      }
      headers.set('Authorization', 'Bearer ' + session.bearer);
    }
    const options: RequestInit = {
      method, headers, signal, cache: 'no-store', credentials: 'omit', mode: 'same-origin',
      redirect: 'error', referrerPolicy: 'no-referrer',
    };
    if (method === 'POST') {
      // Validate the same serialized bytes that go on the wire.
      const body = JSON.stringify(input);
      if (route === 'GET /workspace') throw new Error('Invalid Explorer method');
      decodeHTTPRequest(route, utf8.encode(body), 1);
      headers.set('Content-Type', 'application/json');
      headers.set('X-MCE4D-Intent', 'explorer');
      options.body = body;
    }
    const reply = await fetch(explorerBasePath + pathname + (method === 'GET' ? '?verify=none' : ''), options);
    const raw = await responseBytes(reply, httpLimits(route).responseBytes);
    const result = decodeHTTPResponse(route, raw);
    if (result.budget.serialized_bytes !== raw.length) throw new Error('Số byte phản hồi không khớp.');
    if (result.error || !reply.ok || !result.data) {
      if (reply.status === 401) session = undefined;
      throw new ExplorerRequestError(result.error?.code ?? 'INTERNAL_ERROR',
        result.error?.message ?? 'Explorer chưa trả dữ liệu.', reply.status);
    }
    return result;
  }

  return {
    async connect(code: string, signal: AbortSignal): Promise<void> {
      const response = await exchange('POST /session/bootstrap', { code }, signal);
      const data = response.data;
      if (!data || !/^[A-Za-z0-9_-]{43}$/.test(data.bearer)) throw new Error('Bearer Explorer không hợp lệ.');
      const expires = Date.parse(data.expires_at);
      const remaining = Math.min(30 * 60_000, expires - Date.now());
      if (!Number.isFinite(remaining) || remaining <= 0) throw new Error('Phiên Explorer đã hết hạn.');
      signal.throwIfAborted();
      session = { bearer: data.bearer, expires, deadline: performance.now() + remaining };
    },
    workspace: (signal: AbortSignal) => exchange('GET /workspace', undefined, signal),
    graph: (request: HTTPRequest<'POST /graph/query'>, signal: AbortSignal) => exchange('POST /graph/query', request, signal),
    source: (request: HTTPRequest<'POST /source/read'>, signal: AbortSignal) => exchange('POST /source/read', request, signal),
    disconnect: () => { session = undefined; },
  };
}

export type ExplorerClient = ReturnType<typeof createExplorerClient>;
