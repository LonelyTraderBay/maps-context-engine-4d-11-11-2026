const hash = /^[0-9a-f]{64}$/;
const nonce = /^[A-Za-z0-9_-]{43}$/;

// The target dev app supplies the pinned S43 decoder, its exact allowed
// Explorer origin and its current build map. This SDK receives no credentials.
export function installPreviewSDK({ window, parentOrigin, buildMapID, decodeBridgeMessage, onBound, onDispose, allowScroll = false }) {
  const parent = window.parent;
  const origin = new URL(parentOrigin);
  if (origin.origin !== parentOrigin || !['http:', 'https:'].includes(origin.protocol) ||
      parent === window || window.location.origin === parentOrigin || !hash.test(buildMapID) ||
      typeof decodeBridgeMessage !== 'function') throw new Error('Invalid SDK preview context');
  let state = 'waiting';
  let context;
  let registry;
  let stopObservation;
  let mode = 'inspect';
  let authorizationEpoch = 0;
  let incoming = 0;
  let outgoing = 0;
  const messages = new Set();
  const input = new AbortController();
  const blocked = ['pointerdown', 'pointerup', 'click', 'dblclick', 'contextmenu', 'touchstart', 'touchend',
    'keydown', 'keyup', 'submit', 'wheel', 'focusin'];
  function guard(event) {
    if (mode === 'interact' && !window.document.hidden) return;
    if (event.type === 'focusin' && event.target instanceof window.HTMLElement) event.target.blur();
    if (event.cancelable) event.preventDefault();
    event.stopImmediatePropagation();
  }
  for (const event of blocked) {
    window.addEventListener(event, guard, { capture: true, passive: false, signal: input.signal });
  }
  window.document.addEventListener('visibilitychange', () => {
    if (window.document.hidden) mode = 'inspect';
  }, { signal: input.signal });

  function emit(type, payload, selectionID, replyTo) {
    const response = { protocol_version: 1, ...context, type, payload, selection_id: selectionID,
      seq: ++outgoing, message_id: window.crypto.randomUUID(), reply_to: replyTo };
    const encoded = JSON.stringify(response);
    decodeBridgeMessage(encoded, 'sdk_to_parent', 16_384);
    parent.postMessage(encoded, parentOrigin);
  }

  function reply(type, payload, request) {
    emit(type, payload, request.selection_id, request.message_id);
  }

  function stopTracking() {
    stopObservation?.();
    stopObservation = undefined;
  }

  function track(ref, selectionID) {
    stopTracking();
    if (!ref || !registry || typeof registry.observe !== 'function') return;
    stopObservation = registry.observe(ref, sample => {
      if (sample === null && state === 'bound' && mode === 'inspect')
        emit('INSTANCE_INVALIDATED', { instance_refs: [ref] }, selectionID, null);
    });
  }

  function receive(event) {
    if (event.origin !== parentOrigin || event.source !== parent || typeof event.data !== 'string') return;
    let message;
    try { message = decodeBridgeMessage(event.data, 'parent_to_sdk', 16_384); } catch { return; }
    const selecting = ['PICK_AT', 'LIST_INSTANCES', 'HIGHLIGHT', 'CLEAR_HIGHLIGHT', 'REQUEST_SCROLL'].includes(message.type);
    if (message.seq <= incoming || messages.has(message.message_id) ||
        message.reply_to !== null || selecting !== (message.selection_id !== null)) return;
    if (state === 'waiting') {
      if (message.type !== 'CHALLENGE' || message.document_epoch !== 1 || message.hmr_epoch !== 0 ||
          message.payload.approved_origin !== window.location.origin || !nonce.test(message.payload.nonce)) return;
      context = { session_id: message.session_id, document_epoch: message.document_epoch, hmr_epoch: message.hmr_epoch };
    } else if (message.session_id !== context.session_id || message.document_epoch !== context.document_epoch ||
        message.hmr_epoch !== context.hmr_epoch) return;
    if (message.type === 'CHALLENGE' && state === 'waiting') {
      state = 'challenged';
      reply('READY', { nonce: message.payload.nonce, build_map_id: buildMapID,
        capabilities: ['instance_registry', 'viewport_css', 'heartbeat'] }, message);
    } else if (message.type === 'BOUND' && state === 'challenged' && message.payload.build_map_id === buildMapID &&
        message.payload.authorization_epoch === 0) {
      registry = onBound?.({ ...context, build_map_id: buildMapID });
      state = 'bound';
      reply('BOUND_ACK', message.payload, message);
    } else if (message.type === 'PING' && state === 'bound') {
      reply('PONG', { probe_id: message.payload.probe_id }, message);
    } else if (message.type === 'SET_MODE' && state === 'bound' && message.payload.authorization_epoch > authorizationEpoch) {
      const accepted = message.payload.mode === 'inspect' || !window.document.hidden;
      authorizationEpoch = message.payload.authorization_epoch;
      mode = accepted ? message.payload.mode : 'inspect';
      stopTracking();
      reply('MODE_ACK', { ...message.payload, accepted }, message);
    } else if (message.type === 'PICK_AT' && state === 'bound' && mode === 'inspect') {
      const { x_css, y_css, selection_kind } = message.payload;
      const picked = registry?.pickAt(x_css, y_css, selection_kind) ?? { targets: [], status: 'unsupported' };
      reply('PICK_RESULT', picked, message);
      track(picked.targets[0]?.instance_ref, message.selection_id);
    } else if (message.type === 'LIST_INSTANCES' && state === 'bound' && mode === 'inspect' && registry) {
      const { anchor_refs, limit, cursor } = message.payload;
      let page;
      try { page = registry.listInstances(anchor_refs, limit, cursor); } catch { return; }
      reply('INSTANCES', page, message);
    } else if (message.type === 'HIGHLIGHT' && state === 'bound' && mode === 'inspect' && registry) {
      const observed = registry.highlightStatus(message.payload.instance_refs);
      reply('HIGHLIGHT_RESULT', observed, message);
      track(observed.matched[0], message.selection_id);
    } else if (message.type === 'CLEAR_HIGHLIGHT' && state === 'bound') {
      stopTracking();
    } else if (message.type === 'REQUEST_SCROLL' && state === 'bound' && mode === 'inspect' && registry) {
      const status = allowScroll && !window.document.hidden ? registry.scrollIntoView(message.payload.instance_ref) : 'not_permitted';
      reply('SCROLL_RESULT', { status }, message);
    } else if (message.type === 'DISPOSE' && state !== 'waiting') {
      reply('DISPOSED', {}, message);
      dispose();
      return;
    } else return;
    incoming = message.seq;
    if (messages.size >= 256) messages.delete(messages.values().next().value);
    messages.add(message.message_id);
  }

  function dispose() {
    if (state === 'disposed') return;
    window.removeEventListener('message', receive);
    window.removeEventListener('pagehide', dispose);
    input.abort();
    stopTracking();
    registry?.dispose();
    registry = undefined;
    messages.clear();
    state = 'disposed';
    context = undefined;
    onDispose?.();
  }

  function buildChanged(nextBuildMapID) {
    if (state !== 'bound' || !hash.test(nextBuildMapID) || nextBuildMapID === buildMapID) return false;
    mode = 'inspect';
    state = 'stale';
    stopTracking();
    registry?.dispose();
    registry = undefined;
    emit('BUILD_CHANGED', { build_map_id: nextBuildMapID, new_hmr_epoch: context.hmr_epoch + 1 }, null, null);
    return true;
  }

  window.addEventListener('message', receive);
  window.addEventListener('pagehide', dispose, { once: true });
  return Object.assign(dispose, { buildChanged });
}
