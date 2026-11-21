import assert from 'node:assert/strict';
import test from 'node:test';
import { initialSelection, selectionReducer as select } from '../../web/explorer/src/shell/selection.ts';
import { selectionLocator } from '../../web/explorer/src/shell/context_locator.ts';
import { layout } from '../../web/explorer/src/features/graph/layout.ts';

test('graph worker lays out the bounded viewport in deterministic input order', () => {
  const nodes = Array.from({ length: 500 }, (_, i) => String(i).padStart(3, '0'));
  const edges = nodes.flatMap((from, i) => Array.from({ length: 3 }, (_, branch) => [
    from + branch, from, nodes[Math.floor(i / 10) * 10 + (i + branch + 1) % 10],
  ]));
  const ordered = layout({ nodes, edges });
  assert.equal(ordered.length, 500);
  assert.ok(ordered.every((position, i) => position.id === nodes[i] &&
    Number.isFinite(position.x) && Number.isFinite(position.y)));
  assert.deepEqual(layout({ nodes: nodes.toReversed(), edges: edges.toReversed() }), ordered);
  assert.throws(() => layout({ nodes: [...nodes, 'over'], edges }), /Invalid layout viewport/);
  assert.throws(() => layout({ nodes, edges: [...edges, ['over', '001', '002']] }), /Invalid layout viewport/);
  assert.throws(() => layout({ nodes, edges: [['missing', '001', 'outside']] }), /unknown endpoint/);
});

test('Explorer selection is scoped to the current graph digest', () => {
  let selection = select(initialSelection, { type: 'scope', digest: 'generation-one' });
  selection = select(selection, { type: 'choose', node: 'Show' });
  selection = select(selection, { type: 'range', index: 1 });
  selection = select(selection, { type: 'choose', node: 'Read' });
  selection = select(selection, { type: 'history', delta: -1 });
  selection = select(selection, { type: 'pane', value: 'right' });

  assert.deepEqual(selection.history[selection.position], { node: 'Show', source: 1 });
  assert.equal(select(selection, { type: 'scope', digest: 'generation-one' }), selection);

  const changed = select(selection, { type: 'scope', digest: 'generation-two' });
  assert.equal(changed.scope, 'generation-two');
  assert.deepEqual(changed.history, []);
  assert.equal(changed.position, -1);
  assert.equal(changed.compactPane, 'right');
  assert.equal(select(changed, { type: 'history', delta: 1 }), changed);
});

test('Preview keeps different instances of one graph node in history and drops stale scope and duplicate selection', () => {
  const scope = 'indexed-scope';
  let state = select(initialSelection, { type: 'scope', digest: scope });
  const preview = (selectionID, instance_id, scopeDigest = scope) => ({ type: 'preview', node: 'Page',
    value: { selectionID, scopeDigest, instance: { instance_id } } });
  const first = preview('pick-1', 'instance-37');
  state = select(state, first);
  assert.equal(select(state, first), state);
  assert.equal(select(state, preview('pick-2', 'instance-38', 'old-scope')), state);
  state = select(state, preview('pick-3', 'instance-38'));
  assert.deepEqual(state.history.map(choice => choice.preview.instance.instance_id), ['instance-37', 'instance-38']);
  state = select(state, { type: 'history', delta: -1 });
  assert.equal(state.history[state.position].preview.selectionID, 'pick-1');
});

test('Selection locator retains exact members and source positions', () => {
  const identity = { worktree_id: 'worktree', profile_id: 'profile', generation_id: 'generation' };
  const scope = { kind: 'single', members: [{ ...identity, service_id: null }], unavailable_members: [] };
  const source = { ...identity, path: 'entry.tsx', content_hash: 'source', range: { start_byte: 12, end_byte: 18 } };
  const file = { ...identity, kind: 'file', path: source.path, content_hash: source.content_hash };
  const position = selectionLocator(file, scope, source);
  assert.equal(position.scope.pinned_vector, scope);
  assert.deepEqual(position.selector, { kind: 'source_position', path: 'entry.tsx', content_hash: 'source', byte_offset: 12 });
  assert.equal(selectionLocator(file, scope, { ...source, generation_id: 'other' }), null);

  const entity = { ...identity, kind: 'entity', entity_id: 'operation' };
  const group = { ...scope, kind: 'federated', members: [{ ...identity, service_id: 'service-one' }] };
  assert.deepEqual(selectionLocator(entity, group).selector, { kind: 'ref', ref: entity, service_id: 'service-one' });
  assert.equal(selectionLocator(entity, { ...group, members: [...group.members, { ...identity, service_id: 'service-two' }] }), null);
  assert.equal(selectionLocator(file, group, source), null);
});
