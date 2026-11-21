import { createRoot } from 'react-dom/client';
import { ReactFlow } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import dagre from '@dagrejs/dagre';
import { canonicalDigest, decodeBridgeMessage, decodeHTTPRequest, utf8 } from '../src/index.ts';
import './fixture.css';

const graph = new dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({})).setGraph({ rankdir: 'LR' });
const left = 'caller';
const right = 'callee';
const refs = [left, right];
for (const ref of refs) graph.setNode(ref, { width: 100, height: 30 });
graph.setEdge(left, right);
graph.setEdge(right, left);
dagre.layout(graph);
const nodes = refs.map(ref => {
  const point = graph.node(ref);
  if (!point || !Number.isFinite(point.x) || !Number.isFinite(point.y)) throw new Error('Missing Dagre layout');
  return { id: ref, data: { label: ref }, position: { x: point.x, y: point.y } };
});
const edges = graph.edges().map(({ v, w }) => ({ id: v + '-' + w, source: v, target: w }));
const root = document.getElementById('root');
const wire = document.getElementById('wire');
const frameOrigin = document.documentElement.dataset.frameOrigin;
if (!root || !wire || !frameOrigin) throw new Error('Incomplete local fixture');
createRoot(root).render(<ReactFlow nodes={nodes} edges={edges} fitView />);

const request = decodeHTTPRequest('POST /graph/query', utf8.encode('{"root":{"kind":"workspace"}}'), 1);
if (request.root.kind !== 'workspace') throw new Error('Missing graph request');
const iframe = document.createElement('iframe');
iframe.title = 'Synthetic bridge peer';
iframe.sandbox.add('allow-scripts', 'allow-same-origin');
window.addEventListener('message', async event => {
  if (event.source !== iframe.contentWindow || event.origin !== frameOrigin) return;
  if (typeof event.data !== 'string') throw new Error('Expected bridge JSON');
  const message = decodeBridgeMessage(event.data, 'sdk_to_parent', 16_000);
  if (message.type !== 'PONG') throw new Error('Fixture expected PONG');
  wire.textContent = await canonicalDigest('fixture-v1', { seq: message.seq }, 4096);
});
iframe.src = frameOrigin + '/';
document.body.append(iframe);
