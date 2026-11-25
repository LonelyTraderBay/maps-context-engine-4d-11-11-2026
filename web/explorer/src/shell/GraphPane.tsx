import { useMemo, useState } from 'react';
import { Background, BackgroundVariant, BaseEdge, EdgeLabelRenderer, getBezierPath, getSmoothStepPath, Handle, MarkerType, Position, ReactFlow } from '@xyflow/react';
import type { Edge, EdgeProps, Node, NodeProps, ReactFlowInstance } from '@xyflow/react';
import type { GraphNode } from '../index.ts';
import { edgeRelationLabel, traceFlowEdges } from './map.ts';
import type { DisplayEdge, GraphSlice, RelationDirection, RelationFilter } from './map.ts';
import { compareID, nodeHeight, nodeWidth } from '../features/graph/viewport.ts';
import { useLayout } from '../features/graph/useLayout.ts';

type ExplorerNode = Node<{ title: string; kind: string; boundary: boolean; path: string; vertical: boolean }, 'fact'>;

function FactNode({ data }: NodeProps<ExplorerNode>) {
  return <div className={'fact-node' + (data.boundary ? ' fact-node--boundary' : '')}>
    <Handle type="target" position={data.vertical ? Position.Top : Position.Left} isConnectable={false} />
    <button className="fact-node__pick nopan" type="button" aria-label={'Chọn ' + data.title + ', ' + data.kind}>
      <span className="fact-node__kind">{data.kind}</span>
      <span className="fact-node__label" title={data.title}>{data.title}</span>
    </button>
    <Handle type="source" position={data.vertical ? Position.Bottom : Position.Right} isConnectable={false} />
  </div>;
}

const nodeTypes = { fact: FactNode };

type FlowLane = { offset: number; bend: number };
type FlowRoute = { source?: FlowLane | undefined; target?: FlowLane | undefined };

function FlowEdge({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, markerEnd, label,
  data, style, labelStyle, labelBgStyle, interactionWidth }: EdgeProps) {
  const route = data?.route as FlowRoute | undefined;
  const startX = sourceX + (route?.source?.offset ?? 0);
  const endX = targetX + (route?.target?.offset ?? 0);
  const [path, centerX, centerY] = getSmoothStepPath({
    sourceX: startX, sourceY, targetX: endX, targetY, sourcePosition, targetPosition, offset: 7,
    stepPosition: 0.5 + (route?.source?.bend ?? route?.target?.bend ?? 0),
  });
  const labelX = route?.source ? endX : route?.target ? startX : centerX;
  const labelY = route?.source ? targetY - 12 : route?.target ? sourceY + 12 : centerY;
  return <BaseEdge path={path} label={label} labelX={labelX} labelY={labelY} style={style}
    labelStyle={labelStyle ?? {}} labelBgStyle={labelBgStyle ?? {}} interactionWidth={interactionWidth ?? 16}
    {...(markerEnd ? { markerEnd } : {})} />;
}

function IncomingEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, markerEnd,
  label, data, style, interactionWidth }: EdgeProps) {
  const targetOffset = typeof data?.targetOffset === 'number' ? data.targetOffset : 0;
  const [path] = getBezierPath({
    sourceX, sourceY, targetX: targetX + targetOffset, targetY: targetY - 4, sourcePosition, targetPosition,
  });
  const pick = data?.pick;
  return <>
    <BaseEdge path={path} style={style} interactionWidth={interactionWidth ?? 28} {...(markerEnd ? { markerEnd } : {})} />
    {typeof pick === 'function' && <EdgeLabelRenderer>
      <button className="impact-edge-label nopan nodrag" type="button"
        aria-label={String(data?.accessibleLabel)} aria-pressed={data?.active === true}
        style={{ transform: `translate(-50%, -50%) translate(${sourceX}px, ${sourceY + 21}px)`, color: style?.stroke }}
        onClick={event => { event.stopPropagation(); pick(id); }}>{label}</button>
    </EdgeLabelRenderer>}
  </>;
}

const edgeTypes = { incomingUsage: IncomingEdge, tracedFlow: FlowEdge };

