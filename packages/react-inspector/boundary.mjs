// Call from a layout effect using public DOM refs. The function returned by
// the SDK also serves as the effect's cleanup, including Strict Mode remounts.
export function registerReactBoundary(registry, anchorID, refs, parent = null) {
  if (!Array.isArray(refs) || refs.length < 1 || refs.length > 16) throw new Error('Invalid component refs');
  return registry.registerBoundary(anchorID, refs.map(ref => ref.current), parent);
}
