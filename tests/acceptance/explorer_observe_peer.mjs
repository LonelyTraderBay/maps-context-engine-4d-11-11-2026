import { installPreviewSDK } from '../../packages/preview-sdk/pairing.mjs';
import { decodeBridgeMessage } from '../../web/explorer/src/bridge.ts';
import { sendObserved } from './explorer_observe_site.mjs';

const options = JSON.parse(document.getElementById('options').textContent);
const marker = document.documentElement.dataset;
const sdk = installPreviewSDK({ window, decodeBridgeMessage, parentOrigin: options.parentOrigin,
  buildMapID: options.approval.buildMapID, observeApproval: options.approval });
const observe = sdk.observations;
const actions = new Map();

document.getElementById('arm').addEventListener('click', event => {
  marker.armed = String(observe.arm(event));
});
for (const name of ['first', 'second', 'cors', 'redirect', 'outside', 'compatible', 'retry', 'cache', 'pending']) {
  document.getElementById(name).addEventListener('click', async event => {
    const action = observe.begin(event);
    if (!action) return;
    actions.set(name, action);
    const input = {
      first: '/api/first?fixture-query=redact',
      second: '/api/second',
      cors: options.corsOrigin + '/api/cors',
      redirect: '/api/redirect',
      outside: '/apiv2',
      compatible: '/api/compatible',
      retry: '/api/retry',
      pending: '/api/pending',
    }[name];
    if (name === 'cache') {
      // This app already has the data for this action in memory.
      marker.cache = 'ok';
      return;
    }
    const headers = name === 'compatible' ? { traceparent: options.existingTraceParent, tracestate: 'fixture=sampled' }
      : name === 'first' ? { 'x-fixture-debug': 'synthetic-secret-marker' } : {};
    try {
      const init = { method: 'POST', headers, body: 'synthetic-body' };
      let response = await sendObserved(observe, action, input, init);
      if (name === 'retry' && response.status === 503)
        response = await sendObserved(observe, action, input, init);
      marker[name] = response.ok ? 'ok' : 'error';
    } catch {
      marker[name] = 'Transport unavailable';
    }
  });
}
document.getElementById('background').addEventListener('click', async () => {
  const response = await fetch('/api/background', { method: 'POST' });
  marker.background = response.ok ? 'ok' : 'error';
});
document.getElementById('export').addEventListener('click', event => {
  const name = document.getElementById('export-action').value;
  const result = observe.exportForAction(actions.get(name), event);
  document.getElementById('export-output').textContent = result ? JSON.stringify(result) : '';
});
window.changeFixtureBuild = sdk.buildChanged;
window.observeFixtureArmed = observe.armed;