export function nodeTitle(node: GraphNode): string {
  return node.label || (node.kind === 'unresolved_boundary' ? 'Tham chiếu chưa phân giải' : node.kind);
}

type Props = {
  graph: GraphSlice;
  displayEdges: DisplayEdge[];
  scope: string | null;
  busy: boolean;
  error: string | null;
  hasIndex: boolean;
  selected: string | null;
  selectedEdge: string | null;
  traceNode: string | null;
  onChoose: (node: string) => void;
  onChooseEdge: (edge: string) => void;
  onMore: () => void;
  filePath?: string | undefined;
  onBrowse?: (() => void) | undefined;
  onFocus?: (() => void) | undefined;
  onImpact?: (() => void) | undefined;
  onExpand?: (() => void) | undefined;
  neighborhood?: { id: string; label: string; direction: RelationDirection; filter: RelationFilter;
    change: (direction: RelationDirection, filter: RelationFilter) => void } | undefined;
};

const listBatch = 80;

const flowColors = new Map([
  ['ROUTE_COMPONENT', 'route'], ['RENDERS_COMPONENT', 'render'], ['RENDERS_JSX', 'display'],
  ['BINDS_EVENT', 'event'], ['CHECKS_CONDITION', 'condition'], ['BRANCH_TRUE', 'true'], ['BRANCH_FALSE', 'false'],
  ['CALLS_DIRECT', 'call'], ['CALLS_INTERFACE', 'interface-call'], ['READS', 'read'], ['WRITES', 'write'],
]);

function flowLanes(edges: DisplayEdge[], nodeX: ReadonlyMap<string, number>,
  at: 'from_node_id' | 'to_node_id', other: 'from_node_id' | 'to_node_id') {
  const lanes = new Map<string, FlowLane>();
  for (const group of Map.groupBy(edges, edge => edge[at]).values()) {
    if (group.length < 2) continue;
    group.sort((a, b) => (nodeX.get(a[other]) ?? 0) - (nodeX.get(b[other]) ?? 0) || compareID(a.edge_id, b.edge_id));
    const intervals = group.length - 1;
    const gap = Math.min(18, (nodeWidth - 32) / intervals);
    group.forEach((edge, index) => lanes.set(edge.edge_id, {
      offset: (index - intervals / 2) * gap,
      bend: (index / intervals - 0.5) * 0.44,
    }));
  }
  return lanes;
}

function edgeText(edge: DisplayEdge) {
  return edgeRelationLabel(edge) + (edge.evidence_refs.length > 1 ? ` (${edge.evidence_refs.length})` : '');
}

