import type { BridgeMessage, DOMAnchorRef, GraphNode, GraphRef, InstanceRef, MappingMatch, PreviewData, Rect, ScopeVector } from '../../../.generated/types';
import { ExplorerRequestError } from '../../api/client.ts';
import type { ExplorerClient } from '../../api/client.ts';
import { decodeBridgeMessage } from '../../bridge.ts';
import { graphRefDigest, subgraphDigest } from '../../graph.ts';
import { canonicalOrigin, checkPathname } from '../../paths.ts';
import { verifyScopeDigest } from '../../scope.ts';

type PreviewAPI = Pick<ExplorerClient, 'previewAttach' | 'previewBind' | 'previewStatus' | 'previewInvalidate' |
  'previewDetach' | 'previewMode' | 'previewAck' | 'mapping' | 'graph'>;
type PairStatus = 'pairing' | 'ready' | 'arming' | 'interact' | 'awaiting_map' | 'stale' | 'detached';
type PairInput = { url: string; permission: string; buildMap: string; scope: ScopeVector };
type Waiting = {
  requestID: string;
  expected: BridgeMessage['type'];
  selectionID: string | null;
  timer: number;
  resolve: (message: BridgeMessage) => void;
  reject: (reason: Error) => void;
};
export type PreviewSelection = {
  selectionID: string;
  scopeDigest: string;
  anchor: DOMAnchorRef;
  instance: InstanceRef;
  match: MappingMatch;
  graphNode: GraphNode;
  ancestorFallback: boolean;
  instancesRead: number;
  moreInstances: boolean;
};

type PreviewInstance = Extract<BridgeMessage, { type: 'INSTANCES' }>['payload']['items'][number];
export type ReversePreview = {
  matches: MappingMatch[];
  mappingCursor: string | null;
  anchor: string | null;
  instances: PreviewInstance[];
  instanceCursor: string | null;
  selectedInstance: string | null;
  rects: Rect[];
};
type ReverseSelection = ReversePreview & {
  target: GraphRef;
  id: string;
  live: () => boolean;
};

const ackMS = 3_000;
const bridgeLimit = 16_384;

