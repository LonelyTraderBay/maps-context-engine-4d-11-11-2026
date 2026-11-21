import type { Coverage, Gap, GraphEdge, GraphNode, ScopeVector } from '../index.ts';
import { subgraphDigest, verifyScopeDigest } from '../index.ts';
import type { ExplorerClient } from '../api/client.ts';

export const defaultNodes = 200;
export const defaultEdges = 500;
const hardNodes = 500;
const hardEdges = 1500;

export type GraphSlice = {
  nodes: GraphNode[];
  edges: GraphEdge[];
  coverage: Coverage | null;
  warnings: Gap[];
  cursor: string | null;
  viewportFull: boolean;
  pendingNodes: number | null;
  pendingEdges: number | null;
};

export const emptyGraph: GraphSlice = {
  nodes: [], edges: [], coverage: null, warnings: [], cursor: null, viewportFull: false,
  pendingNodes: 0, pendingEdges: 0,
};

export async function loadGraph(
  client: ExplorerClient,
  scope: ScopeVector,
  signal: AbortSignal,
  previous: GraphSlice = emptyGraph,
  publishPage?: (slice: GraphSlice) => void,
): Promise<GraphSlice> {
  let result = previous;
  const initial = previous === emptyGraph;
  const maxNodes = initial ? defaultNodes : hardNodes;
  const maxEdges = initial ? defaultEdges : hardEdges;
  const cursors = new Set(previous.cursor === null ? [] : [previous.cursor]);
  // The service pages nodes first and then edges. A small initial slice can
  // show both without consuming an unbounded graph.
  for (let attempt = 0; attempt < (initial ? 3 : 1); attempt++) {
    const page = await client.graph({
      root: { kind: 'workspace' }, scope: { pinned_vector: scope },
      node_limit: Math.min(defaultNodes, Math.max(2, maxNodes - result.nodes.length)),
      edge_limit: Math.min(defaultEdges, Math.max(1, maxEdges - result.edges.length)),
      ...(result.cursor === null ? {} : { cursor: result.cursor }),
    }, signal);
    if (!page.data || !page.scope || page.scope.vector_digest !== scope.vector_digest) {
      throw new Error('Scope của trang đồ thị đã thay đổi.');
    }
    await verifyScopeDigest(page.scope, 32768);
    if (await subgraphDigest(page.scope, page.data.nodes, page.data.edges, 2 << 20) !== page.data.subgraph_digest) {
      throw new Error('Digest của trang đồ thị không khớp.');
    }
    signal.throwIfAborted();
    const next = page.pagination.next_cursor;
    if (next !== null && cursors.has(next)) throw new Error('Con trỏ đồ thị không tiến triển.');
    if (next !== null) cursors.add(next);
    const nodes = new Map(result.nodes.map(node => [node.node_id, node]));
    const edges = new Map(result.edges.map(edge => [edge.edge_id, edge]));
    for (const node of page.data.nodes) {
      const prior = nodes.get(node.node_id);
      if (!prior) nodes.set(node.node_id, node);
      else if (JSON.stringify(prior) !== JSON.stringify(node)) throw new Error('Nút đã thay đổi trong snapshot đang đọc.');
    }
    for (const edge of page.data.edges) {
      const prior = edges.get(edge.edge_id);
      if (!prior) edges.set(edge.edge_id, edge);
      else if (JSON.stringify(prior) !== JSON.stringify(edge)) throw new Error('Cạnh đã thay đổi trong snapshot đang đọc.');
    }
    if (nodes.size > maxNodes || edges.size > maxEdges) {
      return { ...result, viewportFull: !initial };
    }
    result = {
      nodes: [...nodes.values()], edges: [...edges.values()], coverage: page.coverage, warnings: page.warnings,
      cursor: next, viewportFull: false,
      pendingNodes: page.pagination.required_pending, pendingEdges: page.pagination.optional_pending,
    };
    publishPage?.(result);
    if (next === null) break;
  }
  return result;
}
