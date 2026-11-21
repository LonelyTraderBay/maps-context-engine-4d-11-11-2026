import type { ByteRange, SourceRef } from '../.generated/types';
import { checkString } from './json.ts';

export function checkRelativePath(value: string): void {
  checkString(value);
  if (Array.from(value).length > 4096 || /[\\:\p{Cc}]/u.test(value) ||
      value.split('/').some(part => part === '' || part === '.' || part === '..')) {
    throw new Error('Invalid portable source path');
  }
}

export function checkRange(range: ByteRange): void {
  if (!Number.isSafeInteger(range.start_byte) || !Number.isSafeInteger(range.end_byte) ||
      range.start_byte < 0 || range.end_byte < range.start_byte) {
    throw new Error('Invalid source byte range');
  }
}

export function checkSourceRef(ref: SourceRef): void {
  checkRelativePath(ref.path);
  checkRange(ref.range);
}

export function canonicalOrigin(origin: string): string {
  const url = new URL(origin);
  if ((url.protocol !== 'http:' && url.protocol !== 'https:') || url.origin !== origin) {
    throw new Error('Expected canonical HTTP(S) origin');
  }
  return url.origin;
}

export function checkPathname(path: string): void {
  checkString(path);
  if (!path.startsWith('/') || Array.from(path).length > 2048 || /[?#\\\p{Cc}]/u.test(path) ||
      new URL('http://localhost' + path).pathname !== path) {
    throw new Error('Expected browser-canonical pathname');
  }
}

export function pathWithinPrefix(prefix: string, pathname: string): boolean {
  checkPathname(prefix);
  checkPathname(pathname);
  return prefix === '/' || pathname === prefix ||
    pathname.startsWith(prefix.endsWith('/') ? prefix : prefix + '/');
}

// The caller owns permission state and the navigation policy. This check is local and fetch-free.
export function matchPreviewURL(raw: string, approvedOrigin: string, prefix: string): { origin: string; pathname: string } {
  const url = new URL(raw);
  if (/[\\\p{Cc}]/u.test(raw) || url.username || url.password ||
      url.origin !== canonicalOrigin(approvedOrigin) || !pathWithinPrefix(prefix, url.pathname)) {
    throw new Error('Preview URL cannot be matched to its approved origin and path');
  }
  return { origin: url.origin, pathname: url.pathname };
}
