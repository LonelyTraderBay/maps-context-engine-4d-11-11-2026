# Preview SDK instance registry

`createInstanceRegistry(document, identity, anchorIDs)` creates one local registry for an approved document/build epoch. Its input uses the session, document epoch, HMR epoch and build map ID from S43 and the build's known opaque anchor IDs.

- `pickAt(x, y)` reads the top DOM element at viewport CSS coordinates. It returns an exact supported host or an explicit ancestor fallback with the corresponding rectangles.
- `listInstances(anchorRefs, limit, cursor)` discovers hosts on a bounded walk when requested and pages known instances in memory. Each cursor belongs to one registry and one selected anchor set.
- `registerBoundary(anchorID, elements, parentRef)` registers one logical instance for up to 16 public DOM handles. Call the returned function on cleanup. Its `instanceRef` can name a known parent in a later explicit registration.
- `observe(instanceRef, listener)` observes one active instance at a time and reports coalesced geometry changes. It reports `null` when that instance becomes invalid and returns a cleanup function. Call `dispose()` at the end of the document or build epoch.

The React adapter's `registerReactBoundary` accepts public DOM refs and returns cleanup for a layout effect. Multiple DOM roots retain separate rectangles. The supported fixture covers scroll containers, CSS zoom and simple transforms. Canvas, nested frame, shadow internals and undeclared portal ownership expose only supported ancestor evidence or an unavailable result. The registry carries opaque IDs and geometry. It sends no messages or input events itself.

The developer's cooperative dev integration can call `buildChanged(nextBuildMapID)` on the handle returned by `installPreviewSDK` after publishing the new build map. This ends the active SDK binding and sends the new opaque map ID to Explorer. Explorer immediately shields and detaches the frame. The owner can pair again in Inspect after the new map binds to matching indexed bytes. The SDK also reports an invalidation when its currently tracked instance unmounts.
