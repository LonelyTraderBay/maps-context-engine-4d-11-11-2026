import assert from 'node:assert/strict';
import test from 'node:test';
import { initialSelection, selectionReducer as select } from '../../web/explorer/src/shell/selection.ts';

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
