import { useEffect, useState } from 'react';
import type { ExplorerClient } from '../api/client.ts';
import { readPinnedSource, sourceKey } from '../api/source.ts';
import type { GraphEdge, GraphNode, ScopeVector, SourceRef } from '../index.ts';
import { nodeTitle } from './GraphPane.tsx';
import { edgeRelationLabel, relationLabel } from './map.ts';
import type { DisplayEdge } from './map.ts';
import type { DetailView } from './selection.ts';
import type { PreviewSelection } from '../features/preview/pairing.ts';

type SourceState = { key: string; content: string } | { key: string; error: string };

export function explain(error: unknown): string {
  return error instanceof Error ? error.message : 'Explorer gặp lỗi khi đọc index.';
}

type Props = {
  client: ExplorerClient;
  scope: ScopeVector | null;
  node: GraphNode | null;
  edge?: DisplayEdge | undefined;
  edgeEndpoints?: string[] | undefined;
  sourceIndex: number;
  detail: DetailView;
  edges: GraphEdge[];
  onRange: (index: number) => void;
  onDetail: (view: DetailView) => void;
  onSessionError: (error: unknown) => void;
  preparedSource: string | null;
  preview?: PreviewSelection;
  mappingSource?: SourceRef;
};

export function SourcePane({ client, scope, node, edge, edgeEndpoints, sourceIndex, detail, edges, onRange, onDetail, onSessionError,
  preview, mappingSource, preparedSource }: Props) {
  const refs = edge ? edge.evidence_refs.flatMap(evidence => evidence.source_refs) : node?.source_refs ?? [];
  const access = edge?.evidence_refs.flatMap((evidence, index) =>
    evidence.source_refs.map(() => edge.accessKinds?.[index])) ?? [];
  const ref = edge ? refs[sourceIndex] : mappingSource ?? preview?.match.source_refs[0] ?? refs[sourceIndex];
  const contiguous = refs.length > 1 && refs.slice(1).every((range, index) => {
    const previous = refs[index];
    return previous?.path === range.path && previous.content_hash === range.content_hash &&
      previous.range.end_byte === range.range.start_byte;
  });
  const key = ref && scope ? scope.vector_digest + sourceKey(ref) : null;
  const [state, setState] = useState<SourceState | null>(null);
  const current = key && preparedSource !== null ? { key, content: preparedSource } : state?.key === key ? state : null;

  useEffect(() => {
    if (!ref || !scope || !key || preparedSource !== null) return;
    const request = new AbortController();
    async function read(bound: ScopeVector, wanted: SourceRef, key: string) {
      try {
        const content = await readPinnedSource(client, bound, wanted, request.signal);
        setState({ key, content });
      } catch (error) {
        if (!request.signal.aborted) {
          setState({ key, error: explain(error) });
          onSessionError(error);
        }
      }
    }
    void read(scope, ref, key);
    return () => request.abort();
  }, [client, key, onSessionError, preparedSource, ref, scope]);

  return <section className="pane source-pane" id="source-pane" aria-labelledby="source-heading">
    <header className="pane-heading">
      <h2 id="source-heading">{edge ? 'Bằng chứng quan hệ' : 'Kiểm tra ký hiệu'}</h2>
      <div className="segmented" aria-label="Nội dung khung phải">
        <button type="button" aria-pressed={detail === 'source'} onClick={() => onDetail('source')}>Mã nguồn</button>
        <button type="button" aria-pressed={detail === 'details'} onClick={() => onDetail('details')}>Chi tiết</button>
      </div>
    </header>
    {!node && !edge && <div className="empty">
      <strong>Chưa chọn ký hiệu</strong>
      <p>Chọn một nút trong đồ thị hoặc danh sách để đọc bằng chứng.</p>
    </div>}
    {(node || edge) && <div className="inspector">
      <div className="inspector-heading">
        <span className="kind-label">{edge ? 'Vị trí trong mã nguồn' : node?.kind}</span>
        <h3>{edge ? edgeRelationLabel(edge) : node && nodeTitle(node)}</h3>
        {edgeEndpoints && <p className="edge-endpoints">{edgeEndpoints.join(' → ')}</p>}
      </div>
      {preview && <p className="notice">Bằng chứng build · {preview.ancestorFallback ? 'Ranh giới tổ tiên' : 'Anchor khớp'}
        {' · '}{preview.moreInstances ? 'Ít nhất ' : ''}{preview.instancesRead} instance đã thấy.</p>}
      {detail === 'source' && <>
        {contiguous ? <div className="ref-list" aria-label="Đoạn mã nguồn">
          <button type="button" className="quiet-button" disabled={sourceIndex === 0}
            onClick={() => onRange(sourceIndex - 1)}>Đoạn trước</button>
          <span aria-live="polite">Đoạn {sourceIndex + 1}/{refs.length}</span>
          <button type="button" className="quiet-button" disabled={sourceIndex + 1 === refs.length}
            onClick={() => onRange(sourceIndex + 1)}>Đoạn sau</button>
        </div> : refs.length > 1 && <div className="ref-list" aria-label="Vùng mã nguồn">
          {refs.map((range, index) => <button type="button" key={sourceKey(range) + index}
            className="quiet-button" aria-pressed={sourceIndex === index} onClick={() => onRange(index)}>
            {access[index] ? relationLabel(access[index]) + ' · ' : ''}{range.path} · {range.range.start_byte}
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
        {edge ? <dl className="metadata">
          <div><dt>Loại quan hệ</dt><dd>{edgeRelationLabel(edge)}</dd></div>
          <div><dt>Trạng thái phân giải</dt><dd>{edge.resolution_status}</dd></div>
          <div><dt>Nguồn bằng chứng</dt><dd>{edge.evidence_refs.map(item => item.kind).join(', ')}</dd></div>
          <div><dt>Vùng mã nguồn</dt><dd>{refs.length}</dd></div>
        </dl> : node && <>
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
        </>}
      </div>}
    </div>}
  </section>;
}
