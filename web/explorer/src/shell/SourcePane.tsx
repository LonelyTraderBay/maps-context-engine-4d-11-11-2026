import { useEffect, useState } from 'react';
import type { ExplorerClient } from '../api/client.ts';
import { utf8, verifyScopeDigest } from '../index.ts';
import type { GraphEdge, GraphNode, ScopeVector, SourceRef } from '../index.ts';
import { nodeTitle } from './GraphPane.tsx';
import type { DetailView } from './selection.ts';

type SourceState = { key: string; content: string } | { key: string; error: string };

export function explain(error: unknown): string {
  return error instanceof Error ? error.message : 'Explorer gặp lỗi khi đọc index.';
}

function sourceKey(ref: SourceRef): string {
  return JSON.stringify([ref.worktree_id, ref.profile_id, ref.generation_id, ref.path, ref.content_hash,
    ref.range.start_byte, ref.range.end_byte]);
}

type Props = {
  client: ExplorerClient;
  scope: ScopeVector | null;
  node: GraphNode | null;
  sourceIndex: number;
  detail: DetailView;
  edges: GraphEdge[];
  onRange: (index: number) => void;
  onDetail: (view: DetailView) => void;
  onSessionError: (error: unknown) => void;
};

export function SourcePane({ client, scope, node, sourceIndex, detail, edges, onRange, onDetail, onSessionError }: Props) {
  const ref = node?.source_refs[sourceIndex];
  const key = ref && scope ? scope.vector_digest + sourceKey(ref) : null;
  const [state, setState] = useState<SourceState | null>(null);
  const current = state?.key === key ? state : null;

  useEffect(() => {
    if (!ref || !scope || !key) return;
    const request = new AbortController();
    async function read(bound: ScopeVector, wanted: SourceRef, key: string) {
      try {
        const response = await client.source({ source_ref: wanted }, request.signal);
        if (!response.data || !response.scope || response.scope.vector_digest !== bound.vector_digest ||
            sourceKey(response.data.source_ref) !== sourceKey(wanted) ||
            utf8.encode(response.data.content).length !== wanted.range.end_byte - wanted.range.start_byte) {
          throw new Error('Nguồn trả về không khớp lựa chọn.');
        }
        await verifyScopeDigest(response.scope, 32768);
        request.signal.throwIfAborted();
        setState({ key, content: response.data.content });
      } catch (error) {
        if (!request.signal.aborted) {
          setState({ key, error: explain(error) });
          onSessionError(error);
        }
      }
    }
    void read(scope, ref, key);
    return () => request.abort();
  }, [client, key, onSessionError, ref, scope]);

  return <section className="pane source-pane" id="source-pane" aria-labelledby="source-heading">
    <header className="pane-heading">
      <h2 id="source-heading">Kiểm tra ký hiệu</h2>
      <div className="segmented" aria-label="Nội dung khung phải">
        <button type="button" aria-pressed={detail === 'source'} onClick={() => onDetail('source')}>Mã nguồn</button>
        <button type="button" aria-pressed={detail === 'details'} onClick={() => onDetail('details')}>Chi tiết</button>
      </div>
    </header>
    {!node && <div className="empty">
      <strong>Chưa chọn ký hiệu</strong>
      <p>Chọn một nút trong đồ thị hoặc danh sách để đọc bằng chứng.</p>
    </div>}
    {node && <div className="inspector">
      <div className="inspector-heading">
        <span className="kind-label">{node.kind}</span>
        <h3>{nodeTitle(node)}</h3>
      </div>
      {detail === 'source' && <>
        {node.source_refs.length > 1 && <div className="ref-list" aria-label="Vùng mã nguồn">
          {node.source_refs.map((range, index) => <button type="button" key={sourceKey(range) + index}
            className="quiet-button" aria-pressed={sourceIndex === index} onClick={() => onRange(index)}>
            {range.path} · {range.range.start_byte}
          </button>)}
        </div>}
        {!ref ? <p className="notice">Index chưa có vùng nguồn cho ký hiệu này.</p> :
          <div className="source-card">
            <header className="source-card__head">
              <span className="file-path">{ref.path}</span>
              <span className="snapshot-label">Snapshot đã lập chỉ mục</span>
            </header>
            <div className="source-meta">
              <span>Byte [{ref.range.start_byte}, {ref.range.end_byte})</span>
              <span title={ref.content_hash} className="digest">SHA-256 {ref.content_hash.slice(0, 12)}</span>
            </div>
            {current && 'content' in current ?
              <pre className="source-code" aria-label="Mã nguồn của snapshot" tabIndex={0}><code>{current.content}</code></pre> :
              current && 'error' in current ? <p className="notice notice--error" role="alert">{current.error}</p> :
                <p className="notice" role="status">Đang đọc vùng mã nguồn…</p>}
          </div>}
      </>}
      {detail === 'details' && <div className="detail-content">
        <dl className="metadata">
          <div><dt>Loại tham chiếu</dt><dd>{node.ref.kind}</dd></div>
          <div><dt>Bằng chứng</dt><dd>{node.plane === 'observed_static' ? 'Phân tích tĩnh' : node.plane}</dd></div>
          <div><dt>Vùng mã nguồn</dt><dd>{node.source_refs.length}</dd></div>
        </dl>
        <h4>Quan hệ trong lát đồ thị</h4>
        {edges.length === 0 ? <p>Trang hiện tại chưa có cạnh cho ký hiệu này.</p> : <ul className="relation-list">
          {edges.map(edge => <li key={edge.edge_id}>
            <span>{edge.relation_kind}</span><span>{edge.resolution_status}</span>
          </li>)}
        </ul>}
      </div>}
    </div>}
  </section>;
}
