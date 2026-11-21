import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import type { FlowStep, GraphNode, ScopeVector, SourceRef } from '../../../.generated/types';
import type { ExplorerClient } from '../../api/client.ts';
import { readFlowSelection, readObservedFlow, readStaticFlow } from './data.ts';
import type { FlowSelection, StaticFlow } from './data.ts';

type Props = {
  client: ExplorerClient;
  root: GraphNode;
  scope: ScopeVector;
  visible: readonly GraphNode[];
  selected: GraphNode | null;
  beginSelection: () => (() => boolean);
  onSelect: (value: FlowSelection) => void;
  onClose: () => void;
  onReturnToPreview?: () => void;
  onSessionError: (reason: unknown) => void;
};

const stepNames: Record<string, string> = {
  http_route: 'Đăng ký route', http_handler: 'Handler', function: 'Hàm', callee: 'Hàm được gọi',
  direct_call: 'Lời gọi trực tiếp', unresolved_call: 'Lời gọi chưa phân giải',
  branch: 'Điều kiện', branch_then: 'Nhánh đúng', branch_else: 'Nhánh còn lại', return: 'Trả về',
  async_boundary: 'Ranh giới goroutine', defer_boundary: 'Lời gọi defer', control_boundary: 'Ranh giới điều khiển',
  loop_boundary: 'Vòng lặp', event_send_boundary: 'Gửi event', event_receive_boundary: 'Nhận event',
  sql_begin: 'BEGIN', sql_commit: 'COMMIT', sql_rollback: 'ROLLBACK', sql_insert: 'INSERT',
  sql_update: 'UPDATE', sql_delete: 'DELETE', sql_read: 'READ', sql_call: 'CALL', sql_boundary: 'SQL chưa phân giải',
  http_request: 'HTTP request', runtime_span: 'Span đã ghi', sql_statement: 'Câu lệnh SQL',
};

const orderNames: Record<FlowStep['order_semantics'], string> = {
  source_sequence: 'Thứ tự trong nguồn', control_flow_supported: 'Điều khiển tĩnh',
  call_relationship_only: 'Quan hệ gọi', unknown: 'Thứ tự chưa rõ', causal_trace: 'Trace',
};

const edgeNames: Record<string, string> = {
  call: 'Lời gọi', control: 'Điều khiển', return: 'Trả về', async: 'Bất đồng bộ',
  contract: 'Binding', transaction_link: 'Cùng giao dịch', candidate: 'Ứng viên',
};

const limitNames: Record<string, string> = {
  FLOW_DEFERRED_EFFECT_UNKNOWN: 'Đây là lời gọi được defer. Kết quả chưa xác định.',
  FLOW_TRANSACTION_RECEIVER_UNRESOLVED: 'Chưa đủ bằng chứng để ghép với giao dịch.',
  FLOW_LOOP_COUNT_UNVERIFIED: 'Số lần lặp hoặc retry chưa xác định.',
  FLOW_EVENT_DELIVERY_UNOBSERVED: 'Chưa có quan sát giao nhận event.',
  FLOW_EVENT_ACK_DECLARED: 'Ack là cấu hình đã khai báo. Trạng thái lưu dữ liệu chưa xác định.',
  FLOW_EVENT_OUTBOX_DECLARED: 'Outbox là khai báo. Chưa có bằng chứng đã publish hoặc commit.',
  OBSERVED_TRANSACTION_OUTCOME_UNKNOWN: 'Chưa có kết quả giao dịch được xác nhận trong lần chạy này.',
  OBSERVED_TRANSACTION_COMMIT: 'Trace ghi nhận commit của giao dịch.',
  OBSERVED_TRANSACTION_ROLLBACK: 'Trace ghi nhận rollback của giao dịch.',
};

const flowBatch = 20;
const filters = {
  all: 'Tất cả',
  http: 'HTTP',
  sql: 'SQL',
  branch: 'Điều kiện và ranh giới',
  event: 'Event',
} as const;

type StepFilter = keyof typeof filters;

function matches(step: FlowStep, filter: StepFilter): boolean {
  if (filter === 'all') return true;
  if (filter === 'branch') return step.step_kind.startsWith('branch') || step.step_kind.endsWith('_boundary');
  return step.step_kind.startsWith(filter + '_');
}

function hasSelectedRef(step: FlowStep, node: GraphNode | null): boolean {
  if (!node) return false;
  const target = node.ref;
  return step.canonical_refs.some(ref => ref.kind === target.kind && ref.worktree_id === target.worktree_id &&
    ref.profile_id === target.profile_id && ref.generation_id === target.generation_id &&
    ((ref.kind === 'symbol' && target.kind === 'symbol' && ref.symbol_id === target.symbol_id) ||
      (ref.kind === 'entity' && target.kind === 'entity' && ref.entity_id === target.entity_id)));
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : 'Không thể đọc backend flow.';
}

