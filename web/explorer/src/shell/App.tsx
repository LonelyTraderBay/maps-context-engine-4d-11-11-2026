import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react';
import type { CSSProperties, FormEvent } from 'react';
import { createExplorerClient, ExplorerRequestError } from '../api/client.ts';
import type { ScopeVector, StatusData } from '../index.ts';
import { verifyScopeDigest } from '../index.ts';
import { GraphPane } from './GraphPane.tsx';
import { emptyGraph, hardNodes, loadGraph } from './map.ts';
import { selectionReducer, initialSelection } from './selection.ts';
import type { SelectionAction } from './selection.ts';
import { explain, SourcePane } from './SourcePane.tsx';
import { Splitter } from './Splitter.tsx';
import { PreviewPanel } from '../features/preview/PreviewPanel.tsx';
import { ManualMappings } from '../features/preview/ManualMappings.tsx';
import type { PreviewSelection } from '../features/preview/pairing.ts';

function Binding({ status, scope }: { status: StatusData | null; scope: ScopeVector | null }) {
  const member = scope?.members[0] ?? status?.registered_scopes[0];
  return <dl className="binding" aria-label="Phạm vi workspace">
    <div><dt>Worktree</dt><dd title={member?.worktree_id}>{member?.worktree_id.slice(0, 8) ?? '—'}</dd></div>
    <div><dt>Profile</dt><dd title={member?.profile_id ?? undefined}>{member?.profile_id?.slice(0, 8) ?? '—'}</dd></div>
    <div><dt>Generation</dt><dd title={scope?.members[0]?.generation_id}>{scope?.members[0]?.generation_id.slice(0, 8) ?? '—'}</dd></div>
  </dl>;
}

