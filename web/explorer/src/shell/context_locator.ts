import type { GraphRef, PackRequest, ScopeVector, SourceRef } from '../../.generated/types';

export function selectionLocator(ref: GraphRef, scope: ScopeVector, source?: SourceRef): PackRequest | null {
  const members = scope.members.filter(member => member.worktree_id === ref.worktree_id &&
    member.profile_id === ref.profile_id && member.generation_id === ref.generation_id);
  if (members.length !== 1) return null;
  const member = members[0]!;
  const pinned = { pinned_vector: scope };
  if (ref.kind === 'file') {
    if (scope.kind !== 'single' || scope.members.length !== 1 || scope.unavailable_members.length !== 0 ||
        !source || ref.path !== source.path || ref.content_hash !== source.content_hash ||
        member.worktree_id !== source.worktree_id || member.profile_id !== source.profile_id ||
        member.generation_id !== source.generation_id) return null;
    return { scope: pinned, view: 'symbol', task_kind: 'explore', freshness_mode: 'analysis_snapshot',
      selector: { kind: 'source_position', path: source.path, content_hash: source.content_hash, byte_offset: source.range.start_byte } };
  }
  if (ref.kind !== 'symbol' && ref.kind !== 'entity') return null;
  return { scope: pinned, view: 'symbol', task_kind: 'explore', freshness_mode: 'analysis_snapshot',
    selector: { kind: 'ref', ref, service_id: member.service_id } };
}
