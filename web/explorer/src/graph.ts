import type { GraphData, GraphRef, ScopeVector } from '../.generated/types';
import { canonicalDigest, compareText } from './canonical.ts';
import { normalizeScope } from './scope.ts';

export function graphRefDigest(ref: GraphRef, limit: number): Promise<string> {
  return canonicalDigest('graph-ref-v1', ref, limit);
}

export function subgraphDigest(
  scope: ScopeVector, nodes: GraphData['nodes'], edges: GraphData['edges'], limit: number,
): Promise<string> {
  return canonicalDigest('subgraph-v1', {
    scope: normalizeScope(scope, limit),
    nodes: [...nodes].sort((a, b) => compareText(a.node_id, b.node_id)),
    edges: [...edges].sort((a, b) => compareText(a.edge_id, b.edge_id)),
  }, limit);
}
