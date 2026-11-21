import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import type { GraphNode, MappingProposeRequest, MappingReviewRequest, RecordReadData, RecordSummary,
  ScopeVector, SourceRef } from '../../../.generated/types';
import type { ExplorerClient } from '../../api/client.ts';
import { ExplorerRequestError } from '../../api/client.ts';
import { verifyScopeDigest } from '../../scope.ts';

type PendingMutation = { kind: 'propose'; request: MappingProposeRequest } |
  { kind: 'review'; request: MappingReviewRequest };

const statusText = {
  proposed: 'Chờ duyệt', approved: 'Đã duyệt', revoked: 'Đã thu hồi',
  superseded: 'Đã thay thế', needs_review: 'Cần kiểm tra lại',
};

export function ManualMappings({ client, scope, node, source, onChanged, onSessionError }: {
  client: ExplorerClient; scope: ScopeVector | null; node: GraphNode | null; source: SourceRef | undefined;
  onChanged: () => Promise<void>; onSessionError: (reason: unknown) => void;
}) {
  const [destination, setDestination] = useState<{ scope: string; node: GraphNode } | null>(null);
  const [rationale, setRationale] = useState('');
  const [reason, setReason] = useState('');
  const [rows, setRows] = useState<RecordSummary[]>([]);
  const [selected, setSelected] = useState<RecordReadData | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [message, setMessage] = useState('');
  const [pending, setPending] = useState<PendingMutation | null>(null);
  const [busy, setBusy] = useState(false);
  const reads = useRef<AbortController | null>(null);
  const write = useRef<AbortController | null>(null);
  const kept = destination?.scope === scope?.vector_digest ? destination : null;

  const fail = useCallback((error: unknown) => {
    setMessage(error instanceof Error ? error.message : 'Không thể đọc hồ sơ mapping.');
    onSessionError(error);
  }, [onSessionError]);

  const load = useCallback(async (next: string | null) => {
    reads.current?.abort();
    const job = new AbortController();
    reads.current = job;
    try {
      const response = await client.listRecords({ record_kind: 'ui_mapping', limit: 10,
        ...(next === null ? {} : { cursor: next }) }, job.signal);
      if (!response.data) throw new Error('Danh sách hồ sơ chưa khả dụng.');
      if (response.scope) await verifyScopeDigest(response.scope, 32768);
      job.signal.throwIfAborted();
      if (response.pagination.next_cursor !== null && response.pagination.next_cursor === next)
        throw new Error('Trang hồ sơ chưa tiến triển.');
      setRows(response.data.records);
      setCursor(response.pagination.next_cursor);
    } catch (error) {
      if (!job.signal.aborted) fail(error);
    }
  }, [client, fail]);

  useEffect(() => {
    setSelected(null);
    setRows([]);
    setCursor(null);
    void load(null);
    return () => reads.current?.abort();
  }, [load, scope?.vector_digest]);
  useEffect(() => () => write.current?.abort(), []);

  async function view(row: RecordSummary) {
    reads.current?.abort();
    const job = new AbortController();
    reads.current = job;
    try {
      const response = await client.readRecord({ record_id: row.record_id, version: row.version }, job.signal);
      if (!response.data || response.data.record.record_id !== row.record_id || response.data.record.version !== row.version)
        throw new Error('Phiên bản hồ sơ đã thay đổi.');
      if (response.scope) await verifyScopeDigest(response.scope, 32768);
      job.signal.throwIfAborted();
      setSelected(response.data);
      setReason('');
    } catch (error) {
      if (!job.signal.aborted) fail(error);
    }
  }

  async function submit(action: PendingMutation) {
    if (write.current) return;
    const job = new AbortController();
    write.current = job;
    setBusy(true);
    setPending(action);
    try {
      const response = action.kind === 'propose' ? await client.proposeMapping(action.request, job.signal)
        : await client.reviewMapping(action.request, job.signal);
      if (!response.data) throw new Error('Chưa nhận được biên nhận mapping.');
      job.signal.throwIfAborted();
      setMessage(statusText[response.data.effective_status] + ' · v' + response.data.version);
      setPending(null);
      setSelected(null);
      setRationale('');
      setReason('');
      await onChanged();
      if (!job.signal.aborted) await load(null);
    } catch (error) {
      if (!job.signal.aborted) {
        if (error instanceof ExplorerRequestError && [400, 403, 409, 410].includes(error.status)) setPending(null);
        fail(error);
      }
    } finally {
      if (write.current === job) {
        write.current = null;
        if (!job.signal.aborted) setBusy(false);
      }
    }
  }

  function propose(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!scope || !source || !kept || !rationale.trim() || pending) return;
    void submit({ kind: 'propose', request: { scope: { pinned_vector: scope }, source_ref: source,
      destination_ref: kept.node.ref, rationale: rationale.trim(), client_mutation_id: crypto.randomUUID() } });
  }

  function review(action: MappingReviewRequest['action']) {
    if (!selected || !scope || !reason.trim() || pending) return;
    void submit({ kind: 'review', request: { scope: { pinned_vector: scope }, record_id: selected.record.record_id,
      expected_version: selected.record.version, action, reason: reason.trim(), client_mutation_id: crypto.randomUUID() } });
  }

  return <section className="mapping-records" aria-labelledby="mapping-records-title">
    <header className="pane-heading"><h2 id="mapping-records-title">Mapping thủ công</h2>
      <button className="quiet-button" type="button" disabled={busy} onClick={() => void load(null)}>Tải lại hồ sơ</button></header>
    <form onSubmit={propose}>
      <p>Nguồn đang chọn <strong>{source ? source.path + ' · byte ' + source.range.start_byte : 'Chưa chọn'}</strong></p>
      <p>Đích đã giữ <strong>{kept?.node.label || kept?.node.kind || 'Chưa chọn'}</strong></p>
      <button className="quiet-button" type="button" disabled={!node || !scope || busy}
        onClick={() => { if (node && scope) setDestination({ node, scope: scope.vector_digest }); }}>Dùng nút đang chọn làm đích</button>
      <label>Lý do đề xuất <textarea value={rationale} onChange={event => setRationale(event.currentTarget.value)}
        maxLength={2048} required /></label>
      <button className="primary-button" type="submit"
        disabled={!scope || !kept || !source || busy || !!pending || !rationale.trim()}>Đề xuất mapping</button>
    </form>
    {pending && !busy && <button className="quiet-button" type="button" onClick={() => void submit(pending)}>Gửi lại cùng yêu cầu</button>}
    {message && <p role="status">{message}</p>}
    <ul aria-label="Hồ sơ mapping">{rows.map(row =>
      <li key={row.record_id}><button className="quiet-button" type="button" disabled={busy} onClick={() => void view(row)}>
        {row.source_refs[0]?.path ?? row.record_id.slice(0, 8)} · {statusText[row.effective_status]} · v{row.version}
      </button></li>)}</ul>
    {cursor && <button className="quiet-button" type="button" disabled={busy} onClick={() => void load(cursor)}>Trang hồ sơ tiếp theo</button>}
    {selected && <div className="mapping-records__review">
      <p>Hồ sơ do người dùng ghi nhận · {statusText[selected.record.effective_status]} · v{selected.record.version}</p>
      {selected.ui_mapping && <p>{selected.ui_mapping.rationale}</p>}
      {selected.record.provenance_status !== 'retained_snapshot' && <p>Vùng nguồn cần được xác minh lại.</p>}
      {(selected.record.declared_status === 'proposed' || selected.record.declared_status === 'approved') && <>
        <label>Lý do duyệt <textarea value={reason} onChange={event => setReason(event.currentTarget.value)} maxLength={2048} /></label>
        {selected.record.declared_status === 'proposed' && <button className="primary-button" type="button"
          disabled={!scope || !!pending || busy || !reason.trim() || selected.record.provenance_status !== 'retained_snapshot'}
          onClick={() => review('approve')}>Duyệt mapping</button>}
        {selected.record.declared_status === 'approved' && <button className="quiet-button" type="button"
          disabled={!scope || !!pending || busy || !reason.trim()} onClick={() => review('revoke')}>Thu hồi mapping</button>}
      </>}
    </div>}
  </section>;
}
