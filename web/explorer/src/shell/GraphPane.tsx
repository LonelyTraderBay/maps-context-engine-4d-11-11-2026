import { useMemo, useState } from 'react';
import { Background, BackgroundVariant, Handle, MarkerType, Position, ReactFlow } from '@xyflow/react';
import type { Edge, Node, NodeProps, ReactFlowInstance } from '@xyflow/react';
import dagre from '@dagrejs/dagre';
import type { GraphNode } from '../index.ts';
import type { GraphSlice } from './map.ts';

const width = 178;
const height = 58;

type ExplorerNode = Node<{ title: string; kind: string; boundary: boolean; path: string }, 'fact'>;

function FactNode({ data }: NodeProps<ExplorerNode>) {
  return <div className={'fact-node' + (data.boundary ? ' fact-node--boundary' : '')}>
    <Handle type="target" position={Position.Left} isConnectable={false} />
    <button className="fact-node__pick nopan" type="button" aria-label={'Chọn ' + data.title + ', ' + data.kind}>
      <span className="fact-node__kind">{data.kind}</span>
      <span className="fact-node__label" title={data.title}>{data.title}</span>
    </button>
    <Handle type="source" position={Position.Right} isConnectable={false} />
  </div>;
}

const nodeTypes = { fact: FactNode };

function layout(graph: GraphSlice): { nodes: ExplorerNode[]; edges: Edge[] } {
  const drawing = new dagre.graphlib.Graph({ multigraph: true })
    .setDefaultEdgeLabel(() => ({}))
    .setGraph({ rankdir: 'LR', nodesep: 26, ranksep: 96, marginx: 18, marginy: 18 });
  const facts = graph.nodes.toSorted((a, b) => a.node_id.localeCompare(b.node_id));
  const links = graph.edges.toSorted((a, b) => a.edge_id.localeCompare(b.edge_id));
  for (const node of facts) drawing.setNode(node.node_id, { width, height });
  for (const edge of links) drawing.setEdge(edge.from_node_id, edge.to_node_id, {}, edge.edge_id);
  if (facts.length > 0) dagre.layout(drawing);
  return {
    nodes: facts.map(fact => {
      const { x, y } = drawing.node(fact.node_id);
      const title = nodeTitle(fact);
      return {
        id: fact.node_id, type: 'fact', position: { x: x - width / 2, y: y - height / 2 }, width, height,
        ariaLabel: title + ', ' + fact.kind,
        data: { title, kind: fact.kind, boundary: fact.plane === 'annotation',
          path: fact.source_refs[0]?.path ?? '' },
      };
    }),
    edges: links.map(edge => ({
      id: edge.edge_id, source: edge.from_node_id, target: edge.to_node_id, type: 'smoothstep',
      className: edge.resolution_status === 'exact_in_supported_scope' ? 'exact-edge' : 'evidence-edge',
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
      ariaLabel: edge.relation_kind + ' · ' + edge.resolution_status,
      focusable: false,
    })),
  };
}

export function nodeTitle(node: GraphNode): string {
  return node.label || (node.kind === 'unresolved_boundary' ? 'Tham chiếu chưa phân giải' : node.kind);
}

type Props = {
  graph: GraphSlice;
  busy: boolean;
  error: string | null;
  hasIndex: boolean;
  selected: string | null;
  onChoose: (node: string) => void;
  onMore: () => void;
};

