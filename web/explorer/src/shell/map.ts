import type { Coverage, FileRef, Gap, GraphData, GraphEdge, GraphNode, ScopeVector, SymbolRef } from '../index.ts';
import { subgraphDigest, verifyScopeDigest } from '../index.ts';
import type { ExplorerClient } from '../api/client.ts';
import { hardEdges, hardNodes } from '../features/graph/viewport.ts';

export { hardNodes };

export const defaultNodes = 200;
export const defaultEdges = 500;

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
  file?: FileRef,
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
      root: file ? { kind: 'ref', ref: file, service_id: null } : { kind: 'workspace' },
      ...(file ? { local_file: true } : {}),
      scope: { pinned_vector: scope },
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
    const symbolCatalog = page.coverage.gaps.some(gap => gap.code === 'GRAPH_PROJECTION_LIMIT');
    if (symbolCatalog && nodes.size === maxNodes && next !== null) result.viewportFull = !initial;
    publishPage?.(result);
    if (next === null || symbolCatalog && (initial || nodes.size === maxNodes)) break;
  }
  return result;
}

export type DirectoryListing = NonNullable<GraphData['directory']>;
export type DirectoryEntry = DirectoryListing['entries'][number];

export type RelationDirection = 'outgoing' | 'incoming' | 'both';
export type RelationFilter = 'functional' | 'calls' | 'imports' | 'variables' | 'structure' | 'impact';

const relationKinds: Record<RelationFilter, string[]> = {
  functional: ['ROUTE_COMPONENT', 'RENDERS_COMPONENT', 'RENDERS_JSX', 'BINDS_EVENT',
    'CHECKS_CONDITION', 'BRANCH_TRUE', 'BRANCH_FALSE', 'CALLS_DIRECT', 'CALLS_INTERFACE', 'READS', 'WRITES'],
  calls: ['CALLS_DIRECT', 'CALLS_INTERFACE', 'MAY_DISPATCH_TO', 'UNRESOLVED_CALL'],
  imports: ['IMPORTS', 'REFERS_TO', 'UNRESOLVED_REF'],
  variables: ['READS', 'WRITES'],
  structure: ['CONTAINS'],
  impact: ['CALLS_DIRECT', 'CALLS_INTERFACE', 'MAY_DISPATCH_TO', 'READS', 'WRITES',
    'BINDS_EVENT', 'RENDERS_COMPONENT', 'RENDERS_JSX', 'ROUTE_COMPONENT', 'REFERS_TO', 'IMPORTS'],
};

const relationLabels: Record<string, string> = {
  ROUTE_COMPONENT: 'Khai báo màn hình', RENDERS_COMPONENT: 'Render', RENDERS_JSX: 'Hiển thị',
  BINDS_EVENT: 'Gắn sự kiện', CALLS_DIRECT: 'Gọi hàm', CALLS_INTERFACE: 'Gọi qua interface',
  CHECKS_CONDITION: 'Xét điều kiện', BRANCH_TRUE: 'Điều kiện đúng', BRANCH_FALSE: 'Điều kiện sai',
  MAY_DISPATCH_TO: 'Có thể gọi', IMPORTS: 'Import', REFERS_TO: 'Tham chiếu', CONTAINS: 'Chứa',
  UNRESOLVED_REF: 'Đích chưa rõ', UNRESOLVED_CALL: 'Lời gọi chưa rõ', READS: 'Đọc biến', WRITES: 'Ghi biến',
};
export const relationLabel = (kind: string) => relationLabels[kind] ?? kind;
export type DisplayEdge = GraphEdge & { accessKinds?: string[] };

export function edgeRelationLabel(edge: DisplayEdge): string {
  return new Set(edge.accessKinds).size > 1 ? 'Đọc và ghi biến' : relationLabel(edge.relation_kind);
}

// Multiple uses of the same variable share one arrow. Each occurrence still
// has its own source reference in the inspector.
export function groupVariableEdges(edges: GraphEdge[]): DisplayEdge[] {
  const grouped = new Map<string, DisplayEdge>();
  for (const edge of edges) {
    if (edge.relation_kind !== 'READS' && edge.relation_kind !== 'WRITES') {
      grouped.set(edge.edge_id, edge);
      continue;
    }
    const key = JSON.stringify([edge.from_node_id, edge.to_node_id, edge.resolution_status]);
    const prior = grouped.get(key);
    const kinds = edge.evidence_refs.map(() => edge.relation_kind);
    grouped.set(key, prior ? { ...prior, accessKinds: [...(prior.accessKinds ?? []), ...kinds],
      evidence_refs: [...prior.evidence_refs, ...edge.evidence_refs] } : { ...edge, accessKinds: kinds });
  }
  return [...grouped.values()];
}