export function GraphPane({ graph, displayEdges, scope, busy, error, hasIndex, selected, selectedEdge, traceNode, onChoose,
  onChooseEdge, onMore, onBrowse, onFocus, onImpact, onExpand, filePath, neighborhood }: Props) {
  const [view, setView] = useState<'canvas' | 'list'>('canvas');
  const [filter, setFilter] = useState('');
  const [grouped, setGrouped] = useState(false);
  const [shown, setShown] = useState(listBatch);
  const [instance, setInstance] = useState<ReactFlowInstance<ExplorerNode, Edge> | null>(null);
  const query = filter.trim().toLocaleLowerCase('vi');
  const catalog = graph.coverage?.gaps.some(gap => gap.code === 'GRAPH_PROJECTION_LIMIT') ?? false;
  const impact = neighborhood?.direction === 'incoming' && neighborhood.filter === 'impact';
  const functionalFlow = neighborhood?.filter === 'functional';
  const focus = impact ? neighborhood.id : selected;
  const vertical = Boolean(neighborhood);
  const layout = useLayout(graph, scope, !catalog && view === 'canvas' && query === '', vertical, displayEdges);
  const positions = layout && 'positions' in layout ? layout.positions : null;
  const list = catalog || view === 'list' || query !== '' || (layout !== null && 'error' in layout);
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
        ariaLabel: fact.title + ', ' + fact.kind, data: { ...fact, vertical }, selected: selected === id,
      }] : [];
    });
  }, [facts, positions, selected, vertical]);
  const titles = useMemo(() => new Map(facts.map(fact => [fact.id, fact.title])), [facts]);
  const visibleEdges = useMemo(() => displayEdges.filter(edge => titles.has(edge.from_node_id) && titles.has(edge.to_node_id)),
    [displayEdges, titles]);
  const trace = useMemo(() => functionalFlow && traceNode !== null ? traceFlowEdges(visibleEdges, traceNode) : null,
    [functionalFlow, traceNode, visibleEdges]);
  const routes = useMemo(() => {
    if (!functionalFlow || !positions) return null;
    const x = new Map(positions.map(position => [position.id, position.x]));
    return { source: flowLanes(visibleEdges, x, 'from_node_id', 'to_node_id'),
      target: flowLanes(visibleEdges, x, 'to_node_id', 'from_node_id') };
  }, [functionalFlow, positions, visibleEdges]);
  const edges = useMemo<Edge[]>(() => {
    const offsets = new Map<string, number>();
    if (impact && positions) {
      // Keep the arrowheads in the same order as their sources along the target node.
      const x = new Map(positions.map(position => [position.id, position.x]));
      const incoming = visibleEdges.filter(edge => edge.to_node_id === focus)
        .sort((a, b) => (x.get(a.from_node_id) ?? 0) - (x.get(b.from_node_id) ?? 0) || compareID(a.edge_id, b.edge_id));
      const intervals = incoming.length - 1;
      const gap = Math.min(28, (nodeWidth - 32) / Math.max(1, intervals));
      incoming.forEach((edge, index) => offsets.set(edge.edge_id, (index - intervals / 2) * gap));
    }
    return visibleEdges.map(edge => {
      const role = selectedEdge === edge.edge_id ? 'selected' :
        functionalFlow ? trace === null ? 'neutral' : trace.has(edge.edge_id) ? 'trace' : 'dimmed' :
        edge.to_node_id === focus ? 'incoming' : edge.from_node_id === focus ? 'outgoing' : 'neutral';
      const relationColor = edge.accessKinds?.includes('READS') && edge.accessKinds.includes('WRITES')
        ? 'read-write' : flowColors.get(edge.relation_kind) ?? 'neutral';
      const base = `var(--graph-edge-${functionalFlow && role !== 'selected' ? relationColor : role})`;
      const color = role === 'dimmed' ? `color-mix(in srgb, ${base} 36%, var(--canvas))` : base;
      const width = role === 'selected' ? 3.5 : role === 'neutral' ? (functionalFlow ? 1.8 : 1.4) :
        role === 'dimmed' ? 1.3 : 2.5;
      const accessibleLabel = `${titles.get(edge.from_node_id)} → ${titles.get(edge.to_node_id)} · ${edgeText(edge)}`;
      return {
        id: edge.edge_id, source: edge.from_node_id, target: edge.to_node_id,
        type: functionalFlow ? 'tracedFlow' : impact && edge.to_node_id === focus ? 'incomingUsage' : 'smoothstep',
        className: (edge.resolution_status === 'exact_in_supported_scope' ? 'exact-edge' : 'evidence-edge') + ` edge-${role}`,
        style: { stroke: color, strokeWidth: width },
        zIndex: role === 'selected' ? 2 : role === 'trace' ? 1 : 0,
        markerEnd: { type: MarkerType.ArrowClosed, color, width: 15, height: 15 },
        label: neighborhood || role !== 'neutral' ? edgeText(edge) : undefined,
        labelStyle: { fill: role === 'dimmed' ? 'var(--graph-edge-dimmed-label)' : color,
          fontSize: 12, fontWeight: role === 'selected' ? 700 : 500 },
        labelBgStyle: { fill: 'var(--surface)' },
        interactionWidth: functionalFlow ? 16 : 28,
        ariaLabel: accessibleLabel,
        data: { pick: onChooseEdge, accessibleLabel, active: role === 'selected', targetOffset: offsets.get(edge.edge_id),
          route: { source: routes?.source.get(edge.edge_id), target: routes?.target.get(edge.edge_id) } },
        focusable: false,
      };
    });
  }, [visibleEdges, titles, neighborhood, selectedEdge, focus, impact, positions, onChooseEdge, functionalFlow, trace, routes]);
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
        <h2 id="graph-heading">{impact ? 'Ảnh hưởng khi sửa' : neighborhood ? 'Luồng chức năng' : filePath ? 'Đồ thị tệp' : catalog ? 'Danh mục ký hiệu' : 'Đồ thị'}</h2>
        {hasGraph && <span className="pane-count">{graph.nodes.length} {catalog ? 'ký hiệu đã tải' : `nút · ${displayEdges.length} cạnh`}</span>}
      </div>
      {hasGraph && <div className="segmented" aria-label="Chế độ hiển thị đồ thị">
        <button type="button" disabled={catalog} aria-pressed={!list} onClick={() => { setFilter(''); setView('canvas'); }}>Bản đồ</button>
        <button type="button" aria-pressed={list} onClick={() => setView('list')}>Danh sách</button>
      </div>}
    </header>
    {onBrowse && <nav className="directory-path" aria-label="Phạm vi đồ thị">
      <button className="quiet-button" type="button" disabled={busy} onClick={onBrowse}>Thư mục</button>
      {filePath && <span className="directory-current" title={filePath}>{filePath}</span>}
      {neighborhood && <span className="directory-current" title={neighborhood.label}>{neighborhood.label}</span>}
    </nav>}
    {neighborhood && <div className="graph-tools neighborhood-controls">
      <label>Hướng <select value={neighborhood.direction} disabled={busy}
        onChange={event => neighborhood.change(event.currentTarget.value as RelationDirection, neighborhood.filter)}>
        <option value="outgoing">Đi tiếp đâu?</option>
        <option value="incoming">Ai nối tới?</option>
        <option value="both">Hai chiều</option>
      </select></label>
      <label>Quan hệ <select value={neighborhood.filter} disabled={busy}
        onChange={event => neighborhood.change(neighborhood.direction, event.currentTarget.value as RelationFilter)}>
        <option value="functional">Chức năng</option><option value="calls">Lời gọi</option>
        <option value="imports">Import và tham chiếu</option><option value="variables">Đọc và ghi biến</option>
        <option value="structure">Cấu trúc chứa</option><option value="impact">Các quan hệ ảnh hưởng</option>
      </select></label>
      {onExpand && <button className="quiet-button" type="button" disabled={busy} onClick={onExpand}>Mở thêm từ nút chọn</button>}
      {onImpact && (!impact || selected !== focus) && <button className="quiet-button" type="button" disabled={busy} onClick={onImpact}>Xem ảnh hưởng</button>}
    </div>}
    {hasGraph && <div className="graph-tools">
      <input type="search" aria-label="Lọc ký hiệu" placeholder="Lọc trong các nút đã tải…" value={filter}
        onChange={event => { setFilter(event.currentTarget.value); setShown(listBatch); }} />
      {list && <button className="quiet-button" type="button" aria-pressed={grouped}
        onClick={() => setGrouped(value => !value)}>Nhóm theo tệp</button>}
      {!list && <button className="quiet-button" type="button" disabled={!positions}
        onClick={() => void instance?.fitView({ padding: 0.18 })}>Vừa khung</button>}
      {!list && neighborhood && <>
        <button className="icon-button" type="button" aria-label="Thu nhỏ luồng" disabled={!positions}
          onClick={() => void instance?.zoomOut()}>−</button>
        <button className="icon-button" type="button" aria-label="Phóng to luồng" disabled={!positions}
          onClick={() => void instance?.zoomIn()}>+</button>
      </>}
      {onFocus && !neighborhood && <button className="quiet-button" type="button" disabled={busy} onClick={onFocus}>Xem luồng từ nút</button>}
      {onImpact && !neighborhood && <button className="quiet-button" type="button" disabled={busy} onClick={onImpact}>Xem ảnh hưởng</button>}
    </div>}
    {impact && <p className="impact-summary" role="status">
      Các nút có thể bị ảnh hưởng khi sửa <strong>{neighborhood.label}</strong>. Kết quả dựa trên quan hệ tĩnh đã lập chỉ mục.
    </p>}
    {displayEdges.length > 0 && (focus || selectedEdge) && <ul className="edge-legend" aria-label="Chú giải mũi tên">
      <li className="edge-legend-selected">Cạnh đang chọn</li>
      {functionalFlow ? <>
        <li>Tuyến qua nút đang xét</li>
        <li className="edge-legend-dimmed">Tuyến khác</li>
      </> : <>
        <li className="edge-legend-incoming">Nối tới nút đang xét</li>
        <li className="edge-legend-outgoing">Đi ra từ nút đang xét</li>
      </>}
    </ul>}
    {error && <p className="notice notice--error" role="alert">{error}</p>}
    {layout && 'error' in layout && <p className="notice notice--partial" role="status">Bố cục chưa khả dụng. Có thể chọn ký hiệu trong danh sách.</p>}
    {graph.coverage?.status === 'partial' && <div className="notice notice--partial">
      <span>{neighborhood ? 'Quan hệ tĩnh từ mã nguồn' : filePath ? 'Quan hệ trong tệp' : catalog ? 'Danh mục có phân trang' : 'Index chỉ bao phủ một phần.'}</span>
      {graph.coverage.gaps.length > 0 && <details className="coverage-details">
        <summary>Giới hạn đã ghi nhận</summary>
        <ul>{graph.coverage.gaps.map((gap, index) =>
          <li key={gap.code + index}><strong>{gap.code}</strong> {gap.reason}</li>)}</ul>
      </details>}
    </div>}
    {neighborhood && displayEdges.length === 0 && !busy &&
      <p className="notice" role="status">Chưa có quan hệ phù hợp. Có thể thử hướng hoặc loại quan hệ khác.</p>}
    <div className={'graph-content' + (hasGraph && !list ? ' graph-content--canvas' : '')}>
      {!hasGraph && <div className="empty">
        <strong>{busy ? 'Đang đọc index…' : error ? 'Đồ thị chưa khả dụng' : hasIndex ? 'Snapshot chưa có ký hiệu' : 'Workspace chưa có index'}</strong>
        {!busy && !hasIndex && !error && <p>Lập chỉ mục worktree đã chọn, rồi tải lại workspace.</p>}
      </div>}
      {hasGraph && list && (visible.length === 0 ? <div className="empty">Không có ký hiệu phù hợp trong phần đã tải.</div> : <>
        {grouped ? <div className="symbol-groups">{[...groups].map(([path, items]) => <details key={path}>
          <summary>{path} · {items.length} ký hiệu</summary>
          {symbolList(items)}
        </details>)}</div> : symbolList(displayed)}
        {displayed.length < visible.length && <div className="graph-tools">
          <button className="quiet-button" type="button" onClick={() => setShown(value => value + listBatch)}>
            Hiện thêm ký hiệu ({visible.length - displayed.length})
          </button>
        </div>}
        {!query && displayEdges.length > 0 && <section className="relation-choices" aria-label="Quan hệ có mã nguồn">
          <h3>Quan hệ</h3>
          <ul className="symbol-list">
            {displayEdges.map(edge => <li key={edge.edge_id}>
              <button type="button" className="symbol-row relation-choice" aria-pressed={selectedEdge === edge.edge_id} onClick={() => onChooseEdge(edge.edge_id)}>
                <span className="symbol-row__title">{titles.get(edge.from_node_id)} → {titles.get(edge.to_node_id)}</span>
                <span className="symbol-row__kind">{edgeText(edge)}</span>
              </button>
            </li>)}
          </ul>
        </section>}
      </>)}
      {hasGraph && !list && !positions && <p className="notice" role="status">Đang sắp xếp đồ thị…</p>}
      {hasGraph && !list && positions && <ReactFlow<ExplorerNode, Edge> className="explorer-flow" forceColorMode="dark"
        nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} fitView minZoom={0.05} maxZoom={2}
        nodesDraggable={false} nodesConnectable={false} edgesReconnectable={false}
        deleteKeyCode={null} elevateNodesOnSelect={false} nodesFocusable={false} elementsSelectable={false}
        onInit={setInstance} onNodeClick={(_event, node) => onChoose(node.id)}
        onEdgeClick={(_event, edge) => onChooseEdge(edge.id)}>
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
