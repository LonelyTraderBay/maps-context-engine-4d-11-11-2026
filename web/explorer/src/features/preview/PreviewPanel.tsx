import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import type { ExplorerClient } from '../../api/client.ts';
import type { GraphNode, ScopeVector, SourceRef } from '../../../.generated/types';
import { startPreviewPairing } from './pairing.ts';
import type { PreviewSelection, ReversePreview } from './pairing.ts';

const labels = {
  idle: 'Chọn build đã import và quyền preview đã được cấp.',
  pairing: 'Đang ghép phiên preview…',
  ready: 'Preview sẵn sàng ở chế độ Inspect.',
  arming: 'Đang xác nhận quyền Interact…',
  interact: 'Interact đang bật trong môi trường thử nghiệm.',
  awaiting_map: 'Đang chờ map của build mới và binding đúng snapshot. Ghép lại sau khi import.',
  stale: 'Preview chưa sẵn sàng. Kiểm tra build và quyền rồi ghép lại.',
  detached: 'Đã ngắt preview.',
};

export function PreviewPanel({ client, scope, close, onSelect, beginSelection, graphNode, onSource }: {
  client: ExplorerClient; scope: ScopeVector; close: () => void; onSelect: (value: PreviewSelection) => void;
  beginSelection: () => () => boolean;
  graphNode: GraphNode | null;
  onSource: (node: string, source: SourceRef) => void;
}) {
  const iframe = useRef<HTMLIFrameElement>(null);
  const buildInput = useRef<HTMLInputElement>(null);
  const shield = useRef<HTMLButtonElement>(null);
  const pairing = useRef<ReturnType<typeof startPreviewPairing> | null>(null);
  const mounted = useRef(false);
  const [status, setStatus] = useState<keyof typeof labels>('idle');
  const [kind, setKind] = useState<'element' | 'component'>('element');
  const [message, setMessage] = useState('');
  const [reverse, setReverse] = useState<ReversePreview | null>(null);
  const selectionCallback = useRef(onSelect);
  useEffect(() => { selectionCallback.current = onSelect; }, [onSelect]);
  useEffect(() => { pairing.current?.clearReverse(); }, [graphNode?.node_id]);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      pairing.current?.close();
      pairing.current = null;
    };
  }, []);

  function attach(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!iframe.current || !shield.current) return;
    pairing.current?.close();
    const data = new FormData(event.currentTarget);
    try {
      pairing.current = startPreviewPairing(iframe.current, shield.current, client, {
        url: String(data.get('url') ?? '').trim(), buildMap: String(data.get('build') ?? '').trim(),
        permission: String(data.get('permission') ?? '').trim(), scope,
      }, value => { if (mounted.current) setStatus(value); },
      value => selectionCallback.current(value), value => { if (mounted.current) setMessage(value); },
      value => { if (mounted.current) setReverse(value); },
      id => { if (buildInput.current) buildInput.current.value = id; });
    } catch {
      setStatus('stale');
    }
  }

  return <section className="preview-panel" aria-labelledby="preview-title"
    onKeyDown={event => { if (event.key === 'Escape') close(); }}>
    <div className="preview-dialog__title">
      <h2 id="preview-title">Preview</h2>
      <button type="button" className="quiet-button" onClick={close}>Đóng</button>
    </div>
    <form className="preview-dialog__form" onSubmit={attach}>
      <label>Địa chỉ <input name="url" type="url" required maxLength={2048} autoComplete="off" /></label>
      <label>Mã build map <input ref={buildInput} name="build" pattern="[0-9a-f]{64}" required maxLength={64} autoComplete="off" /></label>
      <label>Hồ sơ quyền <input name="permission" required maxLength={192} autoComplete="off" /></label>
      <button className="primary-button" type="submit" disabled={status === 'pairing'}>Ghép preview</button>
    </form>
    <p className="preview-dialog__status" role="status">{labels[status]}</p>
    <div className="preview-dialog__modes">
      <button className="quiet-button" type="button" disabled={status !== 'ready'}
        onClick={() => void pairing.current?.changeMode('interact')}>Bật Interact</button>
      <button className="quiet-button" type="button" disabled={status !== 'interact' && status !== 'arming'}
        onClick={() => void pairing.current?.changeMode('inspect')}>Về Inspect</button>
      <label>Chọn <select value={kind} onChange={event => setKind(event.currentTarget.value === 'component' ? 'component' : 'element')}>
        <option value="element">Phần tử</option>
        <option value="component">Component</option>
      </select></label>
      <button className="quiet-button" type="button"
        disabled={status !== 'ready' || !graphNode || !['file', 'symbol'].includes(graphNode.ref.kind)}
        onClick={() => { if (graphNode) void pairing.current?.startReverse(graphNode.ref, beginSelection()); }}>Hiện nút trong preview</button>
      <span>Inspect giữ thao tác chọn trong Explorer. Ứng dụng preview vẫn chạy.</span>
    </div>
    <div className="preview-dialog__viewport">
      <iframe ref={iframe} title="Preview" sandbox="allow-scripts allow-same-origin"
        tabIndex={-1} inert className="preview-dialog__frame" referrerPolicy="no-referrer" />
      <button ref={shield} className="preview-dialog__shield" type="button" aria-label="Vùng chọn Inspect"
        onClick={event => void pairing.current?.pick(event.detail ? event.clientX : null, event.detail ? event.clientY : null,
          kind, beginSelection())}
        onContextMenu={event => event.preventDefault()} />
      <div className="preview-markers" aria-hidden="true">{reverse?.rects.map(({ x, y, width, height }, index) =>
        <div className="preview-marker" key={index} style={{ left: x, top: y, width, height }} />)}</div>
    </div>
    {message && <p className="preview-dialog__status" role="status">{message}</p>}
    {reverse && reverse.matches.length > 0 && <div className="preview-choices">
      <ul aria-label="Vị trí nguồn của nút">
        {reverse.matches.map(match => {
          const source = match.source_refs[0];
          const anchor = match.anchor_ref.anchor_id;
          return <li key={match.match_id}>
            <button className="quiet-button" type="button" aria-pressed={reverse.anchor === anchor}
              onClick={() => void pairing.current?.readInstancePage(anchor)}>
              {source ? source.path + ' · byte ' + source.range.start_byte : anchor.slice(0, 12)}
            </button>
            {source && graphNode && <button className="quiet-button" type="button"
              onClick={() => onSource(graphNode.node_id, source)}>Đọc vùng này</button>}
          </li>;
        })}
      </ul>
      {reverse.mappingCursor !== null && <button className="quiet-button" type="button"
        onClick={() => void pairing.current?.moreMappings()}>Trang nguồn tiếp theo</button>}
      {reverse.anchor !== null && <div>
        <ul aria-label="Instance trong preview">{reverse.instances.map((item, index) => {
          const id = item.instance_ref.instance_id;
          return <li key={id}>
            <button className="quiet-button" type="button" aria-pressed={reverse.selectedInstance === id}
              onClick={() => void pairing.current?.revealInstance(id, false)}>
              Instance {index + 1} · {item.visibility === 'visible' ? 'Đang hiển thị' :
                item.visibility === 'mounted_offscreen' ? 'Ngoài khung' : 'Chưa hiển thị'}
            </button>
            {item.visibility === 'mounted_offscreen' && <button className="quiet-button" type="button"
              onClick={event => { if (event.isTrusted) void pairing.current?.revealInstance(id, true); }}>Cuộn đến</button>}
          </li>;
        })}</ul>
        <button className="quiet-button" type="button"
          onClick={() => { if (reverse.anchor) void pairing.current?.readInstancePage(reverse.anchor); }}>Tải lại instance</button>
        {reverse.instanceCursor !== null && <button className="quiet-button" type="button"
          onClick={() => { if (reverse.anchor) void pairing.current?.readInstancePage(reverse.anchor, true); }}>Trang instance tiếp theo</button>}
      </div>}
    </div>}
  </section>;
}