export function traceFlowEdges(edges: readonly Pick<GraphEdge, 'edge_id' | 'from_node_id' | 'to_node_id'>[],
  nodeId: string | null): Set<string> {
  const trace = new Set<string>();
  if (nodeId === null) return trace;
  // Traverse each direction independently so a common ancestor never brings in a sibling branch.
  for (const [start, end] of [['from_node_id', 'to_node_id'], ['to_node_id', 'from_node_id']] as const) {
    const adjacent = Map.groupBy(edges, edge => edge[start]);
    const visited = new Set([nodeId]);
    const pending = [nodeId];
    while (pending.length > 0) {
      for (const edge of adjacent.get(pending.pop()!) ?? []) {
        trace.add(edge.edge_id);
        const next = edge[end];
        if (!visited.has(next)) {
          visited.add(next);
          pending.push(next);
        }
      }
    }
  }
  return trace;
}

async function readBoundGraph(client: ExplorerClient, request: Parameters<ExplorerClient['graph']>[0],
  scope: ScopeVector, signal: AbortSignal) {
  const page = await client.graph({ ...request, scope: { pinned_vector: scope } }, signal);
  if (!page.data || !page.scope || page.scope.vector_digest !== scope.vector_digest) {
    throw new Error('Scope của đồ thị đã thay đổi.');
  }
  await verifyScopeDigest(page.scope, 32768);
  if (await subgraphDigest(page.scope, page.data.nodes, page.data.edges, 2 << 20) !== page.data.subgraph_digest) {
    throw new Error('Digest của trang đồ thị không khớp.');
  }
  signal.throwIfAborted();
  return { data: page.data, coverage: page.coverage, warnings: page.warnings };
}

export async function findFlowRoots(client: ExplorerClient, scope: ScopeVector, query: string, signal: AbortSignal) {
  const page = await readBoundGraph(client, { root: { kind: 'workspace' }, symbol_query: query }, scope, signal);
  return { matches: page.data.nodes.filter(node => node.ref.kind === 'symbol'),
    limited: page.coverage.gaps.some(gap => gap.code.endsWith('_LIMIT')) };
}

export async function loadNeighborhood(client: ExplorerClient, scope: ScopeVector, root: SymbolRef,
  direction: RelationDirection, filter: RelationFilter, depth: number, signal: AbortSignal): Promise<GraphSlice> {
  const page = await readBoundGraph(client, { root: { kind: 'ref', ref: root, service_id: null },
    neighborhood: direction, depth, kinds: relationKinds[filter] }, scope, signal);
  return { ...emptyGraph, nodes: page.data.nodes, edges: page.data.edges, coverage: page.coverage, warnings: page.warnings };
}

export function extendNeighborhood(existing: GraphSlice, page: GraphSlice): GraphSlice {
  const nodes = new Map(existing.nodes.map(node => [node.node_id, node]));
  const edges = new Map(existing.edges.map(edge => [edge.edge_id, edge]));
  for (const node of page.nodes) nodes.set(node.node_id, node);
  for (const edge of page.edges) edges.set(edge.edge_id, edge);
  if (nodes.size > hardNodes || edges.size > hardEdges) throw new Error('Đã đạt giới hạn. Mở một luồng mới từ nút đang chọn.');
  return { ...page, nodes: [...nodes.values()], edges: [...edges.values()] };
}

export async function loadDirectory(client: ExplorerClient, scope: ScopeVector, directory: string,
  cursor: string | null, signal: AbortSignal) {
  const page = await client.graph({
    root: { kind: 'workspace' }, directory, scope: { pinned_vector: scope },
    ...(cursor ? { cursor } : {}),
  }, signal);
  if (!page.data?.directory || page.data.directory.path !== directory ||
      !page.scope || page.scope.vector_digest !== scope.vector_digest ||
      page.data.nodes.length || page.data.edges.length || cursor && page.pagination.next_cursor === cursor) {
    throw new Error('Danh sách tệp trả về không khớp thư mục.');
  }
  await verifyScopeDigest(page.scope, 32768);
  if (await subgraphDigest(page.scope, [], [], 2 << 20) !== page.data.subgraph_digest) {
    throw new Error('Digest của trang thư mục không khớp.');
  }
  signal.throwIfAborted();
  return { listing: page.data.directory, cursor: page.pagination.next_cursor };
}
