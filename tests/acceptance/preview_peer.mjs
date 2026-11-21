import { installPreviewSDK } from '@mce4d/preview-sdk/pairing';
import { createInstanceRegistry } from '@mce4d/preview-sdk';
import { decodeBridgeMessage } from '../../web/explorer/src/bridge.ts';

const marker = document.documentElement.dataset;
marker.actions = '0';
marker.submits = '0';
marker.actionFetches = '0';
marker.backgroundTicks = '0';
marker.heartbeat = '0';
marker.scrolls = '0';
setInterval(() => { marker.backgroundTicks = String(Number(marker.backgroundTicks) + 1); }, 40);
document.getElementById('fixture-form').addEventListener('click', () => {
  marker.actions = String(Number(marker.actions) + 1);
  marker.actionFetches = String(Number(marker.actionFetches) + 1);
  void fetch('/fixture-action', { method: 'POST' });
});
document.getElementById('fixture-form').addEventListener('submit', event => {
  event.preventDefault();
  marker.submits = String(Number(marker.submits) + 1);
});
let registry;
const sdk = installPreviewSDK({
  window, parentOrigin: marker.parentOrigin, buildMapID: marker.buildMap, decodeBridgeMessage,
  allowScroll: marker.scroll === 'true',
  onBound: identity => {
    registry = createInstanceRegistry(document, identity, marker.anchors?.split(',') ?? []);
    marker.bound = 'true';
    return registry;
  },
});
window.changeFixtureBuild = sdk.buildChanged;
window.stopFixtureSDK = sdk;
window.addEventListener('message', event => {
  if (event.source !== window.parent || event.origin !== marker.parentOrigin) return;
  try {
    const result = decodeBridgeMessage(event.data, 'parent_to_sdk', 1_000_000);
    if (result.type === 'PING') marker.heartbeat = String(Number(marker.heartbeat) + 1);
    if (result.type === 'REQUEST_SCROLL') marker.scrolls = String(Number(marker.scrolls) + 1);
  } catch { marker.unexpected = 'true'; }
});
