import type { FlowData, FlowStep, GraphNode, GraphRef, ScopeVector, SourceRef } from '../../../.generated/types';
import type { ExplorerClient } from '../../api/client.ts';
import { readPinnedSource } from '../../api/source.ts';
import { graphRefDigest, subgraphDigest } from '../../graph.ts';
import { verifyScopeDigest } from '../../scope.ts';
import type { HTTPResponse } from '../../http.ts';

export type StaticFlow = HTTPResponse<'POST /flow/query'> & { data: FlowData };
export type FlowSelection = { node: GraphNode; source: SourceRef; content: string };

function uniqueMember(scope: ScopeVector, ref: Pick<SourceRef, 'worktree_id' | 'profile_id' | 'generation_id'>) {
  const members = scope.members.filter(member => member.worktree_id === ref.worktree_id &&
    member.profile_id === ref.profile_id && member.generation_id === ref.generation_id);
  return members.length === 1 ? members[0]! : null;
}

export async function readStaticFlow(client: Pick<ExplorerClient, 'flow'>, scope: ScopeVector, root: GraphRef,
  signal: AbortSignal): Promise<StaticFlow> {
  if (!uniqueMember(scope, root)) throw new Error('Phạm vi của nút flow không còn khớp.');
  const response = await client.flow({ scope: { pinned_vector: scope }, root_ref: root, mode: 'static' }, signal);
  const data = response.data;
  if (!data || !response.scope || response.scope.vector_digest !== scope.vector_digest || data.mode !== 'static' ||
      await graphRefDigest(data.root_ref, 8192) !== await graphRefDigest(root, 8192)) {
    throw new Error('Flow trả về không khớp lựa chọn.');
  }
  await verifyScopeDigest(response.scope, 32768);
  const stepIDs = new Set(data.steps.map(step => step.step_id));
  const edgeIDs = new Set(data.edges.map(edge => edge.edge_id));
  if (data.steps.length > 100 || data.edges.length > 300 || stepIDs.size !== data.steps.length ||
      edgeIDs.size !== data.edges.length || data.edges.some(edge => !stepIDs.has(edge.from_step_id) || !stepIDs.has(edge.to_step_id))) {
    throw new Error('Flow trả về vượt giới hạn hoặc chứa liên kết ngoài lát hiển thị.');
  }
  for (const step of data.steps) {
    if (step.possible_or_observed !== 'statically_possible' || step.order_semantics === 'causal_trace' ||
        step.outcome_status.endsWith('_observed') || !step.canonical_refs.every(ref => uniqueMember(scope, ref)) ||
        !step.source_refs.every(ref => uniqueMember(scope, ref))) {
      throw new Error('Một bước flow nằm ngoài phạm vi tĩnh đã yêu cầu.');
    }
  }
  signal.throwIfAborted();
  return { ...response, data };
}

export async function readFlowSelection(client: Pick<ExplorerClient, 'graph' | 'source'>, scope: ScopeVector, step: FlowStep,
  visible: readonly GraphNode[], signal: AbortSignal, source = step.source_refs[0]): Promise<FlowSelection> {
  const member = source && uniqueMember(scope, source);
  const ref = step.canonical_refs.find(candidate => (candidate.kind === 'symbol' || candidate.kind === 'entity') &&
    uniqueMember(scope, candidate) === member);
  if (!source || !member || !ref) throw new Error('Bước flow này chưa có nguồn trong phạm vi.');
  const wanted = await graphRefDigest(ref, 8192);
  const node = visible.find(({ ref: candidate }) => uniqueMember(scope, candidate) === member &&
    ((candidate.kind === 'symbol' && ref.kind === 'symbol' && candidate.symbol_id === ref.symbol_id) ||
      (candidate.kind === 'entity' && ref.kind === 'entity' && candidate.entity_id === ref.entity_id)));
  const contentRead = readPinnedSource(client, scope, source, signal);
  // Graph and source use the same immutable selection and cancellation signal.
  const graphRead = node ? Promise.resolve(node) : readGraphNode(client, scope, ref, member.service_id, wanted, signal);
  const [selected, content] = await Promise.all([graphRead, contentRead]);
  signal.throwIfAborted();
  return { node: selected, source, content };
}

async function readGraphNode(client: Pick<ExplorerClient, 'graph'>, scope: ScopeVector, ref: GraphRef,
  service: string | null, wanted: string, signal: AbortSignal): Promise<GraphNode> {
  const response = await client.graph({ root: { kind: 'ref', ref, service_id: service }, scope: { pinned_vector: scope },
    depth: 0, node_limit: 2, edge_limit: 1 }, signal);
  if (!response.data || !response.scope || response.scope.vector_digest !== scope.vector_digest) {
    throw new Error('Nút đồ thị không khớp phạm vi flow.');
  }
  await verifyScopeDigest(response.scope, 32768);
  if (await subgraphDigest(response.scope, response.data.nodes, response.data.edges, 2 << 20) !== response.data.subgraph_digest) {
    throw new Error('Digest của nút flow không khớp.');
  }
  for (const node of response.data.nodes) {
    if (await graphRefDigest(node.ref, 8192) === wanted) return node;
  }
  throw new Error('Nút của bước flow chưa có trong index.');
}
