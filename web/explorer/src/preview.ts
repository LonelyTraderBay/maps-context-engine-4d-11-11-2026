import validate from '../.generated/validators.cjs';
import type { PreviewPermissionProfile, PreviewStatusData } from '../.generated/types';
import { canonicalDigest, compareText } from './canonical.ts';
import { parseJSON } from './json.ts';
import { canonicalOrigin, checkPathname } from './paths.ts';

function utcNanos(input: string): bigint {
  const time = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(?:Z|[+-]00:00)$/.exec(input);
  if (!time) throw new Error('Expected UTC timestamp');
  const seconds = Date.parse(time[1] + 'Z');
  if (!Number.isFinite(seconds)) throw new Error('Invalid UTC timestamp');
  const fraction = (time[2] ?? '').padEnd(9, '0');
  return BigInt(seconds) * 1_000_000n + BigInt(fraction);
}

function sortedSet(values: string[]): string[] {
  if (new Set(values).size !== values.length) throw new Error('Duplicate permission entry');
  return [...values].sort(compareText);
}

function normalize(profile: PreviewPermissionProfile): PreviewPermissionProfile {
  canonicalOrigin(profile.approved_origin);
  checkPathname(profile.approved_path_prefix);
  utcNanos(profile.attested_at);
  utcNanos(profile.expires_at);
  return {
    ...profile,
    source_worktree_ids: sortedSet(profile.source_worktree_ids),
    allowed_dev_destinations: sortedSet(profile.allowed_dev_destinations),
  };
}

export function decodePermissionProfile(bytes: Uint8Array, limit: number): PreviewPermissionProfile {
  const value = parseJSON(bytes, limit, true);
  if (!validate.PreviewPermissionProfile(value)) throw new Error('Invalid preview permission profile');
  normalize(value);
  return value;
}

export function permissionDigest(profile: PreviewPermissionProfile, limit: number): Promise<string> {
  if (!validate.PreviewPermissionProfile(profile)) throw new Error('Invalid preview permission profile');
  return canonicalDigest('preview-permission-v1', normalize(profile), limit);
}

export function checkPreviewStatus(status: PreviewStatusData): void {
  const current = utcNanos(status.server_time) < utcNanos(status.expires_at);
  if (status.interaction_allowed && (!current || status.state !== 'bound' ||
      status.binding_id === null || status.permission_status !== 'approved')) {
    throw new Error('Interaction requires current bound permission');
  }
}
