import type { ScopeVector, SourceRef } from '../../.generated/types';
import { utf8 } from '../json.ts';
import { verifyScopeDigest } from '../scope.ts';
import type { ExplorerClient } from './client.ts';

export function sourceKey(ref: SourceRef): string {
  return JSON.stringify([ref.worktree_id, ref.profile_id, ref.generation_id, ref.path, ref.content_hash,
    ref.range.start_byte, ref.range.end_byte]);
}

export async function readPinnedSource(client: Pick<ExplorerClient, 'source'>, scope: ScopeVector, ref: SourceRef,
  signal: AbortSignal): Promise<string> {
  const response = await client.source({ source_ref: ref }, signal);
  if (!response.data || !response.scope || response.scope.vector_digest !== scope.vector_digest ||
      sourceKey(response.data.source_ref) !== sourceKey(ref) ||
      utf8.encode(response.data.content).length !== ref.range.end_byte - ref.range.start_byte) {
    throw new Error('Nguồn trả về không khớp lựa chọn.');
  }
  await verifyScopeDigest(response.scope, 32768);
  signal.throwIfAborted();
  return response.data.content;
}
