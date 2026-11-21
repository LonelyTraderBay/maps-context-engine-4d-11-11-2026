import { useMemo, useState } from 'react';
import { Background, BackgroundVariant, Handle, MarkerType, Position, ReactFlow } from '@xyflow/react';
import type { Edge, Node, NodeProps, ReactFlowInstance } from '@xyflow/react';
import type { GraphNode } from '../index.ts';
import type { GraphSlice } from './map.ts';
import { compareID, nodeHeight, nodeWidth } from '../features/graph/viewport.ts';
import { useLayout } from '../features/graph/useLayout.ts';

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

export function nodeTitle(node: GraphNode): string {
  return node.label || (node.kind === 'unresolved_boundary' ? 'Tham chiếu chưa phân giải' : node.kind);
}

type Props = {
  graph: GraphSlice;
  scope: string | null;
  busy: boolean;
  error: string | null;
  hasIndex: boolean;
  selected: string | null;
  onChoose: (node: string) => void;
  onMore: () => void;
};

const listBatch = 80;

export function GraphPane({ graph, scope, busy, error, hasIndex, selected, onChoose, onMore }: Props) {
  const [view, setView] = useState<'canvas' | 'list'>('canvas');
  const [filter, setFilter] = useState('');
  const [grouped, setGrouped] = useState(false);
  const [shown, setShown] = useState(listBatch);
  const [instance, setInstance] = useState<ReactFlowInstance<ExplorerNode, Edge> | null>(null);
  const query = filter.trim().toLocaleLowerCase('vi');
  const layout = useLayout(graph, scope, view === 'canvas' && query === '');
  const positions = layout && 'positions' in layout ? layout.positions : null;
  const list = view === 'list' || query !== '' || (layout !== null && 'error' in layout);
  const facts = useMemo(() => graph.nodes.map(node => ({
    id: node.node_id, title: nodeTitle(node), kind: node.kind, boundary: node.plane === 'annotation',
    path: node.source_refs[0]?.path ?? '',
  })).sort((a, b) => compareID(a.path, b.path) || compareID(a.title, b.title) || compareID(a.id, b.id)), [graph.nodes]);
  const nodes = useMemo<ExplorerNode[]>(() => {
    if (!positions) return [];
    const lookup = new Map(facts.map(fact => [fact.id, fact]));
    return positions.flatMap(({ id, x, y }) => {
      const fact = lookup.get(id);
      return fact ? [{
        id, type: 'fact', position: { x, y }, width: nodeWidth, height: nodeHeight,
        ariaLabel: fact.title + ', ' + fact.kind, data: fact, selected: selected === id,
      }] : [];
    });
  }, [facts, positions, selected]);
  const edges = useMemo<Edge[]>(() => {
    const ids = new Set(facts.map(fact => fact.id));
    return graph.edges.filter(edge => ids.has(edge.from_node_id) && ids.has(edge.to_node_id)).map(edge => ({
      id: edge.edge_id, source: edge.from_node_id, target: edge.to_node_id, type: 'smoothstep',
      className: edge.resolution_status === 'exact_in_supported_scope' ? 'exact-edge' : 'evidence-edge',
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
      ariaLabel: edge.relation_kind + ' · ' + edge.resolution_status, focusable: false,
    }));
  }, [facts, graph.edges]);
  const visible = facts.filter(fact => !query ||
    [fact.title, fact.kind, fact.path].some(value => value.toLocaleLowerCase('vi').includes(query)));
  const displayed = visible.slice(0, shown);
  const groups = Map.groupBy(displayed, item => item.path || 'Không có tệp nguồn');
  const hasGraph = graph.nodes.length > 0;

  function symbolList(items: typeof displayed) {
    return <ul className="symbol-list" aria-label="Ký hiệu trong đồ thị">
      {items.map(item => <li key={item.id}>
        <button type="button" className="symbol-row" aria-pressed={selected === item.id} onClick={() => onChoose(item.id)}>
          <span className="symbol-row__title">{item.title}</span>
          <span className="symbol-row__path">{item.path}</span>
          <span className="symbol-row__kind">{item.kind}</span>
        </button>
      </li>)}
    </ul>;
  }

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
        onChange={event => { setFilter(event.currentTarget.value); setShown(listBatch); }} />
      {list && <button className="quiet-button" type="button" aria-pressed={grouped}
        onClick={() => setGrouped(value => !value)}>Nhóm theo tệp</button>}
      {!list && <button className="quiet-button" type="button" disabled={!positions}
        onClick={() => void instance?.fitView({ padding: 0.18 })}>Vừa khung</button>}
    </div>}
    {error && <p className="notice notice--error" role="alert">{error}</p>}
    {layout && 'error' in layout && <p className="notice notice--partial" role="status">Bố cục chưa khả dụng. Có thể chọn ký hiệu trong danh sách.</p>}
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
      {hasGraph && list && (visible.length === 0 ? <div className="empty">Không có ký hiệu phù hợp.</div> : <>
        {grouped ? <div className="symbol-groups">{[...groups].map(([path, items]) => <details key={path}>
          <summary>{path} · {items.length} ký hiệu</summary>
          {symbolList(items)}
        </details>)}</div> : symbolList(displayed)}
        {displayed.length < visible.length && <div className="graph-tools">
          <button className="quiet-button" type="button" onClick={() => setShown(value => value + listBatch)}>
            Hiện thêm ký hiệu ({visible.length - displayed.length})
          </button>
        </div>}
      </>)}
      {hasGraph && !list && !positions && <p className="notice" role="status">Đang sắp xếp đồ thị…</p>}
      {hasGraph && !list && positions && <ReactFlow<ExplorerNode, Edge> className="explorer-flow" forceColorMode="dark"
        nodes={nodes} edges={edges} nodeTypes={nodeTypes} fitView minZoom={0.05} maxZoom={2}
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
