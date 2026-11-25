import { useCallback, useEffect, useRef, useState } from 'react';
import type { ExplorerClient } from '../api/client.ts';
import type { ScopeVector } from '../index.ts';
import { loadDirectory } from './map.ts';
import type { DirectoryEntry, DirectoryListing } from './map.ts';
import { explain } from './SourcePane.tsx';

const counts = (indexed: number, skipped: number) => `${indexed} đã lập chỉ mục · ${skipped} bỏ qua`;
const partsFor = (path: string) => path ? path.split('/') : [];
const skippedLabel = (reason: string | null) => {
  if (reason === 'OUTSIDE_CAPTURE_SCOPE') return 'Ngoài phạm vi phân tích';
  if (reason === 'PARSE_ERROR') return 'Lỗi phân tích';
  return reason ?? 'Chưa có mã nguồn trong index';
};

type Props = {
  client: ExplorerClient;
  scope: ScopeVector;
  directory: string;
  onDirectory: (path: string) => void;
  onFile: (entry: DirectoryEntry) => void;
  onOverview: () => void;
  onSessionError: (reason: unknown) => void;
  busy: boolean;
  graphError: string | null;
};

export function DirectoryPane({ client, scope, directory, onDirectory, onFile, onOverview, onSessionError, busy, graphError }: Props) {
  const [listing, setListing] = useState<DirectoryListing | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState('');
  const job = useRef<AbortController | null>(null);
  const crumbs = partsFor(directory);

  const load = useCallback(async (next: string | null) => {
    job.current?.abort();
    const current = new AbortController();
    job.current = current;
    setLoading(true);
    setError(null);
    try {
      const result = await loadDirectory(client, scope, directory, next, current.signal);
      setListing(prior => {
        if (!next || !prior) return result.listing;
        return { ...result.listing, entries: [...prior.entries, ...result.listing.entries] };
      });
      setCursor(result.cursor);
    } catch (reason) {
      if (current.signal.aborted) return;
      setError(explain(reason));
      onSessionError(reason);
    } finally {
      if (job.current === current && !current.signal.aborted) setLoading(false);
    }
  }, [client, scope, directory, onSessionError]);

  useEffect(() => {
    void load(null);
    return () => job.current?.abort();
  }, [load]);

  const query = filter.trim().toLocaleLowerCase('vi');
  const entries = listing?.entries.filter(entry => partsFor(entry.path).at(-1)?.toLocaleLowerCase('vi').includes(query)) ?? [];
  return <section className="pane graph-pane" id="graph-pane" aria-labelledby="directory-heading">
    <header className="pane-heading">
      <div className="pane-heading__title">
        <h2 id="directory-heading">Thư mục</h2>
        {listing && <span className="pane-count">{counts(listing.indexed, listing.skipped)}</span>}
      </div>
      <button className="quiet-button" type="button" disabled={busy} onClick={onOverview}>Tổng quan dự án</button>
    </header>
    <nav className="directory-path" aria-label="Đường dẫn thư mục">
      <button className="quiet-button" type="button" aria-current={directory ? undefined : 'location'} onClick={() => onDirectory('')}>Dự án</button>
      {crumbs.map((name, index) => <span key={crumbs.slice(0, index + 1).join('/')}><span aria-hidden="true">/</span>
        <button className="quiet-button" type="button" aria-current={index === crumbs.length - 1 ? 'location' : undefined}
          onClick={() => onDirectory(crumbs.slice(0, index + 1).join('/'))}>{name}</button>
      </span>)}
    </nav>
    <div className="graph-tools">
      <input type="search" aria-label="Lọc trang thư mục" placeholder="Lọc các mục đang hiển thị…" value={filter}
        onChange={event => setFilter(event.currentTarget.value)} />
    </div>
    <div className="graph-content" aria-busy={loading || busy}>
      {(error ?? graphError) && <p className="notice notice--error" role="alert">{error ?? graphError}</p>}
      {error && <button className="quiet-button directory-retry" type="button" onClick={() => void load(listing ? cursor : null)}>Thử lại</button>}
      {!listing && loading && <p className="notice" role="status">Đang đọc danh sách tệp…</p>}
      {listing && entries.length === 0 && <div className="empty">{query ? 'Trang này không có mục khớp bộ lọc.' : 'Thư mục chưa có tệp trong snapshot này.'}</div>}
      <ul className="symbol-list directory-list" aria-label="Nội dung thư mục">
        {entries.map(entry => {
          const label = partsFor(entry.path).at(-1) ?? entry.path;
          const folder = entry.kind === 'directory';
          if (!folder && entry.content_hash === null) return <li className="directory-skipped" key={entry.path}>
            <span className="symbol-row__title" title={entry.path}>{label}</span>
            <span className="symbol-row__kind">{skippedLabel(entry.reason_code)}</span>
          </li>;
          return <li key={entry.path}>
            <button className="symbol-row" type="button" title={entry.path} disabled={busy}
              aria-label={(folder ? 'Mở thư mục ' : 'Mở tệp ') + label}
              onClick={() => folder ? onDirectory(entry.path) : onFile(entry)}>
              <span className="symbol-row__title">{label}</span>
              <span className="symbol-row__kind">{folder ? counts(entry.indexed, entry.skipped) : 'Tệp'}</span>
            </button>
          </li>;
        })}
      </ul>
    </div>
    {cursor && <footer className="pane-footer">
      <span>{listing?.entries.length ?? 0} mục đã tải</span>
      <button className="quiet-button" type="button" disabled={loading || busy} onClick={() => void load(cursor)}>Tải thêm tệp</button>
    </footer>}
  </section>;
}
