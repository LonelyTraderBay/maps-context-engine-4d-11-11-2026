# Preview SDK instance registry

`createInstanceRegistry(document, identity, anchorIDs)` creates one local registry for an approved document/build epoch. Its input uses the session, document epoch, HMR epoch and build map ID from S43 and the build's known opaque anchor IDs.

- `pickAt(x, y)` reads the top DOM element at viewport CSS coordinates. It returns an exact supported host or an explicit ancestor fallback with the corresponding rectangles.
- `listInstances(anchorRefs, limit, cursor)` discovers hosts on a bounded walk when requested and pages known instances in memory. Each cursor belongs to one registry and one selected anchor set.
- `registerBoundary(anchorID, elements, parentRef)` registers one logical instance for up to 16 public DOM handles. Call the returned function on cleanup. Its `instanceRef` can name a known parent in a later explicit registration.
- `observe(instanceRef, listener)` observes one active instance at a time and reports coalesced geometry changes. It reports `null` when that instance becomes invalid and returns a cleanup function. Call `dispose()` at the end of the document or build epoch.

The React adapter's `registerReactBoundary` accepts public DOM refs and returns cleanup for a layout effect. Multiple DOM roots retain separate rectangles. The supported fixture covers scroll containers, CSS zoom and simple transforms. Canvas, nested frame, shadow internals and undeclared portal ownership expose only supported ancestor evidence or an unavailable result. The registry carries opaque IDs and geometry. It sends no messages or input events itself.

The developer's cooperative dev integration can call `buildChanged(nextBuildMapID)` on the handle returned by `installPreviewSDK` after publishing the new build map. This ends the active SDK binding and sends the new opaque map ID to Explorer. Explorer immediately shields and detaches the frame. The owner can pair again in Inspect after the new map binds to matching indexed bytes. The SDK also reports an invalidation when its currently tracked instance unmounts.

## Observe in a cooperative test app

Pass an owner-approved `observeApproval` to `installPreviewSDK` for an isolated test deployment. Set `environment: 'test'`, an absolute `expiresAt` in epoch milliseconds, the exact `buildMapID`, `profileID` and `revision`, and approved `application`, `service`, `collector`, `instrumentation` and `operations` labels. Each destination specifies an exact loopback `origin`, a `pathPrefix` and allowed HTTP `methods`. The installation owner provides these settings from the approved test environment and keeps them within its destination policy.

- Call `sdk.observations.arm(event)` from the app's Observe button after Explorer confirms Interact. Use `armed()` for the app's visible status.
- Call `begin(event)` in a later trusted click or submit handler. Pass that handle to `fetchForAction(handle, url, init, operation)` for explicitly instrumented requests. Ordinary requests and requests with existing trace carriers keep their existing behavior.
- Call `exportForAction(handle, event)` from an explicit export gesture after requests complete. It returns one trace artifact and separate local correlation receipts. The application presents these to the local owner for approval and import. The SDK transfers no export through the bridge.
- Browser CORS rules apply to propagation. A blocked or failed request can produce a client attempt with error status. A backend relation requires a separately imported span with explicit parent context. Traced requests reject redirects to keep the propagation within approved destinations.
- Returning to Inspect, hiding the document, changing the build, navigating away, expiry and disposal clear the current capture. Caps apply to actions and traced requests. Call `disarm()` to stop capture from the test app.

The local owner can use `DecodeObserveReceipt` and `BindObserveReceipt` with the existing imported artifact and an authorization callback. The callback verifies the approved export, receipt, current policy and scope. An installation configured with an observed owner lets the Explorer operator open the admitted artifact by ID from Flow.