export function FlowPanel({ client, root, scope, visible, selected, beginSelection, onSelect, onClose,
  onReturnToPreview, onSessionError }: Props) {
  const [observedArtifactId, setObservedArtifactId] = useState<string>();
  const [loaded, setLoaded] = useState<{ result: StaticFlow; artifactId: string | undefined } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reading, setReading] = useState<string | null>(null);
  const [chosen, setChosen] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [filter, setFilter] = useState<StepFilter>('all');
  const [shown, setShown] = useState(flowBatch);
  const selection = useRef<AbortController | null>(null);
  const result = loaded?.artifactId === observedArtifactId ? loaded?.result ?? null : null;
  const canRead = observedArtifactId !== undefined || client.flowEnabled();

  useEffect(() => {
    if (!canRead) return;
    const request = new AbortController();
    const read = observedArtifactId
      ? readObservedFlow(client, scope, root.ref, observedArtifactId, request.signal)
      : readStaticFlow(client, scope, root.ref, request.signal);
    void read.then(data => {
      if (!request.signal.aborted) setLoaded({ result: data, artifactId: observedArtifactId });
    }).catch(reason => {
      if (request.signal.aborted) return;
      setError(message(reason));
      onSessionError(reason);
    });
    return () => {
      request.abort();
      selection.current?.abort();
    };
  }, [attempt, canRead, client, observedArtifactId, onSessionError, root.ref, scope]);

  function changeArtifact(artifactId: string | undefined) {
    setLoaded(null);
    setError(null);
    setChosen(null);
    setReading(null);
    setFilter('all');
    setShown(flowBatch);
    setObservedArtifactId(artifactId);
    setAttempt(value => value + 1);
  }

  function openArtifact(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const value = new FormData(event.currentTarget).get('artifact-id');
    if (typeof value === 'string' && /^[0-9a-f]{64}$/.test(value)) changeArtifact(value);
  }

  async function choose(step: FlowStep, source?: SourceRef) {
    selection.current?.abort();
    const request = new AbortController();
    selection.current = request;
    const live = beginSelection();
    setReading(step.step_id);
    setError(null);
    try {
      const value = await readFlowSelection(client, scope, step, visible, request.signal, source);
      if (live()) {
        setChosen(step.step_id);
        onSelect(value);
      }
    } catch (reason) {
      if (!request.signal.aborted && live()) {
        setError(message(reason));
        onSessionError(reason);
      }
    } finally {
      if (!request.signal.aborted) setReading(null);
    }
  }

  const ids = useMemo(() => new Map(result?.data.steps.map((step, index) => [step.step_id, index + 1] as const)),
    [result]);
  const incomingByStep = useMemo(() => Map.groupBy(result?.data.edges ?? [], edge => edge.to_step_id), [result]);
  const steps = result?.data.steps ?? [];
  const matching = steps.filter(step => matches(step, filter));
  const visibleSteps = matching.slice(0, shown);
  const selectedStepVisible = visibleSteps.some(step => step.step_id === chosen && hasSelectedRef(step, selected));
  const observed = observedArtifactId !== undefined;
  const displayed = observed ? 'Flow quan sát' : 'Flow tĩnh';
  return <section className="pane flow-pane" aria-labelledby="flow-heading">
    <header className="pane-heading">
      <div>
        <h2 id="flow-heading">{observed ? 'Flow quan sát' : 'Flow backend'}</h2>
        <p className="flow-root">{root.label || root.kind}</p>
      </div>
      <div className="flow-actions">
        {onReturnToPreview && <button className="quiet-button" type="button" onClick={onReturnToPreview}>Về Preview</button>}
        <button className="quiet-button" type="button" onClick={onClose}>Đóng flow</button>
      </div>
    </header>
    {client.observedFlowEnabled() && <form className="preview-dialog__form" onSubmit={openArtifact}>
      <label>Mã trace đã nhập <input name="artifact-id" required pattern="[0-9a-f]{64}" maxLength={64} autoComplete="off" /></label>
      <button className="quiet-button" type="submit">Mở trace</button>
      {observed && client.flowEnabled() && <button className="quiet-button" type="button"
        onClick={() => changeArtifact(undefined)}>Về flow tĩnh</button>}
    </form>}
    {error && <p className="notice notice--error" role="alert">{error}</p>}
    {canRead && !result && !error && <p className="notice" role="status">Đang đọc {displayed.toLowerCase()}…</p>}
    {!result && error && <div className="flow-retry"><button className="quiet-button" type="button"
      onClick={() => { setError(null); setAttempt(value => value + 1); }}>Thử lại</button></div>}
    {result && <div className="flow-body" aria-label={'Các bước ' + displayed.toLowerCase()}>
      <p className="flow-status" role="status">
        {result.pagination.truncated || result.coverage.status === 'partial'
          ? `${displayed} một phần · ${steps.length} bước đã đọc`
          : `${displayed} · ${steps.length} bước`}
        {reading && ' · Đang đọc nguồn'}
      </p>
      {steps.length > 0 && <div className="flow-filters">
        <label htmlFor="flow-filter">Lọc bước</label>
        <select id="flow-filter" value={filter} onChange={event => {
          setFilter(event.currentTarget.value as StepFilter);
          setShown(flowBatch);
        }}>
          {Object.entries(filters).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select>
        <span className="pane-count" role="status">{visibleSteps.length}/{matching.length} bước phù hợp</span>
      </div>}
      {steps.length > 0 && matching.length === 0 && <p className="notice">Không có bước phù hợp trong lát flow đã đọc.</p>}
      {result.pagination.truncated && <p className="notice notice--partial">
        {result.pagination.required_pending == null ? 'Chưa rõ số bước còn lại trong giới hạn phân tích.' :
          `Còn ${result.pagination.required_pending} bước ngoài giới hạn phản hồi.`}
      </p>}
      {steps.length === 0 && <p className="notice">Index chưa có flow được xác minh cho lựa chọn này.</p>}
      {result.coverage.gaps.length > 0 && <ul className="flow-gaps">
        {result.coverage.gaps.map((gap, index) => <li key={gap.code + index}>{gap.reason}</li>)}
      </ul>}
      <ol className="flow-list">
        {visibleSteps.map(step => {
          const sql = step.step_kind.startsWith('sql_');
          const title = stepNames[step.step_kind] ?? step.step_kind;
          const source = step.source_refs[0];
          const targets = step.canonical_refs.filter(ref => ref.kind === 'entity');
          const active = hasSelectedRef(step, selected) && (!selectedStepVisible || chosen === step.step_id);
          const incoming = incomingByStep.get(step.step_id) ?? [];
          return <li key={step.step_id} className={'flow-step' + (sql ? ' flow-step--sql' : '')}>
            <button className="flow-step__choose" type="button" aria-pressed={active} disabled={!source}
              onClick={() => void choose(step)}>
              <span className="flow-step__title">{title}</span>
              <span className="flow-step__status">{orderNames[step.order_semantics]}</span>
              {source && <span className="flow-step__source">{source.path} · byte {source.range.start_byte}</span>}
            </button>
            {sql && <dl className="flow-sql">
              <div><dt>Phạm vi dữ liệu</dt><dd>{targets.length ? `${targets.length} thực thể có bằng chứng` : 'Xem câu lệnh trong nguồn'}</dd></div>
              <div><dt>Kết quả lưu</dt><dd>{transactionOutcome(step, observed)}</dd></div>
            </dl>}
            {sql && step.source_refs.length > 1 && <div className="flow-table-sources" aria-label="Nguồn khai báo bảng hoặc view">
              {step.source_refs.slice(1).map(ref => <button className="quiet-button" type="button"
                key={ref.path + ref.range.start_byte} onClick={() => void choose(step, ref)}>
                {ref.path} · byte {ref.range.start_byte}
              </button>)}
            </div>}
            {incoming.length > 0 && <p className="flow-connections">
              {incoming.map(edge => `${observed && edge.kind === 'control' ? 'Span cha' : edgeNames[edge.kind] ?? edge.kind} · bước ${ids.get(edge.from_step_id)}`).join('  ·  ')}
            </p>}
            {step.limitations.length > 0 && <details className="flow-step__limits">
              <summary>Giới hạn bằng chứng</summary>
              <ul>{step.limitations.map((gap, index) => <li key={gap.code + index}>{limitNames[gap.code] ?? gap.reason}</li>)}</ul>
            </details>}
          </li>;
        })}
      </ol>
      {visibleSteps.length < matching.length && <button className="quiet-button" type="button"
        onClick={() => setShown(value => value + flowBatch)}>Hiện thêm bước ({matching.length - visibleSteps.length})</button>}
    </div>}
  </section>;
}

function transactionOutcome(step: FlowStep, observed: boolean): string {
  if (!observed) return 'Chưa xác định';
  if (step.outcome_status === 'rollback_observed' || step.limitations.some(gap => gap.code === 'OBSERVED_TRANSACTION_ROLLBACK')) {
    return 'Đã quan sát rollback';
  }
  if (step.outcome_status === 'commit_observed' || step.limitations.some(gap => gap.code === 'OBSERVED_TRANSACTION_COMMIT')) {
    return 'Đã quan sát commit';
  }
  if (step.outcome_status === 'statement_success_observed') return 'Câu lệnh thành công, kết quả giao dịch chưa rõ';
  return 'Chưa xác định';
}
