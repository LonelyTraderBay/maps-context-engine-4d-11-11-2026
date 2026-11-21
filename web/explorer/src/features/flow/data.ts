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

export async function readObservedFlow(client: Pick<ExplorerClient, 'flow'>, scope: ScopeVector, root: GraphRef,
  artifactId: string, signal: AbortSignal): Promise<StaticFlow> {
  if (!uniqueMember(scope, root) || !/^[0-9a-f]{64}$/.test(artifactId)) {
    throw new Error('Lần quan sát không khớp phạm vi đã chọn.');
  }
  const response = await client.flow({ scope: { pinned_vector: scope }, root_ref: root, mode: 'observed',
    artifact_id: artifactId }, signal);
  const data = response.data;
  if (!data || data.mode !== 'observed' || !response.scope || response.scope.vector_digest !== scope.vector_digest ||
      await graphRefDigest(data.root_ref, 8192) !== await graphRefDigest(root, 8192) ||
      response.coverage.status !== 'partial') {
    throw new Error('Flow quan sát không khớp lần chạy đã chọn.');
  }
  await verifyScopeDigest(response.scope, 32768);
  const validEvidence = (ref: FlowStep['evidence_refs'][number]) =>
    ref.origin === 'runtime_observation' && ref.kind === 'runtime_observed' && ref.artifact_id !== null &&
    /^[0-9a-f]{64}$/.test(ref.artifact_id) && /^span-[0-9a-f]{32}-[0-9a-f]{16}$/.test(ref.evidence_id);
  const steps = new Map(data.steps.map(step => [step.step_id, step]));
  const edges = new Set(data.edges.map(edge => edge.edge_id));
  const first = data.steps[0];
  if (steps.size !== data.steps.length || edges.size !== data.edges.length || steps.size > 100 || edges.size > 300 ||
      !first?.evidence_refs.some(ref => ref.artifact_id === artifactId)) {
    throw new Error('Flow quan sát thiếu span của yêu cầu hoặc vượt giới hạn.');
  }
  const rootDigest = await graphRefDigest(root, 8192);
  const firstRefs = await Promise.all(first.canonical_refs.map(ref => graphRefDigest(ref, 8192)));
  if (!firstRefs.includes(rootDigest)) throw new Error('Span của yêu cầu nằm ngoài gốc flow đã chọn.');
  for (const step of data.steps) {
    if (step.possible_or_observed !== 'observed' || step.order_semantics !== 'causal_trace' ||
        step.outcome_status === 'statically_possible' || step.outcome_status === 'declared_operation' ||
        step.evidence_refs.length === 0 || !step.evidence_refs.every(validEvidence) ||
        !step.canonical_refs.every(ref => uniqueMember(scope, ref)) ||
        !step.source_refs.every(ref => uniqueMember(scope, ref))) {
      throw new Error('Bước quan sát thiếu provenance hoặc nằm ngoài phạm vi.');
    }
  }
  for (const edge of data.edges) {
    const from = steps.get(edge.from_step_id);
    const to = steps.get(edge.to_step_id);
    if (!from || !to || edge.evidence_refs.length !== 2 || !edge.evidence_refs.every(validEvidence) ||
        ![from, to].every(step => step.evidence_refs.some(ref => edge.evidence_refs.some(evidence =>
          evidence.artifact_id === ref.artifact_id && evidence.evidence_id === ref.evidence_id)))) {
      throw new Error('Quan hệ quan sát thiếu bằng chứng cho hai span.');
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
