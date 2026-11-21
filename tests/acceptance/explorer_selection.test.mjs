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
