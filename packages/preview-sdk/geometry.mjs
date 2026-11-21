const cssMagnitude = 1_000_000;
const html = 'http://www.w3.org/1999/xhtml';

export function opaqueElement(element) {
  return element.namespaceURI !== html || element.localName.includes('-') ||
    ['iframe', 'canvas', 'object', 'embed'].includes(element.localName) || element.shadowRoot !== null;
}

export function insideViewport(window, x, y) {
  return Number.isFinite(x) && Number.isFinite(y) && x >= 0 && y >= 0 &&
    x < window.innerWidth && y < window.innerHeight;
}

function clippingBounds(window, root) {
  const bounds = { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
  for (let parent = root.parentElement, depth = 0; parent && depth < 64; parent = parent.parentElement, depth++) {
    const style = window.getComputedStyle(parent);
    if (style.opacity === '0' || style.contentVisibility === 'hidden') return null;
    const clipsX = style.overflowX !== 'visible';
    const clipsY = style.overflowY !== 'visible';
    if (!clipsX && !clipsY) continue;
    const box = parent.getBoundingClientRect();
    const sx = parent.offsetWidth > 0 ? box.width / parent.offsetWidth : 1;
    const sy = parent.offsetHeight > 0 ? box.height / parent.offsetHeight : 1;
    const left = box.x + parent.clientLeft * sx;
    const top = box.y + parent.clientTop * sy;
    if (clipsX) {
      bounds.left = Math.max(bounds.left, left);
      bounds.right = Math.min(bounds.right, left + parent.clientWidth * sx);
    }
    if (clipsY) {
      bounds.top = Math.max(bounds.top, top);
      bounds.bottom = Math.min(bounds.bottom, top + parent.clientHeight * sy);
    }
  }
  return bounds;
}

// getClientRects supplies viewport CSS pixels after layout and transforms.
// A multi-root boundary keeps separate rectangles.
export function readGeometry(window, roots) {
  const rects = [];
  let visible = false;
  for (const root of roots) {
    const style = window.getComputedStyle(root);
    if (style.visibility !== 'visible' || style.display === 'none' || style.opacity === '0' ||
        style.contentVisibility === 'hidden') continue;
    const bounds = clippingBounds(window, root);
    if (bounds === null) continue;
    const layout = root.getClientRects();
    for (let i = 0; i < layout.length && i < 32; i++) {
      const rect = layout[i];
      const { x, y, width, height } = rect;
      if (width <= 0 || height <= 0 || ![x, y, width, height].every(n => Number.isFinite(n) && Math.abs(n) <= cssMagnitude))
        continue;
      const overlap = Math.max(x, bounds.left) < Math.min(x + width, bounds.right) &&
        Math.max(y, bounds.top) < Math.min(y + height, bounds.bottom);
      visible ||= overlap;
      rects.push({ x, y, width, height });
      if (rects.length === 16) break;
    }
    if (rects.length === 16) break;
  }
  return { visibility: visible ? 'visible' : rects.length ? 'mounted_offscreen' : 'hidden', rects };
}
