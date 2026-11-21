import { useEffect, useRef, useState } from 'react';
import type { PackRequest, ScopeVector, SourceRef } from '../../.generated/types';
import type { ExplorerClient } from '../api/client.ts';
import { graphRefDigest } from '../graph.ts';
import type { HTTPResponse } from '../http.ts';
import { verifyScopeDigest } from '../scope.ts';

export function SelectionContext({ client, scope, locator, source, onSessionError }: {
  client: ExplorerClient; scope: ScopeVector; locator: PackRequest | null; source: SourceRef | undefined;
  onSessionError: (error: unknown) => void;
}) {
  const [budget, setBudget] = useState(24576);
  const [output, setOutput] = useState<HTTPResponse<'POST /context/pack'> | null>(null);
  const [show, setShow] = useState(false);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const active = useRef<AbortController | null>(null);
  const typedLocator = locator ?? (source ? { source_ref: source } : null);
  const text = typedLocator ? JSON.stringify(typedLocator, null, 2) : '';
  useEffect(() => () => active.current?.abort(), []);

  async function request() {
    if (!locator || active.current) return;
    const job = new AbortController();
    active.current = job;
    setBusy(true);
    setOutput(null);
    setMessage('');
    try {
      const result = await client.contextPack({ ...locator, max_output_bytes: budget }, job.signal);
      if (!result.scope || !result.data || result.scope.vector_digest !== scope.vector_digest)
        throw new Error('Context pack trả về khác phạm vi đã chọn.');
      await verifyScopeDigest(result.scope, 32768);
      if (locator.selector.kind === 'ref') {
        const selected = await graphRefDigest(locator.selector.ref, 8192);
        const received = await Promise.all(result.data.target_refs.map(ref => graphRefDigest(ref, 8192)));
        if (!received.includes(selected)) throw new Error('Context pack trả về khác đích đã chọn.');
      }
      job.signal.throwIfAborted();
      setOutput(result);
    } catch (error) {
      if (!job.signal.aborted) {
        setMessage(error instanceof Error ? error.message : 'Context pack chưa khả dụng.');
        onSessionError(error);
      }
    } finally {
      if (active.current === job) {
        active.current = null;
        if (!job.signal.aborted) setBusy(false);
      }
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setMessage('Đã sao chép locator.');
    } catch {
      setShow(true);
      setMessage('Chọn văn bản locator để sao chép.');
    }
  }

  function save() {
    const link = document.createElement('a');
    const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
    link.href = url;
    link.download = 'mce4d-selection.json';
    link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 5000);
  }

  return <section className="selection-context" aria-labelledby="selection-context-title">
    <h3 id="selection-context-title">Context của lựa chọn</h3>
    <div className="selection-context__actions">
      <button className="quiet-button" type="button" disabled={!text} onClick={() => setShow(value => !value)}>Xem locator</button>
      <button className="quiet-button" type="button" disabled={!text} onClick={() => void copy()}>Sao chép locator</button>
      <button className="quiet-button" type="button" disabled={!text} onClick={save}>Tải locator</button>
    </div>
    {show && <textarea aria-label="Locator của lựa chọn" readOnly value={text} />}
    {locator && <div className="selection-context__actions">
      <label>Ngân sách <select disabled={busy} value={budget} onChange={event => setBudget(Number(event.currentTarget.value))}>
        <option value={4096}>4 KiB</option><option value={12288}>12 KiB</option><option value={24576}>24 KiB</option>
      </select></label>
      <button className="primary-button" type="button" disabled={busy} onClick={() => void request()}>
        {busy ? 'Đang đọc context…' : 'Tạo context pack'}
      </button>
    </div>}
    {message && <p role="status">{message}</p>}
    {output?.data && <div aria-label="Context pack đã lập">
      <p>{output.budget.serialized_bytes} byte · {output.data.items.length} mục
        {output.pagination.truncated ? ' · Còn nội dung chờ' : ' · Hoàn tất phân trang'}</p>
      {output.pagination.required_pending !== 0 && <p>Mục bắt buộc còn chờ {output.pagination.required_pending ?? 'chưa xác định'}.</p>}
      {output.coverage.gaps.map((gap, n) => <p key={gap.code + n}>{gap.reason}</p>)}
      {output.data.items.map(item => <details key={item.item_id}>
        <summary>{item.source_refs[0]?.path ?? item.kind}</summary>
        {item.content !== null && <pre>{item.content}</pre>}
      </details>)}
    </div>}
  </section>;
}