export function App() {
  const [client] = useState(createExplorerClient);
  const [connected, setConnected] = useState(false);
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [workspace, setWorkspace] = useState<StatusData | null>(null);
  const [scope, setScope] = useState<ScopeVector | null>(null);
  const [graph, setGraph] = useState(emptyGraph);
  const [width, setWidth] = useState(45);
  const [preview, setPreview] = useState(false);
  const [manualMappings, setManualMappings] = useState(false);
  const [selection, dispatch] = useReducer(selectionReducer, initialSelection);
  const work = useRef<AbortController | null>(null);
  const selectionOrder = useRef(0);

  function beginSelection() {
    const revision = ++selectionOrder.current;
    return () => revision === selectionOrder.current;
  }

  function manualSelection(action: SelectionAction) {
    selectionOrder.current++;
    dispatch(action);
  }

  useEffect(() => () => work.current?.abort(), []);

  const endSession = useCallback((message: string) => {
    work.current?.abort();
    client.disconnect();
    setConnected(false);
    setPreview(false);
    setManualMappings(false);
    setBusy(false);
    setError(message);
    setWorkspace(null);
    setScope(null);
    setGraph(emptyGraph);
    dispatch({ type: 'scope', digest: null });
  }, [client]);

  const sessionError = useCallback((reason: unknown) => {
    if (reason instanceof ExplorerRequestError && reason.status === 401) {
      endSession('Phiên Explorer đã hết hạn. Mở phiên mới từ terminal để tiếp tục.');
    }
  }, [endSession]);

  function begin(): AbortController {
    work.current?.abort();
    const job = new AbortController();
    work.current = job;
    setBusy(true);
    setError(null);
    return job;
  }

  function fromPreview(value: PreviewSelection) {
    if (value.scopeDigest !== scope?.vector_digest) return;
    const node = value.graphNode;
    if (!graph.nodes.some(item => item.node_id === node.node_id)) {
      if (busy || graph.nodes.length >= hardNodes) {
        setError('Anchor đã khớp nguồn. Đồ thị đang tải hoặc đã đầy.');
        return;
      }
      setGraph(current => current.nodes.some(item => item.node_id === node.node_id) ? current :
        { ...current, nodes: [...current.nodes, node] });
    }
    setError(null);
    dispatch({ type: 'preview', node: node.node_id, value });
  }

  async function readWorkspace(job: AbortController) {
    selectionOrder.current++;
    const response = await client.workspace(job.signal);
    const next = response.scope ?? null;
    if (!response.data) throw new Error('Thông tin workspace chưa khả dụng.');
    if (next) await verifyScopeDigest(next, 32768);
    job.signal.throwIfAborted();
    setWorkspace(response.data);
    setScope(next);
    dispatch({ type: 'scope', digest: next?.vector_digest ?? null });
    setGraph(emptyGraph);
    if (next && response.data.index_state === 'published') {
      const page = await loadGraph(client, next, job.signal, emptyGraph, setGraph);
      job.signal.throwIfAborted();
      setGraph(page);
    }
  }

  function failed(job: AbortController, reason: unknown) {
    if (job.signal.aborted) return;
    setError(explain(reason));
    sessionError(reason);
  }

  async function connect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !/^[A-Za-z0-9_-]{43}$/.test(code.trim())) return;
    const job = begin();
    const submitted = code.trim();
    let authenticated = false;
    setCode('');
    try {
      await client.connect(submitted, job.signal);
      authenticated = true;
      setConnected(true);
      await readWorkspace(job);
    } catch (reason) {
      if (authenticated) failed(job, reason);
      else if (!job.signal.aborted && reason instanceof ExplorerRequestError && reason.status === 401) {
        setError(reason.code === 'AUTH_EXPIRED' ? 'Mã đã hết hạn. Mở phiên Explorer mới từ terminal.' :
          'Mã không hợp lệ hoặc đã dùng. Kiểm tra mã trong terminal.');
      } else if (!job.signal.aborted) setError(explain(reason));
    } finally {
      if (!job.signal.aborted) setBusy(false);
    }
  }

  async function refresh() {
    const job = begin();
    try {
      await readWorkspace(job);
    } catch (reason) {
      failed(job, reason);
    } finally {
      if (!job.signal.aborted) setBusy(false);
    }
  }

  async function more() {
    if (!scope || !graph.cursor || busy) return;
    const job = begin();
    try {
      const page = await loadGraph(client, scope, job.signal, graph);
      job.signal.throwIfAborted();
      setGraph(page);
    } catch (reason) {
      failed(job, reason);
    } finally {
      if (!job.signal.aborted) setBusy(false);
    }
  }

  const chosen = selection.history[selection.position];
  const node = graph.nodes.find(item => item.node_id === chosen?.node) ?? null;
  const selectedSource = chosen?.mappingSource ?? chosen?.preview?.match.source_refs[0] ?? node?.source_refs[chosen?.source ?? 0];
  const relations = useMemo(() => node ? graph.edges.filter(edge =>
    edge.from_node_id === node.node_id || edge.to_node_id === node.node_id) : [], [graph.edges, node]);
  const statusText = !connected ? 'Ngắt kết nối' : busy && !workspace ? 'Đang tải' :
    workspace?.index_state === 'missing' ? 'Thiếu index' : graph.coverage?.status === 'partial' ? 'Index một phần' :
      workspace?.index_state === 'published' ? 'Đã lập chỉ mục' : 'Đã kết nối';

  return <main className="explorer" onKeyDown={event => {
    if (connected && event.key === 'Escape' && !(event.target instanceof HTMLInputElement) && chosen) {
      manualSelection({ type: 'clear' });
    }
  }}>
    <header className="toolbar">
      <div className="brand">
        <span className="brand-mark" aria-hidden="true"><i /><i /><i /></span>
        <div><p>Maps Context Engine</p><h1>Explorer</h1></div>
      </div>
      {connected && <Binding status={workspace} scope={scope} />}
      <div className="toolbar__actions">
        <span className={'status-mark' + (graph.coverage?.status === 'partial' ? ' status-mark--partial' :
          connected ? ' status-mark--online' : '')} role="status">{statusText}</span>
        {connected && <button className="quiet-button" type="button" disabled={busy} onClick={() => void refresh()}>Tải lại</button>}
        {connected && scope && client.previewEnabled() &&
          <button className="quiet-button" type="button" onClick={() => { setPreview(true); dispatch({ type: 'pane', value: 'right' }); }}>Preview</button>}
        {connected && client.recordsEnabled() &&
          <button className="quiet-button" type="button" aria-pressed={manualMappings}
            onClick={() => { setManualMappings(value => !value); dispatch({ type: 'pane', value: 'right' }); }}>Hồ sơ mapping</button>}
      </div>
    </header>
    {!connected ? <div className="connect">
      <form className="connect-card" onSubmit={event => void connect(event)}>
        <h2>Kết nối workspace</h2>
        <p>Nhập mã dùng một lần từ terminal đang chạy Explorer.</p>
        <label htmlFor="bootstrap-code">Mã bootstrap</label>
        <input id="bootstrap-code" type="password" autoComplete="off" spellCheck={false}
          value={code} onChange={event => setCode(event.currentTarget.value)} maxLength={64} required />
        <button className="primary-button" type="submit" disabled={busy || !/^[A-Za-z0-9_-]{43}$/.test(code.trim())}>
          {busy ? 'Đang kết nối…' : 'Kết nối'}
        </button>
        {error && <p className="notice notice--error" role="alert">{error}</p>}
      </form>
    </div> : <>
      <div className="workspace-bar">
        <div className="history" aria-label="Lịch sử lựa chọn">
          <button className="icon-button" type="button" aria-label="Lựa chọn trước" disabled={selection.position <= 0}
            onClick={() => manualSelection({ type: 'history', delta: -1 })}>←</button>
          <button className="icon-button" type="button" aria-label="Lựa chọn sau"
            disabled={selection.position + 1 >= selection.history.length}
            onClick={() => manualSelection({ type: 'history', delta: 1 })}>→</button>
        </div>
        <span className="workspace-bar__label">{node ? node.label || node.kind : 'Workspace đã chọn'}</span>
        <nav className="segmented compact-switch" aria-label="Chọn khung">
          <button type="button" aria-pressed={selection.compactPane === 'graph'}
            onClick={() => dispatch({ type: 'pane', value: 'graph' })}>Đồ thị</button>
          <button type="button" aria-pressed={selection.compactPane === 'right'}
            onClick={() => dispatch({ type: 'pane', value: 'right' })}>Nguồn</button>
        </nav>
      </div>
      <div className="workspace" data-pane={selection.compactPane}
        style={{ '--graph-share': width + '%' } as CSSProperties}>
        <GraphPane graph={graph} busy={busy} error={error} hasIndex={workspace?.index_state === 'published'}
          selected={chosen?.node ?? null} onChoose={value => manualSelection({ type: 'choose', node: value })}
          onMore={() => void more()} />
        <Splitter value={width} onChange={setWidth} />
        <div className="workspace-right">
          {preview && scope && client.previewEnabled() &&
            <PreviewPanel key={scope.vector_digest} client={client} scope={scope} close={() => setPreview(false)}
              onSelect={fromPreview} beginSelection={beginSelection} graphNode={node}
              onSource={(node, source) => dispatch({ type: 'mapping-range', node, source })} />}
          {manualMappings && client.recordsEnabled() &&
            <ManualMappings client={client} scope={scope} node={node} source={selectedSource} onChanged={refresh} onSessionError={sessionError} />}
          <SourcePane client={client} scope={scope} node={node} sourceIndex={chosen?.source ?? 0}
            detail={selection.detail} edges={relations}
            onRange={index => manualSelection({ type: 'range', index })}
            onDetail={value => dispatch({ type: 'detail', value })}
            onSessionError={sessionError} {...(chosen?.preview ? { preview: chosen.preview } : {})}
            {...(chosen?.mappingSource ? { mappingSource: chosen.mappingSource } : {})} />
        </div>
      </div>
    </>}
  </main>;
}
