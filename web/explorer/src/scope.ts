import validate from '../.generated/validators.cjs';
import type { ScopeMember, ScopeRequest, ScopeVector } from '../.generated/types';
import { canonicalBytes, canonicalDigest, compareBytes, compareNullable, compareText } from './canonical.ts';

function compareMembers(a: ScopeMember, b: ScopeMember): number {
  return compareNullable(a.service_id, b.service_id) || compareText(a.worktree_id, b.worktree_id) ||
    compareText(a.profile_id, b.profile_id) || compareText(a.generation_id, b.generation_id);
}

export function checkScopeVector(scope: ScopeVector): void {
  if (scope.kind === 'single') {
    if (scope.group_id !== null || scope.group_revision !== null || scope.members.length !== 1) {
      throw new Error('Invalid single scope');
    }
  } else if (scope.group_id === null || scope.group_revision === null) {
    throw new Error('Invalid group scope');
  }
}

export function scopeSelection(scope: ScopeRequest, explicitBindings: number): 'pinned' | 'group' | 'single' | 'bound' {
  const single = scope.worktree_id !== undefined || scope.profile_id !== undefined || scope.generation_id !== undefined;
  if (scope.pinned_vector !== undefined) {
    if (single || scope.group_id !== undefined) throw new Error('Mixed pinned scope and live selectors');
    checkScopeVector(scope.pinned_vector);
    return 'pinned';
  }
  if (scope.group_id !== undefined) {
    if (single) throw new Error('Mixed single and group selectors');
    return 'group';
  }
  if (single) {
    if (scope.worktree_id === undefined || scope.profile_id === undefined) throw new Error('Incomplete single scope');
    return 'single';
  }
  if (explicitBindings !== 1) throw new Error('No unique explicit binding');
  return 'bound';
}

export function normalizeScope(scope: ScopeVector, limit: number): ScopeVector {
  if (!validate.ScopeVector(scope)) throw new Error('Invalid scope vector');
  checkScopeVector(scope);
  const members = [...scope.members].sort(compareMembers);
  for (let index = 1; index < members.length; index++) {
    const previous = members[index - 1]!;
    const current = members[index]!;
    if (compareMembers(previous, current) === 0 &&
        compareBytes(canonicalBytes(previous, limit), canonicalBytes(current, limit)) !== 0) {
      throw new Error('Conflicting resolved member identity');
    }
  }
  const unavailable = [...scope.unavailable_members].sort((a, b) =>
    compareNullable(a.service_id, b.service_id) || compareNullable(a.worktree_id, b.worktree_id) ||
    compareText(a.status, b.status) || compareBytes(canonicalBytes(a, limit), canonicalBytes(b, limit)));
  return { ...scope, members, unavailable_members: unavailable };
}

export function scopeDigest(scope: ScopeVector, limit: number): Promise<string> {
  const { vector_digest: _previous, ...identity } = normalizeScope(scope, limit);
  return canonicalDigest('scope-v1', identity, limit);
}

export async function verifyScopeDigest(scope: ScopeVector, limit: number): Promise<void> {
  if (await scopeDigest(scope, limit) !== scope.vector_digest) throw new Error('Scope digest mismatch');
}
