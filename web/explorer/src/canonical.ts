import { checkString, maxDepth, maxKeys, utf8 } from './json.ts';
import type { JsonValue } from './json.ts';

export function compareBytes(a: Uint8Array, b: Uint8Array): number {
  const common = Math.min(a.length, b.length);
  for (let i = 0; i < common; i++) {
    const diff = a[i]! - b[i]!;
    if (diff !== 0) return diff;
  }
  return a.length - b.length;
}

export function compareText(a: string, b: string): number {
  checkString(a);
  checkString(b);
  return compareBytes(utf8.encode(a), utf8.encode(b));
}

export function compareNullable(a: string | null, b: string | null): number {
  if (a === null) return b === null ? 0 : -1;
  return b === null ? 1 : compareText(a, b);
}

function encode(values: JsonValue[], limit: number, domainPrefix = ''): Uint8Array<ArrayBuffer> {
  if (!Number.isSafeInteger(limit) || limit <= 0) throw new Error('Invalid codec budget');
  const chunks: Uint8Array[] = [];
  let size = 0;
  const append = (part: Uint8Array): void => {
    if (part.length > limit - size) throw new Error('Canonical byte limit');
    chunks.push(part);
    size += part.length;
  };
  const ascii = (text: string): void => append(utf8.encode(text));
  function string(value: string): void {
    checkString(value);
    if (value.length > limit - size) throw new Error('Canonical byte limit');
    const bytes = utf8.encode(value);
    ascii(`s${bytes.length}:`);
    append(bytes);
  }
  function item(value: JsonValue, depth: number): void {
    if (depth > maxDepth) throw new Error('Canonical nesting limit');
    if (value === null) ascii('n');
    else if (typeof value === 'boolean') ascii(value ? 't' : 'f');
    else if (typeof value === 'number') {
      if (!Number.isSafeInteger(value)) throw new Error('Invalid identity integer');
      ascii(`i${value};`);
    } else if (typeof value === 'string') {
      string(value);
    } else if (Array.isArray(value)) {
      ascii(`a${value.length}:`);
      for (const child of value) item(child, depth + 1);
    } else {
      const keys = Object.keys(value);
      if (keys.length > maxKeys) throw new Error('Canonical field limit');
      keys.sort(compareText);
      ascii(`o${keys.length}:`);
      for (const key of keys) {
        string(key);
        item(value[key]!, depth + 1);
      }
    }
  }
  if (domainPrefix) ascii(domainPrefix);
  for (const value of values) item(value, 0);
  const output = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    output.set(chunk, offset);
    offset += chunk.length;
  }
  return output;
}

export function canonicalBytes(value: JsonValue, limit: number): Uint8Array<ArrayBuffer> {
  return encode([value], limit);
}

export async function sha256(bytes: Uint8Array<ArrayBuffer>): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
}

export function canonicalDigest(domain: string, value: JsonValue, limit: number): Promise<string> {
  if (domain === '') throw new Error('Canonical domain is empty');
  return sha256(encode([domain, value], limit, 'MCE4D\0C14N1\0'));
}
