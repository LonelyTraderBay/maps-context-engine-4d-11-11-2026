import { createElement as h, Fragment, useLayoutEffect, useRef } from 'react';
import { createPortal, flushSync } from 'react-dom';
import { createRoot } from 'react-dom/client';
import { createInstanceRegistry } from '@mce4d/preview-sdk';
import { registerReactBoundary } from '@mce4d/react-inspector';

const S = '01234567-89ab-cdef-0123-456789abcdef';
const identifier = character => character.repeat(64);
const anchors = {
  repeated: identifier('1'), root: identifier('2'), host: identifier('3'), outer: identifier('4'),
};
const build_map_id = identifier('a');
const identity = { session_id: S, document_epoch: 3, hmr_epoch: 2, build_map_id };
const sdk = createInstanceRegistry(document, identity, Object.values(anchors));
const id = name => document.getElementById(name);
const pinned = name => ({ build_map_id, anchor_id: anchors[name] });
const state = { actions: 0, submits: 0, changes: [] };
const own = sdk.registerBoundary(anchors.outer, [id('outside')]);

function View() {
  const left = useRef(null);
  const right = useRef(null);
  useLayoutEffect(() => registerReactBoundary(sdk, anchors.root, [left, right], own.instanceRef), []);
  return h(Fragment, null,
    h('div', { ref: left, id: 'group-a', className: 'group' }, h('div', { id: 'known-host', 'data-mce4d-anchor': anchors.host })),
    h('div', { ref: right, id: 'group-b', className: 'group' }, h('canvas', { id: 'inside-canvas', width: 30, height: 14 })),
    createPortal(h('span', { id: 'unknown-portal' }, 'Portal'), id('portal')));
}

const fixture = createRoot(id('react-root'));
flushSync(() => fixture.render(h(View)));
for (let i = 0; i < 100; i++) {
  const button = document.createElement('button');
  button.type = 'submit';
  button.setAttribute('data-mce4d-anchor', anchors.repeated);
  button.addEventListener('click', () => state.actions++);
  id('buttons').append(button);
}
id('buttons').addEventListener('submit', event => { state.submits++; event.preventDefault(); });
const shadow = id('shadow-host').attachShadow({ mode: 'closed' });
const hiddenNode = document.createElement('b');
hiddenNode.textContent = 'Only an opaque boundary';
shadow.append(hiddenNode);

function pick(element) {
  const r = element.getBoundingClientRect();
  return sdk.pickAt(r.x + r.width / 2, r.y + r.height / 2);
}

function nextLayout() {
  return new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
}

window.runSDKFixture = async () => {
  const exact37 = pick(id('buttons').children[36]);
  const page = sdk.listInstances([pinned('repeated')], 32);
  const items = [...page.items];
  let cursor = page.next_cursor;
  while (cursor) {
    const reply = sdk.listInstances([pinned('repeated')], 32, cursor);
    items.push(...reply.items);
    cursor = reply.next_cursor;
  }
  const nested = pick(id('known-host'));
  const canvas = pick(id('inside-canvas'));
  const opaque = pick(id('shadow-host'));
  const portal = pick(id('unknown-portal'));
  const group = sdk.listInstances([pinned('root')], 4).items[0];
  const selected = exact37.targets[0].instance_ref;
  const initialObservers = window.fixtureObservers();
  sdk.observe(selected, value => state.changes.push(value));
  const attachedObservers = window.fixtureObservers();
  await nextLayout();
  id('scroll-area').scrollTop = 18;
  id('buttons').children[36].style.transform = 'translateX(5px)';
  id('buttons').children[36].style.width = '35px';
  await nextLayout();
  const moved = pick(id('buttons').children[36]);
  const changesBeforeDetach = state.changes.length;
  id('buttons').children[36].remove();
  await nextLayout();
  const afterDetach = sdk.listInstances([pinned('repeated')], 100);
  const staleObserved = state.changes.at(-1) === null;
  let staleRejected = false;
  try { sdk.listInstances([pinned('repeated')], 32, page.next_cursor); } catch { staleRejected = true; }
  const unregistered = () => sdk.listInstances([pinned('root')], 1).items.length === 0;
  flushSync(() => fixture.unmount());
  const boundaryUnmounted = unregistered();
  own();
  sdk.dispose();
  return { exact37, nested, canvas, opaque, portal, group, moved, items, selected, afterDetach, staleObserved,
    staleRejected, boundaryUnmounted, changesBeforeDetach, changes: state.changes,
    initialObservers, attachedObservers, finalObservers: window.fixtureObservers(),
    afterDispose: sdk.pickAt(0, 0), actions: state.actions, submits: state.submits, anchors, identity };
};
