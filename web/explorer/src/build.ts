import validate from '../.generated/validators.cjs';
import type { AnchorEntry, BuildManifest } from '../.generated/types';
import { canonicalDigest, compareText, sha256 } from './canonical.ts';
import { parseJSON } from './json.ts';
import { checkRange, checkRelativePath } from './paths.ts';

function normalize(manifest: BuildManifest): BuildManifest {
  const sources = new Map<string, string>();
  for (const source of manifest.sources) {
    checkRelativePath(source.path);
    if (sources.has(source.path)) throw new Error('Duplicate build source');
    sources.set(source.path, source.content_hash);
  }
  const anchors = new Map<string, AnchorEntry>();
  for (const anchor of manifest.anchors) {
    checkRange(anchor.range);
    if (sources.get(anchor.path) !== anchor.content_hash) throw new Error('Anchor source hash mismatch');
    if (anchors.has(anchor.anchor_id)) throw new Error('Duplicate build anchor');
    anchors.set(anchor.anchor_id, anchor);
  }
  const settled = new Set<string>();
  for (const anchor of anchors.values()) {
    const seen = new Set<string>();
    let id: string | null = anchor.anchor_id;
    while (id !== null && !settled.has(id)) {
      if (seen.has(id)) throw new Error('Anchor owner cycle');
      const entry = anchors.get(id);
      if (!entry) throw new Error('Unknown anchor owner');
      seen.add(id);
      id = entry.owner_anchor_id;
    }
    for (const known of seen) settled.add(known);
  }
  return {
    ...manifest,
    sources: [...manifest.sources].sort((a, b) => compareText(a.path, b.path)),
    anchors: [...manifest.anchors].sort((a, b) => compareText(a.path, b.path) ||
      a.range.start_byte - b.range.start_byte || a.range.end_byte - b.range.end_byte ||
      compareText(a.kind, b.kind) || compareText(a.anchor_id, b.anchor_id)),
  };
}

export function decodeBuildManifest(bytes: Uint8Array, limit: number): BuildManifest {
  const value = parseJSON(bytes, limit, true);
  if (!validate.BuildManifest(value)) throw new Error('Invalid build manifest');
  normalize(value);
  return value;
}

export function anchorDigest(anchor: AnchorEntry, provider_digest: string, limit: number): Promise<string> {
  const { path, content_hash, kind, range: { start_byte, end_byte } } = anchor;
  return canonicalDigest('anchor-v1', { path, content_hash, kind, start_byte, end_byte, provider_digest }, limit);
}

export function buildMapDigest(manifest: BuildManifest, limit: number): Promise<string> {
  if (!validate.BuildManifest(manifest)) throw new Error('Invalid build manifest');
  const { build_map_id: _previous, ...identity } = normalize(manifest);
  return canonicalDigest('build-map-v1', identity, limit);
}

export async function verifyBuildIdentity(manifest: BuildManifest, limit: number): Promise<void> {
  if (await buildMapDigest(manifest, limit) !== manifest.build_map_id) throw new Error('Build map digest mismatch');
  for (const anchor of manifest.anchors) {
    if (await anchorDigest(anchor, manifest.provider_digest, limit) !== anchor.anchor_id) {
      throw new Error('Anchor digest mismatch');
    }
  }
}

export async function verifyBuildCaptured(
  manifest: BuildManifest,
  captured: ReadonlyMap<string, Uint8Array<ArrayBuffer>>,
  manifestLimit: number,
  sourceLimit: number,
): Promise<void> {
  if (!Number.isSafeInteger(sourceLimit) || sourceLimit < 1) throw new Error('Invalid source budget');
  await verifyBuildIdentity(manifest, manifestLimit);
  let total = 0;
  const decoder = new TextDecoder('utf-8', { fatal: true });
  for (const source of manifest.sources) {
    const bytes = captured.get(source.path);
    if (!bytes) throw new Error('Missing captured build source');
    if (bytes.length > sourceLimit - total) throw new Error('Captured source byte limit');
    total += bytes.length;
    decoder.decode(bytes);
    if (await sha256(bytes) !== source.content_hash) throw new Error('Captured source hash mismatch');
  }
  for (const { path, range } of manifest.anchors) {
    const bytes = captured.get(path)!;
    const boundary = (offset: number): boolean => offset === bytes.length || (bytes[offset]! & 0xc0) !== 0x80;
    if (range.end_byte > bytes.length || !boundary(range.start_byte) || !boundary(range.end_byte)) {
      throw new Error('Anchor range outside UTF-8 source boundaries');
    }
  }
}

export type BindingIdentity = {
  build_map_id: string;
  source_generation_id: string;
  profile_id: string;
  policy_digest: string;
  resolver_digest: string;
};

export function bindingDigest(binding: BindingIdentity, limit: number): Promise<string> {
  const { build_map_id, source_generation_id, profile_id, policy_digest, resolver_digest } = binding;
  const hashes = [build_map_id, profile_id, policy_digest, resolver_digest];
  if (hashes.some(hash => !/^[a-f0-9]{64}$/.test(hash)) ||
      !/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(source_generation_id)) {
    throw new Error('Invalid binding identity');
  }
  return canonicalDigest('binding-v1',
    { build_map_id, source_generation_id, profile_id, policy_digest, resolver_digest }, limit);
}
