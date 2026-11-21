import assert from 'node:assert/strict';
import test from 'node:test';
import { readObservedFlow } from '../../web/explorer/src/features/flow/data.ts';
import { scopeDigest } from '../../web/explorer/src/scope.ts';

test('observed flow requires pinned scope, the request span and endpoint evidence', async () => {
  const member = {
    worktree_id: '11111111-1111-4111-8111-111111111111',
    generation_id: '22222222-2222-4222-8222-222222222222',
    profile_id: 'a'.repeat(64), snapshot_digest: 'b'.repeat(64), policy_digest: 'c'.repeat(64),
    policy_epoch: 1, binding_revision: 1, record_epoch: 1, service_id: null,
    index_incarnation: '33333333-3333-4333-8333-333333333333',
    records_incarnation: '44444444-4444-4444-8444-444444444444',
  };
  const scope = {
    kind: 'single', catalog_incarnation: '55555555-5555-4555-8555-555555555555',
    group_id: null, group_revision: null, vector_digest: '0'.repeat(64), members: [member], unavailable_members: [],
  };
  scope.vector_digest = await scopeDigest(scope, 32768);
  const root = { kind: 'symbol', symbol_id: 'fixture-root', worktree_id: member.worktree_id,
    generation_id: member.generation_id, profile_id: member.profile_id };
  const artifact = 'd'.repeat(64);
  const evidence = span => ({ evidence_id: 'span-' + 'e'.repeat(32) + '-' + span.repeat(16),
    origin: 'runtime_observation', kind: 'runtime_observed', source_refs: [], record_ref: null, artifact_id: artifact });
  const step = (id, kind, span) => ({ step_id: id, step_kind: kind, canonical_refs: [root], source_refs: [],
    order_semantics: 'causal_trace', possible_or_observed: 'observed', outcome_status: 'attempt_observed',
    evidence_refs: [evidence(span)], limitations: [] });
  const data = { operation: 'flow_query', mode: 'observed', root_ref: root,
    projection_id: 'f'.repeat(64), steps: [step('request', 'http_request', '1'), step('handler', 'http_handler', '2')],
    edges: [{ edge_id: 'parent', from_step_id: 'request', to_step_id: 'handler', kind: 'control', condition_source_ref: null,
      evidence_refs: [evidence('1'), evidence('2')] }] };
  const signal = new AbortController().signal;
  const read = async mutate => readObservedFlow({
    async flow(request) {
      assert.deepEqual(request, { root_ref: root, mode: 'observed', artifact_id: artifact, scope: { pinned_vector: scope } });
      const value = structuredClone({ scope, coverage: { status: 'partial' }, data });
      mutate?.(value);
      return value;
    },
  }, scope, root, artifact, signal);
  assert.equal((await read()).data.edges.length, 1);
  await assert.rejects(read(value => { value.data.mode = 'static'; }), /không khớp lần chạy/);
  await assert.rejects(read(value => { value.scope.vector_digest = '0'.repeat(64); }), /không khớp lần chạy/);
  await assert.rejects(read(value => { value.data.steps[0].evidence_refs[0].artifact_id = 'a'.repeat(64); }), /thiếu span/);
  await assert.rejects(read(value => { value.data.edges[0].evidence_refs[1] = evidence('3'); }), /thiếu bằng chứng/);
});
