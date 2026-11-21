import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createTraceObserver } from '../../packages/preview-sdk/observe.mjs';

function fixture(overrides = {}) {
  const calls = [];
  let pending;
  const buildMapID = 'a'.repeat(64);
  const state = { mode: 'interact', session_id: crypto.randomUUID(), build_map_id: buildMapID,
    document_epoch: 1, hmr_epoch: 0, authorization_epoch: 1 };
  class FixtureEvent {
    type = 'click';
    isTrusted = true;
    eventPhase = 2;
  }
  const app = {
    Event: FixtureEvent, crypto, URL, Headers, document: { hidden: false }, location: { href: 'http://127.0.0.1:14100/app' },
    fetch: (input, init) => {
      calls.push({ input, traced: new Headers(init.headers).has('traceparent') });
      return pending ?? Promise.resolve({ ok: true });
    },
  };
  const observer = createTraceObserver(app, {
    application: 'fixture', service: 'frontend', collector: 'fixture', instrumentation: 'v1',
    environment: 'test', buildMapID, profileID: 'b'.repeat(64), revision: 'd'.repeat(40),
    expiresAt: Date.now() + 60_000, operations: ['request'],
    destinations: [{ origin: 'http://127.0.0.1:14100', pathPrefix: '/api', methods: ['POST'] }],
    ...overrides,
  }, () => state);
  return { observer, calls, app, state, event: () => new FixtureEvent(), deferFetch: promise => { pending = promise; } };
}

test('the SDK caps per-grant requests and active actions', async () => {
  const { observer, calls, event } = fixture();
  assert.ok(observer.arm(event()));
  const action = observer.begin(event());
  for (let i = 0; i < 129; i++)
    await observer.fetchForAction(action, '/api', { method: 'POST' }, 'request');
  assert.equal(calls.filter(call => call.traced).length, 128);
  assert.equal(calls.at(-1).traced, false);
  assert.equal(observer.exportForAction(action, event()).receipts.length, 128);
  for (let i = 0; i < 32; i++) assert.ok(observer.begin(event()));
  assert.equal(observer.begin(event()), undefined);
  observer.disarm();
  assert.equal(observer.begin(event()), undefined);
  assert.ok(observer.arm(event()));
  assert.ok(observer.begin(event()));
});

test('pending fetches cannot be exported across visibility and authorization changes', async () => {
  const { observer, app, state, event, deferFetch } = fixture();
  assert.ok(observer.arm(event()));
  const action = observer.begin(event());
  let finish;
  deferFetch(new Promise(resolve => { finish = resolve; }));
  const request = observer.fetchForAction(action, '/api/path', { method: 'POST' }, 'request');
  assert.equal(observer.exportForAction(action, event()), undefined);
  app.document.hidden = true;
  assert.equal(observer.armed(), false);
  app.document.hidden = false;
  state.authorization_epoch = 2;
  assert.ok(observer.arm(event()));
  finish({ ok: true });
  await request;
  assert.equal(observer.exportForAction(action, event()), undefined);
  const issued = observer.begin(event());
  state.document_epoch++;
  assert.equal(observer.exportForAction(issued, event()), undefined);
  assert.equal(observer.armed(), false);
});

test('expired permits and reused events cannot arm or attribute a request', () => {
  const expired = fixture({ expiresAt: Date.now() - 1 });
  assert.equal(expired.observer.arm(expired.event()), false);
  const { observer, event } = fixture();
  const one = event();
  assert.ok(observer.arm(one));
  assert.equal(observer.begin(one), undefined);
  const two = event();
  assert.ok(observer.begin(two));
  assert.equal(observer.begin(two), undefined);
  observer.disarm();
  assert.equal(observer.arm(one), false);
});
