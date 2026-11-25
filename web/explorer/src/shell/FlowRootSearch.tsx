import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import type { ExplorerClient } from '../api/client.ts';
import type { GraphNode, ScopeVector } from '../index.ts';
import { findFlowRoots } from './map.ts';
import { explain } from './SourcePane.tsx';

type Props = {
  client: ExplorerClient;
  scope: ScopeVector;
  busy: boolean;
  graphError: string | null;
  onClose: () => void;
  onOpen: (node: GraphNode) => void;
  onSessionError: (reason: unknown) => void;
};

export function FlowRootSearch({ client, scope, busy, graphError, onClose, onOpen, onSessionError }: Props) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<Awaited<ReturnType<typeof findFlowRoots>> | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const job = useRef<AbortController | null>(null);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    input.current?.focus();
    return () => job.current?.abort();
  }, []);

  async function find(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const term = query.trim();
    if (!term || busy) return;
    job.current?.abort();
    const current = new AbortController();
    job.current = current;
    setLoading(true);
    setError(null);
    setResults(null);
    try {
      setResults(await findFlowRoots(client, scope, term, current.signal));
    } catch (reason) {
      if (current.signal.aborted) return;
      setError(explain(reason));
      onSessionError(reason);
    } finally {
      if (job.current === current && !current.signal.aborted) setLoading(false);
    }
  }

  return <section className="pane graph-pane" id="graph-pane" aria-labelledby="flow-search-heading">
    <header className="pane-heading">
      <h2 id="flow-search-heading">Tìm điểm bắt đầu</h2>
      <button className="quiet-button" type="button" disabled={busy} onClick={onClose}>Quay lại</button>
    </header>
    <form className="graph-tools" onSubmit={event => void find(event)}>
      <input ref={input} type="search" aria-label="Tìm trong toàn bộ index" placeholder="Nhập đầu tên hàm, component hoặc route…"
        maxLength={256} required value={query} onChange={event => setQuery(event.currentTarget.value)} />
      <button className="quiet-button" type="submit" disabled={busy || loading}>Tìm</button>
    </form>
    <div className="graph-content" aria-busy={busy || loading}>
      {loading && <p className="notice" role="status">Đang tìm trong index…</p>}
      {busy && <p className="notice" role="status">Đang mở luồng…</p>}
      {(error ?? graphError) && <p className="notice notice--error" role="alert">{error ?? graphError}</p>}
      {results?.limited && <p className="notice">Còn kết quả. Nhập thêm ký tự để thu hẹp tìm kiếm.</p>}
      {results && results.matches.length === 0 && <p className="notice">Không tìm thấy tên hoặc route bắt đầu bằng cụm này trong index.</p>}
      {results && <ul className="symbol-list" aria-label="Kết quả tìm điểm bắt đầu">
        {results.matches.map(node => <li key={node.node_id}>
          <button className="symbol-row" type="button" disabled={busy || loading} onClick={() => onOpen(node)}>
            <span className="symbol-row__title">{node.label}</span>
            <span className="symbol-row__path">{node.source_refs[0]?.path}</span>
            <span className="symbol-row__kind">{node.kind}</span>
          </button>
        </li>)}
      </ul>}
    </div>
  </section>;
}
