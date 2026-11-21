import { insideViewport, opaqueElement, readGeometry } from './geometry.mjs';

const hash = /^[0-9a-f]{64}$/;
const uuid = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/;
const attribute = 'data-mce4d-anchor';
const maxInstances = 1024;
const maxWalk = 8192;
const maxParents = 64;
const maxBodyBytes = 15 * 1024;

export function createInstanceRegistry(document, identity, anchorIDs) {
  const window = document.defaultView;
  const { session_id, document_epoch, hmr_epoch, build_map_id } = identity;
  if (window === null || !uuid.test(session_id) || !hash.test(build_map_id) ||
      !Number.isSafeInteger(document_epoch) || document_epoch < 1 ||
      !Number.isSafeInteger(hmr_epoch) || hmr_epoch < 0 ||
      !Array.isArray(anchorIDs) || anchorIDs.length > maxWalk || anchorIDs.some(id => typeof id !== 'string' || !hash.test(id)))
    throw new Error('Invalid instance registry context');
  const anchors = new Set(anchorIDs);
  const identityFields = { session_id, document_epoch, hmr_epoch };
  const entries = new Map();
  const hostEntries = new WeakMap();
  const boundaryEntries = new WeakMap();
  const pages = new Map();
  let disposed = false;
  let stopObservation = null;

  function live(element) {
    return element instanceof window.Element && element.ownerDocument === document &&
      element.isConnected && element.getRootNode() === document;
  }

  function rootsOf(entry) {
    const elements = entry.roots.map(root => root.deref());
    if (elements.every(live) && (entry.explicit || elements[0].getAttribute(attribute) === entry.anchor)) {
      return elements;
    }
    entries.delete(entry.ref.instance_id);
    return null;
  }

  function find(ref) {
    if (!ref || ref.session_id !== session_id || ref.document_epoch !== document_epoch || ref.hmr_epoch !== hmr_epoch)
      return null;
    const entry = entries.get(ref.instance_id);
    return entry && rootsOf(entry) ? entry : null;
  }

  function register(anchor, elements, explicit, parent) {
    if (entries.size >= maxInstances) return null;
    const ref = Object.freeze({ ...identityFields, instance_id: window.crypto.randomUUID() });
    const entry = { anchor, ref, parent, roots: elements.map(element => new WeakRef(element)), explicit };
    entries.set(ref.instance_id, entry);
    return entry;
  }

  function registerBoundary(anchor, roots, parent = null) {
    const elements = Array.isArray(roots) && roots.length <= 16 ? [...new Set(roots)] : [];
    const owner = parent === null ? null : find(parent);
    if (disposed || !anchors.has(anchor) || elements.length === 0 || parent !== null && owner === null ||
        elements.some(element => {
          if (!live(element)) return true;
          const previous = boundaryEntries.get(element);
          return previous !== undefined && find(previous.ref) !== null;
        })) throw new Error('Invalid component boundary');
    if (entries.size >= maxInstances) {
      for (const entry of entries.values()) rootsOf(entry);
    }
    const entry = register(anchor, elements, true, owner?.ref ?? null);
    if (entry === null) throw new Error('Instance registry limit');
    for (const element of elements) boundaryEntries.set(element, entry);
    const unregister = () => {
      entries.delete(entry.ref.instance_id);
      for (const root of entry.roots) {
        const element = root.deref();
        if (element && boundaryEntries.get(element) === entry) boundaryEntries.delete(element);
      }
      requestObservation();
    };
    return Object.assign(unregister, { instanceRef: entry.ref });
  }

  function hostAt(element) {
    const anchor = element.getAttribute(attribute);
    if (!anchors.has(anchor) || opaqueElement(element)) return null;
    const previous = hostEntries.get(element);
    if (previous?.anchor === anchor && find(previous.ref)) return previous;
    const entry = register(anchor, [element], false, null);
    if (entry) hostEntries.set(element, entry);
    return entry;
  }

  function sample(entry) {
    const elements = rootsOf(entry);
    if (!elements) return null;
    return {
      instance_ref: entry.ref,
      anchor_ref: { build_map_id, anchor_id: entry.anchor },
      ...readGeometry(window, elements),
    };
  }

  function highlightStatus(refs) {
    const matched = [];
    const unavailable = [];
    for (const ref of refs) {
      const entry = find(ref);
      const output = entry && sample(entry)?.visibility === 'visible' ? matched : unavailable;
      output.push(ref);
    }
    return { matched, unavailable };
  }

  function scrollIntoView(ref) {
    const entry = find(ref);
    const roots = entry && rootsOf(entry);
    if (!roots?.length) return 'not_rendered';
    const item = sample(entry);
    if (item?.visibility === 'hidden') return 'not_rendered';
    roots[0].scrollIntoView({ behavior: 'instant', block: 'nearest', inline: 'nearest' });
    return 'done';
  }

  function pickAt(x, y, selectionKind = 'element') {
    const empty = status => ({ targets: [], status });
    if (disposed || !insideViewport(window, x, y)) return empty('unmapped');
    const hit = document.elementFromPoint(x, y);
    if (!hit) return empty('unmapped');
    const candidates = [];
    const seen = new Set();
    function add(entry) {
      if (entry && !seen.has(entry.ref.instance_id)) {
        seen.add(entry.ref.instance_id);
        candidates.push(entry);
      }
    }
    let cursor = hit;
    for (let depth = 0; cursor && depth < maxParents && candidates.length < 8; depth++, cursor = cursor.parentElement) {
      add(hostAt(cursor));
      const boundary = boundaryEntries.get(cursor);
      if (boundary) add(find(boundary.ref));
    }
    if (selectionKind === 'component') {
      const selected = candidates.findIndex(entry => entry.explicit);
      if (selected === -1) return empty('unmapped');
      const [boundary] = candidates.splice(selected, 1);
      candidates.unshift(boundary);
    }
    const first = candidates[0];
    if (!first) return empty(opaqueElement(hit) ? 'unsupported' : 'unmapped');
    for (const entry of candidates.slice()) add(find(entry.parent));
    const targets = [];
    for (const entry of candidates.slice(0, 8)) {
      const { visibility, ...item } = sample(entry) ?? {};
      if (visibility !== 'visible') continue;
      const parent = find(entry.parent)?.ref ?? null;
      const target = { ...item, parent_instance_ref: parent };
      if (JSON.stringify([...targets, target]).length > maxBodyBytes) break;
      targets.push(target);
    }
    if (targets.length === 0) return empty('unmapped');
    const exact = !opaqueElement(hit) && targets[0].instance_ref === first.ref &&
      (rootsOf(first)?.includes(hit) || selectionKind === 'component' && first.explicit);
    return { targets, status: exact ? 'exact_in_supported_scope' : 'ancestor_fallback' };
  }

  function listInstances(anchorRefs, limit, cursor = null) {
    if (disposed || !Array.isArray(anchorRefs) || anchorRefs.length > 100 ||
        !Number.isInteger(limit) || limit < 1 || limit > 100 ||
        anchorRefs.some(ref => !ref || ref.build_map_id !== build_map_id || !anchors.has(ref.anchor_id)))
      throw new Error('Invalid instance page');
    const selected = new Set(anchorRefs.map(ref => ref.anchor_id));
    const key = [...selected].sort().join(',');
    let page;
    if (cursor === null) {
      for (const entry of entries.values()) rootsOf(entry);
      const walker = document.createTreeWalker(document.documentElement, window.NodeFilter.SHOW_ELEMENT);
      for (let i = 0, element = walker.currentNode; element && i < maxWalk; i++, element = walker.nextNode()) {
        const anchor = element.getAttribute(attribute);
        if (selected.size === 0 || selected.has(anchor)) hostAt(element);
      }
      page = { refs: [...entries.values()].filter(entry => selected.size === 0 || selected.has(entry.anchor)).map(entry => entry.ref), at: 0, key };
    } else {
      page = pages.get(cursor);
      if (!page || page.key !== key) throw new Error('Stale instance page');
      pages.delete(cursor);
    }
    const items = [];
    while (page.at < page.refs.length && items.length < limit) {
      const entry = find(page.refs[page.at]);
      const item = entry && sample(entry);
      if (item && JSON.stringify([...items, item]).length > maxBodyBytes) break;
      page.at++;
      if (item) items.push(item);
    }
    let next_cursor = null;
    if (page.at < page.refs.length) {
      if (pages.size >= 16) pages.delete(pages.keys().next().value);
      next_cursor = window.crypto.randomUUID();
      pages.set(next_cursor, page);
    }
    return { items, next_cursor, pending: null };
  }

  let requestObservation = () => {};
  function observe(ref, listener) {
    const entry = find(ref);
    if (disposed || !entry || typeof listener !== 'function') throw new Error('Unknown observed instance');
    stopObservation?.();
    const elements = rootsOf(entry);
    let frame = 0;
    let previous = '';
    const refresh = () => {
      frame = 0;
      const current = find(ref) ? sample(entry) : null;
      const text = JSON.stringify(current);
      if (!current) stop();
      if (text !== previous) {
        previous = text;
        listener(current);
      }
    };
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(refresh);
    };
    const resize = new window.ResizeObserver(schedule);
    const mutations = new window.MutationObserver(schedule);
    const observed = new Set(elements);
    for (const element of elements) {
      resize.observe(element);
      mutations.observe(element, { childList: true, attributes: true, attributeFilter: [attribute, 'style', 'class', 'hidden'] });
      for (let parent = element.parentElement, depth = 0; parent && depth < maxParents;
        parent = parent.parentElement, depth++) {
        if (observed.has(parent)) continue;
        observed.add(parent);
        mutations.observe(parent, { childList: true, attributes: true, attributeFilter: ['style', 'class', 'hidden'] });
      }
    }
    const stop = () => {
      if (stopObservation !== stop) return;
      resize.disconnect();
      mutations.disconnect();
      window.removeEventListener('resize', schedule);
      window.removeEventListener('scroll', schedule, true);
      if (frame) window.cancelAnimationFrame(frame);
      stopObservation = null;
      requestObservation = () => {};
    };
    stopObservation = stop;
    requestObservation = schedule;
    window.addEventListener('resize', schedule);
    window.addEventListener('scroll', schedule, true);
    schedule();
    return stop;
  }

  return Object.freeze({
    registerBoundary, pickAt, listInstances, highlightStatus, scrollIntoView, observe,
    dispose() {
      stopObservation?.();
      entries.clear();
      pages.clear();
      disposed = true;
    },
  });
}