export function startPreviewPairing(iframe: HTMLIFrameElement, shield: HTMLElement, api: PreviewAPI, input: PairInput,
  report: (state: PairStatus) => void, select: (selection: PreviewSelection) => void, notice: (text: string) => void,
  showReverse: (value: ReversePreview | null) => void, buildChanged: (id: string) => void) {
  const url = new URL(input.url);
  const origin = canonicalOrigin(url.origin);
  checkPathname(url.pathname);
  if (origin === location.origin || !['127.0.0.1', 'localhost'].includes(url.hostname) ||
      url.username || url.password || url.search || url.hash ||
      !/^[a-f0-9]{64}$/.test(input.buildMap) || !input.permission) throw new Error('Địa chỉ preview chưa hợp lệ.');
  const abort = new AbortController();
  const used = new Set<string>();
  const invalidated = new Set<string>();
  let session: PreviewData | null = null;
  let peer: Window | null = null;
  let pending: Waiting | null = null;
  let receiveSeq = 0;
  let sendSeq = 0;
  let pollTimer = 0;
  let pollWork: AbortController | null = null;
  let modeWork: AbortController | null = null;
  let pickWork: AbortController | null = null;
  let reverse: ReverseSelection | null = null;
  let forwardSelectionID: string | null = null;
  let paired = false;
  let lastStatusMS = performance.now();
  let missedStatus = 0;
  let missedPong = 0;
  let initialLoad = true;
  let closed = false;

  function send(type: BridgeMessage['type'], payload: unknown, selectionID: string | null = null): string {
    if (!peer || !session) throw new Error('Preview frame unavailable');
    const id = crypto.randomUUID();
    const context = { protocol_version: 1, session_id: session.session_id, document_epoch: session.document_epoch,
      hmr_epoch: session.hmr_epoch, message_id: id, seq: ++sendSeq, reply_to: null, selection_id: selectionID };
    const raw = JSON.stringify({ ...context, type, payload });
    decodeBridgeMessage(raw, 'parent_to_sdk', bridgeLimit);
    peer.postMessage(raw, origin);
    return id;
  }

  function request(type: BridgeMessage['type'], payload: unknown, expected: BridgeMessage['type'], selectionID: string | null = null) {
    if (closed || pending) throw new Error('Preview request unavailable');
    return new Promise<BridgeMessage>((resolve, reject) => {
      const requestID = send(type, payload, selectionID);
      const timer = window.setTimeout(() => {
        if (pending?.requestID === requestID) pending = null;
        reject(new Error('Preview ACK unavailable'));
      }, ackMS);
      pending = { requestID, expected, selectionID, timer, resolve, reject };
    });
  }

  function receive(event: MessageEvent<unknown>) {
    const current = session;
    if (closed || !current || event.source !== peer || event.origin !== origin || typeof event.data !== 'string') return;
    let value: BridgeMessage;
    try { value = decodeBridgeMessage(event.data, 'sdk_to_parent', bridgeLimit); } catch { return; }
    if (value.session_id !== current.session_id || value.document_epoch !== current.document_epoch ||
        value.hmr_epoch !== current.hmr_epoch || value.seq <= receiveSeq || used.has(value.message_id)) return;
    if (paired && value.type === 'BUILD_CHANGED' && value.reply_to === null && value.selection_id === null &&
        value.payload.new_hmr_epoch > current.hmr_epoch && value.payload.build_map_id !== input.buildMap) {
      focusInspect();
      buildChanged(value.payload.build_map_id);
      finish('awaiting_map', 'hmr');
      return;
    }
    if (paired && value.type === 'INSTANCE_INVALIDATED' && value.reply_to === null && value.selection_id !== null &&
        (value.selection_id === forwardSelectionID || value.selection_id === reverse?.id)) {
      const ids = value.payload.instance_refs.filter(ref => ref.session_id === current.session_id &&
        ref.document_epoch === current.document_epoch && ref.hmr_epoch === current.hmr_epoch).map(ref => ref.instance_id);
      if (ids.length === 0) return;
      remember(value);
      for (const id of ids) {
        if (invalidated.size >= 256) invalidated.delete(invalidated.values().next().value!);
        invalidated.add(id);
      }
      if (value.selection_id === reverse?.id) {
        reverse.instances = reverse.instances.filter(item => !invalidated.has(item.instance_ref.instance_id));
        if (reverse.selectedInstance !== null && invalidated.has(reverse.selectedInstance)) {
          reverse.selectedInstance = null;
          reverse.rects = [];
        }
        showReverse({ ...reverse });
      }
      notice('Instance đã rời khỏi preview.');
      return;
    }
    if (!pending || value.type !== pending.expected || value.reply_to !== pending.requestID ||
        value.selection_id !== pending.selectionID) return;
    remember(value);
    const completed = pending;
    pending = null;
    window.clearTimeout(completed.timer);
    completed.resolve(value);
  }

  function remember(value: BridgeMessage) {
    receiveSeq = value.seq;
    if (used.size >= 256) used.delete(used.values().next().value!);
    used.add(value.message_id);
  }

  function focusInspect() {
    const permittedInput = !iframe.inert;
    shield.hidden = false;
    iframe.inert = true;
    iframe.tabIndex = -1;
    iframe.style.pointerEvents = 'none';
    if (permittedInput && !document.hidden) shield.focus({ preventScroll: true });
  }

  function stopPolling() {
    window.clearTimeout(pollTimer);
    pollWork?.abort();
    pollWork = null;
    if (pending) {
      window.clearTimeout(pending.timer);
      pending.reject(new Error('Preview request cancelled'));
      pending = null;
    }
  }

  function cancelPick() {
    pickWork?.abort();
    pickWork = null;
  }

  function clearReverse() {
    if (!reverse) return;
    const id = reverse.id;
    reverse = null;
    showReverse(null);
    if (!closed && paired) send('CLEAR_HIGHLIGHT', {}, id);
  }

  function finish(kind: 'stale' | 'detached' | 'awaiting_map',
    reason: 'client_error' | 'hmr' | 'build_changed' | 'navigation' = 'client_error') {
    if (closed) return;
    focusInspect();
    closed = true;
    abort.abort();
    modeWork?.abort();
    clearReverse();
    cancelPick();
    stopPolling();
    window.removeEventListener('message', receive);
    document.removeEventListener('visibilitychange', visibility);
    iframe.removeEventListener('load', loaded);
    const current = session;
    if (peer && current) {
      try { send('DISPOSE', {}); } catch { /* The frame may already be closed. */ }
    }
    iframe.removeAttribute('src');
    peer = null;
    session = null;
    used.clear();
    invalidated.clear();
    forwardSelectionID = null;
    report(kind);
    if (current) {
      const signal = AbortSignal.timeout(1_000);
      const update = kind === 'detached' ? api.previewDetach(current.session_id, signal)
        : api.previewInvalidate({ session_id: current.session_id, expected_revision: current.revision, reason }, signal);
      void update.catch(() => {});
    }
  }

  function visibility() {
    if (document.hidden && (modeWork || session?.mode_state === 'interact')) {
      focusInspect();
      if (modeWork) finish('stale');
      else void changeMode('inspect');
    } else if (!document.hidden && performance.now() - lastStatusMS > 10_000) finish('stale');
  }

  async function poll() {
    const current = session;
    if (closed || !current || modeWork || pickWork) return;
    const job = new AbortController();
    pollWork = job;
    const probe = crypto.randomUUID();
    const statusPromise = api.previewStatus(current.session_id, AbortSignal.any([abort.signal, job.signal, AbortSignal.timeout(2_000)]));
    const pingPromise = request('PING', { probe_id: probe }, 'PONG');
    const [status, pong] = await Promise.allSettled([statusPromise, pingPromise]);
    if (closed || pollWork !== job) return;
    pollWork = null;
    if (status.status === 'fulfilled') {
      const observed = status.value.data;
      if (!observed || observed.session_id !== current.session_id || observed.state !== 'bound' ||
          observed.permission_status !== 'approved' || observed.permission_version !== current.permission_version ||
          observed.revision !== current.revision || observed.document_epoch !== current.document_epoch ||
          observed.hmr_epoch !== current.hmr_epoch || observed.authorization_epoch !== current.authorization_epoch ||
          observed.mode_state !== current.mode_state || observed.interaction_allowed !== (current.mode_state === 'interact') ||
          observed.binding_id !== current.binding_id || status.value.scope?.vector_digest !== input.scope.vector_digest) {
        finish('stale');
        return;
      }
      missedStatus = 0;
      lastStatusMS = performance.now();
    } else if (status.reason instanceof ExplorerRequestError && (status.reason.status === 401 || status.reason.status === 403)) {
      finish('stale');
      return;
    } else missedStatus++;
    if (pong.status === 'fulfilled' && pong.value.type === 'PONG' && pong.value.payload.probe_id === probe) missedPong = 0;
    else missedPong++;
    if (missedStatus >= 2 || missedPong >= 2) finish('stale');
    else pollTimer = window.setTimeout(() => void poll(), document.hidden ? 5_000 : 2_000);
  }

  function sameSession(next: PreviewData, previous: PreviewData) {
    return next.state === 'bound' && next.session_id === previous.session_id && next.binding_id === previous.binding_id &&
      next.resolver_digest === previous.resolver_digest && next.document_epoch === previous.document_epoch &&
      next.hmr_epoch === previous.hmr_epoch && next.permission_version === previous.permission_version &&
      next.expires_at === previous.expires_at && next.pairing_nonce === null;
  }

  async function changeMode(mode: 'inspect' | 'interact') {
    const current = session;
    if (!current || !paired || closed || mode === 'interact' && document.hidden) return;
    if (modeWork) {
      if (mode === 'inspect') finish('stale');
      return;
    }
    if (current.mode_state === mode) return;
    focusInspect();
    cancelPick();
    clearReverse();
    stopPolling();
    const job = new AbortController();
    modeWork = job;
    const deadline = performance.now() + ackMS;
    const signal = AbortSignal.any([abort.signal, job.signal, AbortSignal.timeout(ackMS)]);
    if (mode === 'interact') report('arming');
    try {
      const response = await api.previewMode({ session_id: current.session_id, expected_revision: current.revision,
        expected_authorization_epoch: current.authorization_epoch, mode, permission_profile_id: input.permission }, signal);
      const changed = response.data;
      if (closed || modeWork !== job) return;
      if (!changed || !sameSession(changed, current) || changed.revision !== current.revision + 1 ||
          changed.authorization_epoch !== current.authorization_epoch + 1 || changed.command_id === null ||
          changed.mode_state !== (mode === 'interact' ? 'arming' : 'inspect'))
        throw new Error('Invalid preview mode revision');
      session = changed;
      const payload = { mode, authorization_epoch: changed.authorization_epoch, command_id: changed.command_id };
      if (mode === 'inspect') {
        send('SET_MODE', payload);
        report('ready');
        return;
      }
      if (response.scope?.vector_digest !== input.scope.vector_digest) throw new Error('Preview scope changed');
      const ack = await request('SET_MODE', payload, 'MODE_ACK');
      const match = ack.type === 'MODE_ACK' && ack.payload.mode === 'interact' && ack.payload.accepted &&
        ack.payload.authorization_epoch === changed.authorization_epoch && ack.payload.command_id === changed.command_id;
      if (!match || document.hidden || performance.now() >= deadline || closed || modeWork !== job)
        throw new Error('Preview did not acknowledge Interact');
      const result = await api.previewAck({ session_id: changed.session_id, expected_revision: changed.revision,
        authorization_epoch: changed.authorization_epoch, command_id: changed.command_id, acknowledged: true }, signal);
      const accepted = result.data;
      if (!accepted || !sameSession(accepted, changed) || accepted.mode_state !== 'interact' || accepted.command_id !== null ||
          accepted.authorization_epoch !== changed.authorization_epoch || accepted.revision !== changed.revision + 1 ||
          result.scope?.vector_digest !== input.scope.vector_digest || document.hidden ||
          performance.now() >= deadline || closed || modeWork !== job ||
          !(Date.now() < Date.parse(accepted.expires_at))) throw new Error('Interact approval no longer current');
      session = accepted;
      iframe.inert = false;
      iframe.tabIndex = 0;
      iframe.style.pointerEvents = 'auto';
      shield.hidden = true;
      report('interact');
    } catch (reason) {
      if (mode === 'interact' && session === current && reason instanceof ExplorerRequestError && reason.code === 'PREVIEW_NOT_APPROVED')
        report('ready');
      else finish('stale');
    } finally {
      if (modeWork === job) {
        modeWork = null;
        if (!closed) pollTimer = window.setTimeout(() => void poll(), 2_000);
      }
    }
  }

  async function runSelection(currentSelection: () => boolean,
    act: (current: PreviewData, signal: AbortSignal, active: () => boolean) => Promise<void>) {
    const current = session;
    if (closed || !paired || !current || modeWork || current.mode_state !== 'inspect' || document.hidden ||
        !currentSelection() || !(Date.now() < Date.parse(current.expires_at))) return;
    cancelPick();
    stopPolling();
    const job = new AbortController();
    pickWork = job;
    const signal = AbortSignal.any([abort.signal, job.signal, AbortSignal.timeout(8_000)]);
    const active = () => !closed && pickWork === job && session === current && !document.hidden && !signal.aborted && currentSelection();
    notice('Đang đối chiếu với index…');
    try {
      await act(current, signal, active);
    } catch (error) {
      if (job.signal.aborted || closed || pickWork !== job || !currentSelection()) return;
      if (error instanceof ExplorerRequestError && [401, 403, 409].includes(error.status)) finish('stale');
      else notice(error instanceof ExplorerRequestError ? error.message : 'Chưa thể xác minh lựa chọn. Hãy thử lại.');
    } finally {
      if (pickWork === job) {
        pickWork = null;
        if (!currentSelection()) notice('');
        if (!closed && !modeWork) pollTimer = window.setTimeout(() => void poll(), 2_000);
      }
    }
  }

  async function pick(clientX: number | null, clientY: number | null, kind: 'element' | 'component', currentSelection: () => boolean) {
    const rect = iframe.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return;
    const x = clientX === null ? iframe.clientWidth / 2 : (clientX - rect.left) * iframe.offsetWidth / rect.width - iframe.clientLeft;
    const y = clientY === null ? iframe.clientHeight / 2 : (clientY - rect.top) * iframe.offsetHeight / rect.height - iframe.clientTop;
    if (!Number.isFinite(x) || !Number.isFinite(y) || x < 0 || y < 0 || x >= iframe.clientWidth || y >= iframe.clientHeight) return;
    await runSelection(currentSelection, async (current, signal, active) => {
      clearReverse();
      const selectionID = crypto.randomUUID();
      forwardSelectionID = selectionID;
      const picked = await request('PICK_AT', { x_css: x, y_css: y, selection_kind: kind }, 'PICK_RESULT', selectionID);
      if (!active() || picked.type !== 'PICK_RESULT') return;
      const hit = picked.payload.targets[0];
      if (!hit || hit.anchor_ref.build_map_id !== input.buildMap ||
          picked.payload.status !== 'exact_in_supported_scope' && picked.payload.status !== 'ancestor_fallback') {
        notice('Điểm chọn chưa có anchor được hỗ trợ.');
        return;
      }
      const result = await api.mapping({ scope: { pinned_vector: input.scope }, session_id: current.session_id,
        build_map_id: input.buildMap, document_epoch: current.document_epoch, hmr_epoch: current.hmr_epoch,
        ref: hit.anchor_ref, limit: 2 }, signal);
      const data = result.data;
      const match = data?.matches[0];
      if (!active()) return;
      if (!data || !match?.target_ref || data.mapping_status !== 'exact_in_supported_scope' || data.matches.length !== 1 ||
          data.binding_id !== current.binding_id || data.resolver_digest !== current.resolver_digest ||
          data.build_map_id !== input.buildMap || match.anchor_ref.anchor_id !== hit.anchor_ref.anchor_id ||
          result.scope?.vector_digest !== input.scope.vector_digest) {
        notice('Chưa có một liên kết nguồn duy nhất cho điểm chọn.');
        return;
      }
      await verifyScopeDigest(result.scope, 32768);
      const seen = new Set<string>();
      let cursor: string | null = null;
      for (let i = 0; i < 3; i++) {
        if (!active()) return;
        const reply = await request('LIST_INSTANCES', { anchor_refs: [hit.anchor_ref], limit: 100, cursor }, 'INSTANCES', selectionID);
        if (reply.type !== 'INSTANCES' || !active()) return;
        for (const item of reply.payload.items) {
          if (item.anchor_ref.anchor_id === hit.anchor_ref.anchor_id && item.anchor_ref.build_map_id === input.buildMap)
            seen.add(item.instance_ref.instance_id);
        }
        if (cursor !== null && cursor === reply.payload.next_cursor) throw new Error('Invalid instance continuation');
        cursor = reply.payload.next_cursor;
        if (cursor === null) break;
      }
      if (!active()) return;
      if (!seen.has(hit.instance_ref.instance_id) || invalidated.has(hit.instance_ref.instance_id)) {
        notice('Instance đã thay đổi. Chọn lại trong preview.');
        return;
      }
      const graph = await api.graph({ root: { kind: 'ref', ref: match.target_ref, service_id: null }, scope: { pinned_vector: input.scope },
        depth: 0, node_limit: 2, edge_limit: 1 }, signal);
      if (!active() || invalidated.has(hit.instance_ref.instance_id)) return;
      if (!graph.data || !graph.scope || graph.scope.vector_digest !== input.scope.vector_digest ||
          graph.pagination.next_cursor !== null) throw new Error('Graph target unavailable');
      await verifyScopeDigest(graph.scope, 32768);
      if (await subgraphDigest(graph.scope, graph.data.nodes, graph.data.edges, 2 << 20) !== graph.data.subgraph_digest)
        throw new Error('Graph target digest changed');
      const targetDigest = await graphRefDigest(match.target_ref, 8192);
      const candidates = await Promise.all(graph.data.nodes.map(async node =>
        await graphRefDigest(node.ref, 8192) === targetDigest ? node : undefined));
      const graphNode = candidates.find(node => node !== undefined);
      if (!active() || invalidated.has(hit.instance_ref.instance_id)) return;
      if (!graphNode) throw new Error('Graph target mismatch');
      select({ selectionID, scopeDigest: input.scope.vector_digest, anchor: hit.anchor_ref, instance: hit.instance_ref,
        match, graphNode, ancestorFallback: picked.payload.status === 'ancestor_fallback',
        instancesRead: seen.size, moreInstances: cursor !== null });
      notice(picked.payload.status === 'ancestor_fallback' ? 'Đã chọn ranh giới tổ tiên có bằng chứng.' : '');
    });
  }

  async function readMappingPage(plan: ReverseSelection, more: boolean) {
    await runSelection(plan.live, async (current, signal, active) => {
      const cursor = more ? plan.mappingCursor : null;
      const response = await api.mapping({ scope: { pinned_vector: input.scope }, session_id: current.session_id,
        build_map_id: input.buildMap, document_epoch: current.document_epoch, hmr_epoch: current.hmr_epoch,
        ref: plan.target, limit: 20, ...(cursor === null ? {} : { cursor }) }, signal);
      if (!active() || reverse !== plan) return;
      const data = response.data;
      if (!data || !response.scope || response.scope.vector_digest !== input.scope.vector_digest ||
          data.build_map_id !== input.buildMap || data.binding_id !== current.binding_id ||
          data.resolver_digest !== current.resolver_digest || !('kind' in data.requested_ref))
        throw new Error('Mapping scope changed');
      await verifyScopeDigest(response.scope, 32768);
      const target = await graphRefDigest(plan.target, 8192);
      if (target !== await graphRefDigest(data.requested_ref, 8192)) throw new Error('Mapping target changed');
      let last = more ? plan.matches.at(-1)?.anchor_ref.anchor_id ?? '' : '';
      for (const match of data.matches) {
        if (!match.target_ref || await graphRefDigest(match.target_ref, 8192) !== target ||
            match.anchor_ref.anchor_id <= last || match.anchor_ref.binding_id !== current.binding_id)
          throw new Error('Mapping pair changed');
        last = match.anchor_ref.anchor_id;
      }
      const next = response.pagination.next_cursor;
      if (next !== null && (next === cursor || data.matches.length === 0)) throw new Error('Mapping did not advance');
      if (!active() || reverse !== plan) return;
      send('CLEAR_HIGHLIGHT', {}, plan.id);
      Object.assign(plan, { matches: data.matches, mappingCursor: next, anchor: null,
        instances: [], instanceCursor: null, selectedInstance: null, rects: [] });
      showReverse({ ...plan });
      notice(data.matches.length ? 'Chọn một vị trí nguồn để xem instance.' : 'Snapshot chưa có liên kết preview cho nút này.');
    });
  }

  function startReverse(target: GraphRef, live: () => boolean) {
    clearReverse();
    forwardSelectionID = null;
    const plan: ReverseSelection = { target, live, id: crypto.randomUUID(), matches: [], mappingCursor: null,
      anchor: null, instances: [], instanceCursor: null, selectedInstance: null, rects: [] };
    reverse = plan;
    return readMappingPage(plan, false);
  }

  async function readInstancePage(anchor: string, more = false) {
    const plan = reverse;
    if (!plan || !plan.matches.some(match => match.anchor_ref.anchor_id === anchor)) return;
    await runSelection(plan.live, async (_current, _signal, active) => {
      const cursor = more ? plan.instanceCursor : null;
      const result = await request('LIST_INSTANCES', { anchor_refs: [{ build_map_id: input.buildMap, anchor_id: anchor }],
        limit: 40, cursor }, 'INSTANCES', plan.id);
      if (result.type !== 'INSTANCES' || !active() || reverse !== plan) return;
      const { items, next_cursor } = result.payload;
      if (items.some(item => item.anchor_ref.anchor_id !== anchor || item.anchor_ref.build_map_id !== input.buildMap) ||
          next_cursor !== null && (next_cursor === cursor || items.length === 0)) throw new Error('Invalid instance page');
      send('CLEAR_HIGHLIGHT', {}, plan.id);
      Object.assign(plan, { anchor, instances: items.filter(item => !invalidated.has(item.instance_ref.instance_id)),
        instanceCursor: next_cursor, selectedInstance: null, rects: [] });
      showReverse({ ...plan });
      notice(items.length ? '' : 'Vị trí này chưa được render trong preview hiện tại.');
    });
  }

  async function revealInstance(instanceID: string, scroll: boolean) {
    const plan = reverse;
    const item = plan?.instances.find(entry => entry.instance_ref.instance_id === instanceID);
    if (!plan || !item) return;
    await runSelection(plan.live, async (current, signal, active) => {
      plan.rects = [];
      showReverse({ ...plan });
      if (scroll) {
        const result = await api.previewStatus(current.session_id, signal);
        if (!active() || reverse !== plan) return;
        const observed = result.data;
        if (!result.scrollAllowed || !observed || observed.session_id !== current.session_id || observed.state !== 'bound' ||
            observed.mode_state !== 'inspect' || observed.permission_status !== 'approved' ||
            observed.permission_version !== current.permission_version || observed.revision !== current.revision ||
            observed.document_epoch !== current.document_epoch || observed.hmr_epoch !== current.hmr_epoch ||
            observed.authorization_epoch !== current.authorization_epoch || observed.binding_id !== current.binding_id ||
            result.scope?.vector_digest !== input.scope.vector_digest || !(Date.now() < Date.parse(observed.expires_at))) {
          notice('Phiên hiện tại chưa cấp quyền cuộn preview.');
          return;
        }
        await verifyScopeDigest(result.scope, 32768);
        if (!active()) return;
        const resultScroll = await request('REQUEST_SCROLL', { instance_ref: item.instance_ref }, 'SCROLL_RESULT', plan.id);
        if (resultScroll.type !== 'SCROLL_RESULT' || !active() || reverse !== plan) return;
        notice(resultScroll.payload.status === 'done' ? 'Đã cuộn đến instance. Tải lại danh sách để xem vị trí mới.' :
          resultScroll.payload.status === 'not_rendered' ? 'Instance chưa được render.' : 'SDK chưa cho phép cuộn.');
        return;
      }
      const result = await request('HIGHLIGHT', { instance_refs: [item.instance_ref] }, 'HIGHLIGHT_RESULT', plan.id);
      if (result.type !== 'HIGHLIGHT_RESULT' || !active() || reverse !== plan || invalidated.has(instanceID)) return;
      const matched = result.payload.matched.some(ref => ref.instance_id === instanceID &&
        ref.session_id === current.session_id && ref.document_epoch === current.document_epoch && ref.hmr_epoch === current.hmr_epoch);
      plan.selectedInstance = matched ? instanceID : null;
      plan.rects = matched && item.visibility === 'visible' ? item.rects : [];
      showReverse({ ...plan });
      notice(plan.rects.length ? '' : 'Instance chưa hiển thị trong viewport.');
    });
  }

  async function pair() {
    const current = session;
    if (!current?.pairing_nonce) throw new Error('Pairing nonce unavailable');
    const ack = await request('CHALLENGE', { nonce: current.pairing_nonce, approved_origin: origin }, 'READY');
    if (ack.type !== 'READY' || ack.payload.nonce !== current.pairing_nonce || ack.payload.build_map_id !== input.buildMap)
      throw new Error('Build or nonce mismatch');
    const result = await api.previewBind({ session_id: current.session_id, expected_revision: current.revision,
      document_epoch: current.document_epoch, hmr_epoch: current.hmr_epoch, build_map_id: input.buildMap,
      pairing_nonce: current.pairing_nonce, capabilities: ack.payload.capabilities }, abort.signal);
    const bound = result.data;
    if (!bound || bound.session_id !== current.session_id || bound.state !== 'bound' || bound.mode_state !== 'inspect' ||
        bound.binding_id === null || bound.pairing_nonce !== null || bound.revision !== current.revision + 1 ||
        bound.document_epoch !== current.document_epoch || bound.hmr_epoch !== current.hmr_epoch ||
        bound.authorization_epoch !== current.authorization_epoch || bound.permission_version !== current.permission_version ||
        result.scope?.vector_digest !== input.scope.vector_digest) throw new Error('Preview bind mismatch');
    session = bound;
    const value = await request('BOUND', { build_map_id: input.buildMap, authorization_epoch: bound.authorization_epoch }, 'BOUND_ACK');
    if (value.type !== 'BOUND_ACK' || value.payload.build_map_id !== input.buildMap ||
        value.payload.authorization_epoch !== bound.authorization_epoch) throw new Error('Preview acknowledgement mismatch');
    if (closed) return;
    lastStatusMS = performance.now();
    paired = true;
    report('ready');
    pollTimer = window.setTimeout(() => void poll(), 2_000);
  }

  function loaded() {
    if (closed || !session) return;
    if (!initialLoad) {
      finish('stale', 'navigation');
      return;
    }
    initialLoad = false;
    window.clearTimeout(pollTimer);
    peer = iframe.contentWindow;
    void pair().catch(reason => reason instanceof ExplorerRequestError && reason.code === 'BUILD_MAP_MISSING'
      ? finish('awaiting_map', 'build_changed') : finish('stale'));
  }

  async function attach() {
    const result = await api.previewAttach({ origin, path: url.pathname, build_map_id: input.buildMap,
      permission_profile_id: input.permission, scope: { pinned_vector: input.scope } }, abort.signal);
    if (closed) return;
    const attached = result.data;
    if (!attached || attached.state !== 'pairing' || attached.revision !== 1 || attached.document_epoch !== 1 ||
        attached.hmr_epoch !== 0 || attached.authorization_epoch !== 0 || !attached.pairing_nonce ||
        result.scope?.vector_digest !== input.scope.vector_digest) throw new Error('Preview scope mismatch');
    session = attached;
    iframe.sandbox.value = 'allow-scripts allow-same-origin';
    iframe.referrerPolicy = 'no-referrer';
    iframe.src = origin + url.pathname;
    pollTimer = window.setTimeout(() => { if (initialLoad) finish('stale'); }, 10_000);
  }

  focusInspect();
  report('pairing');
  window.addEventListener('message', receive);
  document.addEventListener('visibilitychange', visibility);
  iframe.addEventListener('load', loaded);
  void attach().catch(reason => reason instanceof ExplorerRequestError && reason.code === 'BUILD_MAP_MISSING'
    ? finish('awaiting_map', 'build_changed') : finish('stale'));
  return { close: () => finish('detached'), changeMode, pick, clearReverse, startReverse, readInstancePage, revealInstance,
    moreMappings: () => reverse?.mappingCursor && readMappingPage(reverse, true) };
}
