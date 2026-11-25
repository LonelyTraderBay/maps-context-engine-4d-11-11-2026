import dagre from '@dagrejs/dagre';
import { compareID, hardEdges, hardNodes, nodeHeight, nodeWidth } from './viewport.ts';

export type LayoutInput = {
  nodes: readonly string[];
  edges: readonly (readonly [id: string, from: string, to: string])[];
  vertical?: boolean;
};
export type LayoutPosition = { id: string; x: number; y: number };
export type LayoutResult = { positions: LayoutPosition[] } | { error: true };

export function layout(input: LayoutInput): LayoutPosition[] {
  const ids = new Set(input.nodes);
  const edgeIDs = new Set(input.edges.map(edge => edge[0]));
  if (input.nodes.length > hardNodes || input.edges.length > hardEdges || ids.size !== input.nodes.length ||
      edgeIDs.size !== input.edges.length) {
    throw new Error('Invalid layout viewport');
  }
  const drawing = new dagre.graphlib.Graph({ multigraph: true })
    .setDefaultEdgeLabel(() => ({}))
    .setGraph({ rankdir: input.vertical ? 'TB' : 'LR', ranker: 'tight-tree',
      nodesep: 26, ranksep: input.vertical ? 54 : 96, marginx: 18, marginy: 18 });
  const nodes = input.nodes.toSorted(compareID);
  for (const id of nodes) drawing.setNode(id, { width: nodeWidth, height: nodeHeight });
  for (const [id, from, to] of input.edges.toSorted((a, b) => compareID(a[0], b[0]))) {
    if (!ids.has(from) || !ids.has(to)) throw new Error('Layout edge has an unknown endpoint');
    drawing.setEdge(from, to, {}, id);
  }
  if (nodes.length > 0) dagre.layout(drawing);
  return nodes.map(id => {
    const { x, y } = drawing.node(id);
    return { id, x: x - nodeWidth / 2, y: y - nodeHeight / 2 };
  });
}
