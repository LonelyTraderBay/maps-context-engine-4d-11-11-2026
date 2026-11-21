import { visit } from 'jsonc-parser';

export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };

const decoder = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });
export const utf8 = new TextEncoder();
export const maxDepth = 32;
export const maxKeys = 4096;
const integerToken = /^-?(?:0|[1-9][0-9]*)$/;

export function checkString(value: string): void {
  if (!value.isWellFormed()) throw new Error('Invalid Unicode string');
}

export function parseJSON(
  bytes: Uint8Array,
  limit: number,
  identity: boolean | ((path: readonly (string | number)[]) => boolean) = false,
): JsonValue {
  if (!Number.isSafeInteger(limit) || bytes.byteLength === 0 || bytes.byteLength > limit) {
    throw new Error('JSON byte limit');
  }
  const raw = decoder.decode(bytes);
  const containers: Array<Set<string> | null> = [];
  function open(keys: Set<string> | null): void {
    if (containers.length > maxDepth) throw new Error('JSON nesting limit');
    containers.push(keys);
  }
  visit(raw, {
    onObjectBegin: () => open(new Set()),
    onArrayBegin: () => open(null),
    onObjectEnd: () => { containers.pop(); },
    onArrayEnd: () => { containers.pop(); },
    onObjectProperty(key) {
      checkString(key);
      const keys = containers.at(-1);
      if (!keys || keys.has(key)) throw new Error('Duplicate JSON field');
      if (keys.size === maxKeys) throw new Error('JSON field limit');
      keys.add(key);
    },
    onLiteralValue(value: unknown, offset, length, _line, _column, path) {
      if (containers.length > maxDepth) throw new Error('JSON nesting limit');
      if (typeof value === 'string') checkString(value);
      if (typeof value !== 'number') return;
      if (!Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value))) {
        throw new Error('Invalid JSON number');
      }
      if ((typeof identity === 'function' ? identity(path()) : identity) &&
          !integerToken.test(raw.slice(offset, offset + length))) {
        throw new Error('Identity number must be an integer token');
      }
    },
    onError() { throw new Error('Invalid JSON syntax'); },
  }, { disallowComments: true, allowTrailingComma: false, allowEmptyContent: false });
  // The syntax and token checks above limit the possible values of JSON.parse.
  return JSON.parse(raw) as JsonValue;
}
