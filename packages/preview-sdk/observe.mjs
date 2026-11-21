const digest = /^[0-9a-f]{64}$/;
const revision = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const label = /^[a-zA-Z][a-zA-Z0-9._-]{0,79}$/;
const verbs = new Set(['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE']);
const maxRequests = 128;
const maxActions = 32;

function validLabel(value) { return typeof value === 'string' && label.test(value); }
function matches(pattern, value) { return typeof value === 'string' && pattern.test(value); }

function approvedDestinations(input) {
  if (!Array.isArray(input) || input.length < 1 || input.length > 16) throw new Error('Invalid Observe destinations');
  return input.map(({ origin, pathPrefix, methods }) => {
    const url = new URL(origin);
    if (url.origin !== origin || !['http:', 'https:'].includes(url.protocol) ||
        !['127.0.0.1', '[::1]'].includes(url.hostname) || !matches(/^\/[A-Za-z0-9._/-]{0,2047}$/, pathPrefix) ||
        pathPrefix.split('/').some(part => part === '.' || part === '..') ||
        (pathPrefix.length > 1 && pathPrefix.endsWith('/')) ||
        !Array.isArray(methods) || methods.length < 1 || methods.some(method => !verbs.has(method))) {
      throw new Error('Invalid Observe destination');
    }
    return { origin, pathPrefix, methods: new Set(methods) };
  });
}

function inDestination(rule, url, method) {
  return url.origin === rule.origin && !url.username && !url.password && !url.hash && rule.methods.has(method) &&
    /^\/[A-Za-z0-9._/-]*$/.test(url.pathname) &&
    (rule.pathPrefix === '/' || url.pathname === rule.pathPrefix || url.pathname.startsWith(rule.pathPrefix + '/'));
}

function iso(ms) { return new Date(ms).toISOString(); }

// The paired dev application opts into this helper for specific user actions.
// It never installs a global network hook, starts the app or starts a collector.
export function createTraceObserver(window, approval, sessionState) {
  if (!approval || typeof sessionState !== 'function' || approval.environment !== 'test' ||
      !Number.isSafeInteger(approval.expiresAt) || approval.expiresAt <= 0 || !matches(digest, approval.buildMapID) ||
      !matches(digest, approval.profileID) || !matches(revision, approval.revision) ||
      ![approval.application, approval.service, approval.collector, approval.instrumentation].every(validLabel) ||
      !Array.isArray(approval.operations) || approval.operations.length < 1 || approval.operations.length > 128 ||
      !approval.operations.every(validLabel)) {
    throw new Error('Invalid Observe approval');
  }
  const destinations = approvedDestinations(approval.destinations);
  const operations = new Set(approval.operations);
  const profile = Object.freeze({ application: approval.application, service: approval.service,
    environment: approval.environment, revision: approval.revision, profile_id: approval.profileID,
    build_digest: approval.buildMapID, collector: approval.collector, instrumentation: approval.instrumentation });
  const boundFetch = window.fetch.bind(window);
  const expiresAt = approval.expiresAt;
  const events = new WeakSet();
  const actions = new Map();
  let armed;
  let requests = 0;
  let inFlight = 0;

  function session() {
    const current = sessionState();
    if (!current || current.mode !== 'interact' || current.authorization_epoch < 1 ||
        window.document.hidden || Date.now() >= expiresAt ||
        current.build_map_id !== profile.build_digest) return undefined;
    return [current.session_id, current.document_epoch, current.hmr_epoch, current.authorization_epoch].join(':');
  }

  function disarm() {
    armed = undefined;
    actions.clear();
    requests = 0;
  }

  function live() {
    if (armed && armed === session()) return true;
    disarm();
    return false;
  }

  function trustedAction(event) {
    if (!(event instanceof window.Event) || !event.isTrusted || event.eventPhase === 0 ||
        !['click', 'submit'].includes(event.type) || events.has(event)) return false;
    events.add(event);
    return true;
  }

  function arm(event) {
    const key = session();
    if (!key || !trustedAction(event)) return false;
    disarm();
    armed = key;
    return true;
  }

  function hex(bytes) {
    const buffer = window.crypto.getRandomValues(new Uint8Array(bytes));
    let value = Array.from(buffer, byte => byte.toString(16).padStart(2, '0')).join('');
    if (/^0+$/.test(value)) value = '1' + value.slice(1);
    return value;
  }

  function begin(event) {
    if (!live() || actions.size >= maxActions || !trustedAction(event)) return undefined;
    const interactionID = window.crypto.randomUUID();
    const handle = Object.freeze({ interactionID });
    const action = { records: [] };
    actions.set(handle, action);
    return handle;
  }

  async function fetchForAction(handle, input, init = {}, operation) {
    const action = live() && actions.get(handle);
    if (!action || requests >= maxRequests || inFlight >= maxRequests || !operations.has(operation) || init.mode === 'no-cors' ||
        (typeof input !== 'string' && !(input instanceof window.URL))) return boundFetch(input, init);
    const url = new window.URL(input, window.location.href);
    const method = (init.method || 'GET').toUpperCase();
    if (!destinations.some(rule => inDestination(rule, url, method))) return boundFetch(input, init);
    const headers = new window.Headers(init.headers);
    // Existing carriers remain with the application's own instrumentation.
    // The helper cannot attribute their parent span to this interaction.
    if (headers.has('traceparent') || headers.has('tracestate')) return boundFetch(input, init);
    const traceID = hex(16);
    const spanID = hex(8);
    const requestID = window.crypto.randomUUID();
    const traceparent = `00-${traceID}-${spanID}-01`;
    headers.set('traceparent', traceparent);
    const current = sessionState();
    const record = { traceID, spanID, requestID, traceparent, operation, start: Date.now(),
      context: { session_id: current.session_id, document_epoch: current.document_epoch, hmr_epoch: current.hmr_epoch,
        authorization_epoch: current.authorization_epoch } };
    requests++;
    inFlight++;
    action.records.push(record);
    try {
      const response = await boundFetch(url.href, { ...init, headers, redirect: 'error' });
      record.status = response.ok ? 'ok' : 'error';
      return response;
    } catch (error) {
      record.status = 'error';
      throw error;
    } finally {
      inFlight--;
      record.end = Math.max(Date.now(), record.start);
      live();
    }
  }

  function exportForAction(handle, event) {
    const action = live() && actions.get(handle);
    if (!action || !action.records.length || action.records.some(row => row.end === undefined) ||
        !trustedAction(event)) return undefined;
    actions.delete(handle);
    const rows = action.records;
    const metadata = { schema_version: 1, ...profile, sampling: 'all', sample_rate_ppm: 1_000_000,
      window_start: iso(Math.min(...rows.map(row => row.start))), window_end: iso(Math.max(...rows.map(row => row.end))) };
    const spans = rows.map(row => ({ trace_id: row.traceID, span_id: row.spanID, parent_span_id: '',
      interaction_id: handle.interactionID, operation: row.operation, started_at: iso(row.start), ended_at: iso(row.end),
      status: row.status, links: [] }));
    const receipts = rows.map(row => ({ ...row.context, build_map_id: profile.build_digest, interaction_id: handle.interactionID,
      request_id: row.requestID, traceparent: row.traceparent, trace_id: row.traceID, span_id: row.spanID }));
    return { artifact: { metadata, spans }, receipts };
  }

  return Object.freeze({ arm, disarm, begin, fetchForAction, exportForAction, armed: live });
}
