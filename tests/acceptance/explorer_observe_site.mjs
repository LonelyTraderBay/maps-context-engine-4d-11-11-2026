export function sendObserved(observe, action, input, init) {
  return observe.fetchForAction(action, input, init, 'request');
}

export function captureObservedArrival(traceparent, timestamp, spanID) {
  const [, traceID, parentSpanID] = traceparent.split('-');
  return {
    trace_id: traceID, span_id: spanID, parent_span_id: parentSpanID, interaction_id: '',
    operation: 'handler', started_at: timestamp, ended_at: timestamp, status: 'ok', links: [],
  };
}
