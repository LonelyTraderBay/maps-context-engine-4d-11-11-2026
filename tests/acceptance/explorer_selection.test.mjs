import assert from 'node:assert/strict';
import test from 'node:test';
import { initialSelection, selectionReducer as select } from '../../web/explorer/src/shell/selection.ts';
import { selectionLocator } from '../../web/explorer/src/shell/context_locator.ts';

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