export function GraphPane({ graph, busy, error, hasIndex, selected, onChoose, onMore }: Props) {
  const [view, setView] = useState<'canvas' | 'list'>('canvas');
  const [filter, setFilter] = useState('');
  const [instance, setInstance] = useState<ReactFlowInstance<ExplorerNode, Edge> | null>(null);
  const drawing = useMemo(() => layout(graph), [graph]);
  const nodes = useMemo(() => drawing.nodes.map(node => ({ ...node, selected: selected === node.id })),
    [drawing.nodes, selected]);
  const query = filter.trim().toLocaleLowerCase('vi');
  const list = view === 'list' || query !== '';
  const visible = drawing.nodes.filter(node => !query ||
    [node.data.title, node.data.kind, node.data.path].some(text => text.toLocaleLowerCase('vi').includes(query)));
  const hasGraph = graph.nodes.length > 0;

  return <section className="pane graph-pane" id="graph-pane" aria-labelledby="graph-heading">
    <header className="pane-heading">
      <div className="pane-heading__title">
        <h2 id="graph-heading">Đồ thị</h2>
        {hasGraph && <span className="pane-count">{graph.nodes.length} nút · {graph.edges.length} cạnh</span>}
      </div>
      {hasGraph && <div className="segmented" aria-label="Chế độ hiển thị đồ thị">
        <button type="button" aria-pressed={!list} onClick={() => { setFilter(''); setView('canvas'); }}>Bản đồ</button>
        <button type="button" aria-pressed={list} onClick={() => setView('list')}>Danh sách</button>
      </div>}
    </header>
    {hasGraph && <div className="graph-tools">
      <input type="search" aria-label="Lọc ký hiệu" placeholder="Lọc tên, loại hoặc tệp…" value={filter}
        onChange={event => setFilter(event.currentTarget.value)} />
      {!list && <button className="quiet-button" type="button" onClick={() => void instance?.fitView({ padding: 0.18 })}>Vừa khung</button>}
    </div>}
    {error && <p className="notice notice--error" role="alert">{error}</p>}
    {graph.coverage?.status === 'partial' && <div className="notice notice--partial">
      <span>Index chỉ bao phủ một phần.</span>
      {graph.coverage.gaps.length > 0 && <details className="coverage-details">
        <summary>Giới hạn đã ghi nhận</summary>
        <ul>{graph.coverage.gaps.map((gap, index) =>
          <li key={gap.code + index}><strong>{gap.code}</strong> {gap.reason}</li>)}</ul>
      </details>}
    </div>}
    <div className={'graph-content' + (hasGraph && !list ? ' graph-content--canvas' : '')}>
      {!hasGraph && <div className="empty">
        <strong>{busy ? 'Đang đọc index…' : error ? 'Đồ thị chưa khả dụng' : hasIndex ? 'Snapshot chưa có ký hiệu' : 'Workspace chưa có index'}</strong>
        {!busy && !hasIndex && !error && <p>Lập chỉ mục worktree đã chọn, rồi tải lại workspace.</p>}
      </div>}
      {hasGraph && list && (visible.length === 0 ? <div className="empty">Không có ký hiệu phù hợp.</div> :
        <ul className="symbol-list" aria-label="Ký hiệu trong đồ thị">
          {visible.map(item => <li key={item.id}>
            <button type="button" className="symbol-row" aria-pressed={selected === item.id} onClick={() => onChoose(item.id)}>
              <span className="symbol-row__title">{item.data.title}</span>
              <span className="symbol-row__path">{item.data.path}</span>
              <span className="symbol-row__kind">{item.data.kind}</span>
            </button>
          </li>)}
        </ul>)}
      {hasGraph && !list && <ReactFlow<ExplorerNode, Edge> className="explorer-flow" forceColorMode="dark"
        nodes={nodes} edges={drawing.edges} nodeTypes={nodeTypes} fitView minZoom={0.05} maxZoom={2}
        nodesDraggable={false} nodesConnectable={false} edgesReconnectable={false}
        deleteKeyCode={null} elevateNodesOnSelect={false} nodesFocusable={false} elementsSelectable={false}
        onInit={setInstance} onNodeClick={(_event, node) => onChoose(node.id)}>
        <Background variant={BackgroundVariant.Dots} gap={19} size={1} color="var(--dot)" />
      </ReactFlow>}
    </div>
    {graph.cursor !== null && <footer className="pane-footer">
      <span>{graph.viewportFull ? 'Đã đạt giới hạn hiển thị.' :
        `Còn ${graph.pendingNodes ?? 'chưa rõ số'} nút · ${graph.pendingEdges ?? 'chưa rõ số'} cạnh`}</span>
      {!graph.viewportFull && <button className="quiet-button" type="button" disabled={busy} onClick={onMore}>Tải trang tiếp</button>}
    </footer>}
  </section>;
}
