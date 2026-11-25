import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { edgeRelationLabel, groupVariableEdges, traceFlowEdges } from '../../web/explorer/src/shell/map.ts';
import {
  anchorDigest, bindingDigest, buildMapDigest, canonicalBytes, canonicalDigest,
  checkPagination, checkRelativePath, decodeBridgeMessage, decodeBuildManifest,
  decodeHTTPRequest, decodeHTTPResponse, decodePermissionProfile, decodeWorkspaceQuery,
  httpLimits, matchPreviewURL, parseJSON, pathWithinPrefix, permissionDigest,
  scopeDigest, scopeSelection, sha256, subgraphDigest, utf8, verifyBuildCaptured, verifyBuildIdentity, verifyScopeDigest,
} from '../../web/explorer/src/index.ts';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const { $defs: defs } = JSON.parse(read('../../contracts/mce4d.v1.schema.json'));
const wire = JSON.parse(read('../../contracts/explorer-wire.v1.json'));
const id = '01234567-89ab-cdef-0123-456789abcdef';
const alt = '01234567-89ab-cdef-0123-456789abcdea';
const hash = digit => digit.repeat(64);
const bytes = value => utf8.encode(JSON.stringify(value));
const limit = 131072;
const at = '2026-11-21T04:00:00Z';
const later = '2026-11-21T04:00:00.000000001Z';
const fixed = { UUID: id, Hash: hash('a'), ID: 'x', Token: 'sample', Path: 'x.ts', Time: at };

test('a grouped variable arrow retains each read and write occurrence', () => {
  const occurrence = (relation_kind, index) => ({
    edge_id: String(index), from_node_id: 'handler', to_node_id: 'variable',
    relation_kind, resolution_status: 'exact_in_supported_scope',
    evidence_refs: [{ evidence_id: String(index) }],
  });
  const [grouped, other] = groupVariableEdges([
    occurrence('READS', 0), occurrence('WRITES', 1), occurrence('READS', 2), occurrence('REFERS_TO', 3),
  ]);
  assert.equal(grouped.edge_id, '0');
  assert.equal(edgeRelationLabel(grouped), 'Đọc và ghi biến');
  assert.deepEqual(grouped.accessKinds, ['READS', 'WRITES', 'READS']);
  assert.deepEqual(grouped.evidence_refs.map(item => item.evidence_id), ['0', '1', '2']);
  assert.equal(other.edge_id, '3');
});

test('a flow trace follows both directions through the selected node and excludes sibling branches', () => {
  const edges = [
    ['route', 'screen'], ['screen', 'button'], ['button', 'handleNext'], ['handleNext', 'condition'],
    ['condition', 'true'], ['condition', 'false'], ['screen', 'handleExport'], ['screen', 'handleSaveDraft'],
    ['true', 'condition'], ['unrelated', 'false'],
  ].map(([from_node_id, to_node_id], index) => ({ edge_id: String(index), from_node_id, to_node_id }));
  assert.deepEqual([...traceFlowEdges(edges, 'handleNext')].sort(), ['0', '1', '2', '3', '4', '5', '8']);
  assert.deepEqual([...traceFlowEdges(edges, 'handleExport')].sort(), ['0', '6']);
  assert.equal(traceFlowEdges(edges, null).size, 0);
});

function fixture(node) {
  if (node.$ref) {
    const name = node.$ref.split('/').at(-1);
    return fixed[name] ?? fixture(defs[name]);
  }
  if (Object.hasOwn(node, 'const')) return node.const;
  if (node.enum) return node.enum[0];
  if (node.oneOf || node.anyOf) return fixture((node.oneOf ?? node.anyOf)[0]);
  switch (node.type) {
    case 'object': return Object.fromEntries(node.required.map(key => [key, fixture(node.properties[key])]));
    case 'array': return Array.from({ length: node.minItems ?? 0 }, () => fixture(node.items));
    case 'integer': return node.minimum ?? 0;
    case 'number': return 0.5;
    case 'string': return 'x';
    case 'boolean': return false;
    case 'null': return null;
    default: throw Error('Missing fixture literal');
  }
}

test('C174 exact encoded bytes and digests for all 14 normative vectors', async () => {
  const section = read('../../Maps-Context-Engine-4D_SPEC.md').split('<!-- BEGIN MCE4D_HASH_VECTORS_V1 -->')[1];
  const vectors = JSON.parse(section.split('```json')[1].split('```')[0]).valid;
  assert.equal(vectors.length, 14);
  for (const vector of vectors) {
    const prefix = utf8.encode('MCE4D\0C14N1\0');
    const encoding = Buffer.concat([prefix, canonicalBytes(vector.domain, limit), canonicalBytes(vector.value, limit)]);
    assert.equal(encoding.toString('hex'), vector.encoding_hex, vector.case);
    assert.equal(await canonicalDigest(vector.domain, vector.value, limit), vector.digest, vector.case);
  }
  for (const value of [1.5, 9007199254740992, '\ud800', Infinity]) {
    assert.throws(() => canonicalBytes(value, limit));
  }
  assert.throws(() => canonicalBytes('日本', 4), /limit/);
  assert.throws(() => canonicalDigest('', null, limit), /domain/);
  const deepest = '['.repeat(32) + '0' + ']'.repeat(32);
  assert.doesNotThrow(() => canonicalBytes(parseJSON(utf8.encode(deepest), limit, true), limit));
  assert.throws(() => canonicalBytes(JSON.parse('[' + deepest + ']'), limit), /nesting/);
});

test('SV01 rejects invalid UTF-8, decoded duplicates, malformed and unbounded JSON', () => {
  const invalid = [
    '{"a":{"a":0,"\\u0061":0}}', '{"a":1} {}', '{"a":1,}', '/* hello */{}',
    '{"x":"\\ud800"}', '{"\\udc00":0}', '1.0', '1e0', '9007199254740993',
    '1e400', '\uFEFF{}', '\u00A0{}', '['.repeat(33) + '0' + ']'.repeat(33),
    JSON.stringify(Object.fromEntries(Array.from({ length: 4097 }, (_, i) => [i, 0]))),
  ];
  for (const raw of invalid) assert.throws(() => parseJSON(utf8.encode(raw), limit, true), raw.slice(0, 100));
  assert.throws(() => parseJSON(Uint8Array.of(0xc0, 0xaf), limit, true));
  assert.throws(() => parseJSON(utf8.encode('null'), 3));
  assert.equal(parseJSON(utf8.encode('1.5'), limit), 1.5);
  assert.equal(parseJSON(utf8.encode('{"__proto__":0}'), limit).__proto__, 0);
});

test('C124 and D015 all 23 bridge variants are closed and direction-bound', () => {
  const variants = defs.BridgeMessage.oneOf;
  assert.equal(variants.length, 23);
  for (const shape of variants) {
    const message = fixture(shape);
    for (const [direction, allowed] of Object.entries(wire.bridge)) {
      if (allowed.includes(message.type)) {
        assert.deepEqual(decodeBridgeMessage(bytes(message), direction, 16000), message);
        for (const extra of [{ generation_id: id }, { source_ref: { path: 'x.ts' } }, { dom_text: 'x' }]) {
          const unknown = { ...message, payload: { ...message.payload, ...extra } };
          assert.throws(() => decodeBridgeMessage(bytes(unknown), direction, 16000));
        }
      } else {
        assert.throws(() => decodeBridgeMessage(bytes(message), direction, 16000), /direction/);
      }
    }
  }
  const pick = fixture(defs.Bridge_PICK_AT);
  assert.throws(() => decodeBridgeMessage(bytes(pick), 'parent_to_sdk', 0.25), /bounds/);
  assert.throws(() => decodeBridgeMessage(JSON.stringify(pick).replace('"seq":1', '"seq":1e0'), 'parent_to_sdk', 16000));
  assert.throws(() => decodeBridgeMessage(' '.repeat(16385), 'parent_to_sdk', 16000));
  assert.throws(() => decodeBridgeMessage(bytes(pick), 'constructor', 16000), /direction/);
  const result = fixture(defs.Bridge_PICK_RESULT);
  result.payload.targets.push(fixture(defs.Bridge_PICK_RESULT.properties.payload.properties.targets.items));
  result.payload.targets[0].instance_ref.document_epoch++;
  assert.throws(() => decodeBridgeMessage(bytes(result), 'sdk_to_parent', 16000), /Stale bridge instance/);
});

test('C176 the 18 Explorer routes bind methods, DTOs, operations and byte caps', () => {
  const registry = JSON.parse(read('../../contracts/operations.v1.json'));
  const names = Object.keys(wire.http);
  assert.equal(names.length, 18);
  for (const route of names) {
    const [label, requestBytes, responseBytes] = wire.http[route];
    assert.deepEqual(httpLimits(route), { requestBytes, responseBytes });
    const [requestShape, dataShape] = registry[route];
    if (route.startsWith('POST')) {
      const request = fixture(defs[requestShape]);
      if (route === 'POST /preview/attach') Object.assign(request, { origin: 'http://localhost:4173', path: '/app' });
      assert.deepEqual(decodeHTTPRequest(route, bytes(request), 1), request);
      assert.throws(() => decodeHTTPRequest(route, bytes({ ...request, extra: true }), 1));
      assert.throws(() => decodeHTTPRequest(route, new Uint8Array(requestBytes + 1), 1), /byte limit/);
    }
    const response = envelope({ ...fixture(defs[dataShape]), operation: label });
    if (route === 'POST /session/bootstrap') response.readiness = 'blocked';
    assert.deepEqual(decodeHTTPResponse(route, bytes(response)), response);
    assert.throws(() => decodeHTTPResponse(route, bytes({ ...response, extra: true })));
    assert.throws(() => decodeHTTPResponse(route, new Uint8Array(responseBytes + 1)), /byte limit/);
  }
  assert.deepEqual(decodeWorkspaceQuery(new URLSearchParams('verify=workspace'), 0), { verify: 'workspace' });
  for (const query of ['scope=default', 'verify=typo', 'verify=none&verify=none']) {
    assert.throws(() => decodeWorkspaceQuery(new URLSearchParams(query), 0));
  }
  assert.throws(() => decodeWorkspaceQuery(new URLSearchParams(), 1));
  assert.throws(() => httpLimits('mce4d_locate'));
  assert.throws(() => httpLimits('POST /preview-permission'));
  assert.throws(() => decodeHTTPRequest('GET /workspace', bytes({}), 1));
});

function envelope(data) {
  return {
    schema_version: 1, request_id: id, status: 'ok', scope: null, readiness: 'needs_context',
    freshness: { status: 'not_checked', scope: 'none', checked_at: null, buffer_scope: 'disk_only',
      consistency: 'captured_bytes_with_observed_stability', changed_count: null, source_view: 'indexed_snapshot' },
    coverage: { status: 'unknown', capabilities: [], gaps: [], scope_description: 'Synthetic fixture' },
    data, pagination: { truncated: false, enumeration_complete: true, returned_count: 0,
      required_pending: 0, optional_pending: 0, next_cursor: null, limit_reason: null },
    budget: { max_output_bytes: 4096, serialized_bytes: 0, token_count: null, token_estimate: null,
      tokenizer_id: null, estimate_only: false, delivery_mode: 'http' },
    warnings: [], error: null,
  };
}

test('C124 endpoint label and response status cannot grant data from another operation', () => {
  const mapping = envelope(fixture(defs.MappingData));
  assert.throws(() => decodeHTTPResponse('POST /mapping/resolve', bytes({
    ...mapping, data: { ...mapping.data, instances: [] },
  })));
  assert.throws(() => decodeHTTPResponse('POST /source/read', bytes(mapping)), /operation mismatch/);
  assert.throws(() => decodeHTTPResponse('POST /mapping/resolve', bytes({ ...mapping, status: 'blocked' })));
  assert.throws(() => decodeHTTPResponse('POST /mapping/resolve', bytes({
    ...mapping, pagination: { ...mapping.pagination, truncated: true, optional_pending: 1 },
  })), /Incomplete mapping/);
  const mode = envelope({ ...fixture(defs.PreviewData), operation: 'preview_mode' });
  assert.throws(() => decodeHTTPResponse('POST /preview/ack', bytes(mode)), /operation mismatch/);
});

test('SV09 distinguishes unknown pending counts from complete results', () => {
  const base = envelope(null).pagination;
  checkPagination(base);
  checkPagination({ ...base, truncated: true, enumeration_complete: false, required_pending: null, next_cursor: 'opaque' });
  assert.throws(() => checkPagination({ ...base, enumeration_complete: false }));
  assert.throws(() => checkPagination({ ...base, enumeration_complete: false, truncated: true }));
  assert.throws(() => checkPagination({ ...base, next_cursor: 'stale' }));
});

test('SV03 and CT04 pin exact scope without discarding tie fields', async () => {
  const member = fixture(defs.ScopeMember);
  const a = { ...fixture(defs.MemberUnavailable), reason_code: 'é' };
  const b = { ...a, reason_code: 'e\u0301' };
  const scope = { ...fixture(defs.ScopeVector), kind: 'federated', group_id: id, group_revision: 1,
    members: [member, { ...member, service_id: 'next' }], unavailable_members: [a, b] };
  scope.vector_digest = await scopeDigest(scope, limit);
  const reversed = { ...scope, members: [...scope.members].reverse(), unavailable_members: [b, a] };
  assert.equal(await scopeDigest(reversed, limit), scope.vector_digest);
  assert.equal(await subgraphDigest(reversed, [], [], limit), await subgraphDigest(scope, [], [], limit));
  await verifyScopeDigest(reversed, limit);
  await assert.rejects(verifyScopeDigest({ ...reversed, group_revision: 2 }, limit), /mismatch/);
  assert.throws(() => scopeDigest({ ...scope, members: [member, { ...member, record_epoch: 2 }] }, limit));
  assert.equal(scopeSelection({ pinned_vector: scope }, 0), 'pinned');
  assert.throws(() => scopeSelection({ pinned_vector: scope, group_id: id }, 1), /Mixed pinned/);
  assert.throws(() => decodeHTTPRequest('POST /graph/query', bytes({ root: { kind: 'workspace' } }), 0), /explicit binding/);
});

test('D014 URL matching and status evidence preserve the approved boundary', async () => {
  assert.equal(pathWithinPrefix('/app', '/app'), true);
  assert.equal(pathWithinPrefix('/app', '/app/main'), true);
  assert.equal(pathWithinPrefix('/app', '/application'), false);
  assert.equal(pathWithinPrefix('/app/', '/app'), false);
  assert.equal(pathWithinPrefix('/', '/application'), true);
  const origin = 'http://localhost:4173';
  assert.equal(matchPreviewURL(origin + '/app/../app/view', origin, '/app').pathname, '/app/view');
  assert.deepEqual(matchPreviewURL(origin + '/app?secret=redacted#token', origin, '/app'), { origin, pathname: '/app' });
  for (const url of ['http://127.0.0.1:4173/app', 'http://u@localhost:4173/app',
    origin + '/application', origin + '/app/%2e%2e/private', 'file:///app']) {
    assert.throws(() => matchPreviewURL(url, origin, '/app'));
  }
  const p = { ...fixture(defs.PreviewPermissionProfile), approved_origin: origin, approved_path_prefix: '/app',
    source_worktree_ids: [id, alt], allowed_dev_destinations: [origin, 'http://localhost:5173'] };
  assert.deepEqual(decodePermissionProfile(bytes(p), limit), p);
  const reverse = { ...p, source_worktree_ids: [alt, id], allowed_dev_destinations: [...p.allowed_dev_destinations].reverse() };
  assert.equal(await permissionDigest(p, limit), await permissionDigest(reverse, limit));
  assert.notEqual(await permissionDigest(p, limit), await permissionDigest({ ...p, version: p.version + 1 }, limit));
  assert.throws(() => decodePermissionProfile(bytes({ ...p, source_worktree_ids: [id, id] }), limit));
  const status = { ...fixture(defs.PreviewStatusData), state: 'bound', binding_id: hash('a'),
    permission_status: 'approved', interaction_allowed: true, server_time: at, expires_at: later };
  assert.deepEqual(decodeHTTPResponse('POST /preview/status', bytes(envelope(status))).data, status);
  for (const changed of [{ state: 'pairing' }, { binding_id: null }, { permission_status: 'revoked' }, { server_time: later }]) {
    assert.throws(() => decodeHTTPResponse('POST /preview/status', bytes(envelope({ ...status, ...changed }))), /bound permission/);
  }
});

test('C123/C152/C153 build identities track byte evidence independently of resolver and generation', async () => {
  const path = 'src/App.tsx';
  const content_hash = hash('d');
  const map = fixture(defs.BuildManifest);
  map.sources = [{ path, content_hash }, { path: 'src/lib.ts', content_hash }];
  const root = { ...fixture(defs.AnchorEntry), path, content_hash, owner_anchor_id: null,
    range: { start_byte: 0, end_byte: 9 } };
  root.anchor_id = await anchorDigest(root, map.provider_digest, limit);
  const child = { ...root, range: { start_byte: 2, end_byte: 5 }, owner_anchor_id: root.anchor_id };
  child.anchor_id = await anchorDigest(child, map.provider_digest, limit);
  map.anchors = [root, child];
  map.build_map_id = await buildMapDigest(map, limit);
  await verifyBuildIdentity(decodeBuildManifest(bytes(map), limit), limit);
  assert.equal(await buildMapDigest({ ...map, anchors: [...map.anchors].reverse(), sources: [...map.sources].reverse() }, limit), map.build_map_id);
  assert.notEqual(await buildMapDigest({ ...map, module_coverage: 'loaded_modules_only' }, limit), map.build_map_id);
  const identity = { build_map_id: map.build_map_id, source_generation_id: id,
    profile_id: map.profile_id, policy_digest: hash('b'), resolver_digest: hash('c') };
  assert.notEqual(await bindingDigest(identity, limit), await bindingDigest({ ...identity, resolver_digest: hash('f') }, limit));
  assert.notEqual(await bindingDigest(identity, limit), await bindingDigest({ ...identity, source_generation_id: alt }, limit));
  assert.throws(() => decodeBuildManifest(bytes({ ...map, anchors: [root, { ...child, content_hash: hash('4') }] }), limit));
  assert.throws(() => decodeBuildManifest(bytes({ ...map, anchors: [{ ...root, owner_anchor_id: child.anchor_id }, child] }), limit), /cycle/);
  await assert.rejects(verifyBuildIdentity({ ...map, anchors: [root] }, limit), /map digest mismatch/);
  for (const name of ['../a', 'a/../../b', 'a//b', 'C:/a', '\\server\\a', '/a', 'a/./b']) {
    assert.throws(() => checkRelativePath(name));
  }
  checkRelativePath('src/日本語.ts');
});

test('Recipes match native Go fixture digests and reject a range splitting captured UTF-8', async () => {
  assert.equal(await bindingDigest({
    build_map_id: hash('c'), source_generation_id: '00000000-0000-4000-8000-000000000001',
    profile_id: hash('a'), policy_digest: hash('b'), resolver_digest: hash('d'),
  }, limit), '3b352d73b1c13cbcd0ca363aa3baa59b65db0a2d725733fd4613375606cc2b49');
  const unavailable = { service_id: null, worktree_id: null, profile_id: null, status: 'access_denied' };
  assert.equal(await scopeDigest({
    kind: 'federated', members: [], vector_digest: hash('a'), group_id: id, group_revision: 1,
    catalog_incarnation: id, unavailable_members: [
      { ...unavailable, reason_code: 'REASON_B' }, { ...unavailable, reason_code: 'REASON_A' },
    ],
  }, limit), '1c95fdb38de90e25c3ba5043441cb289e4cf8517bdbfe4a5589afaf22c583dd0');
  const map = { ...fixture(defs.BuildManifest), provider_version: 'v1', module_coverage: 'partial' };
  const entry = { anchor_id: hash('b'), kind: 'component_definition', path: 'a.tsx', content_hash: hash('a'),
    range: { start_byte: 0, end_byte: 2 }, owner_anchor_id: null };
  map.sources = ['z.tsx', 'a.tsx'].map(path => ({ path, content_hash: hash('a') }));
  map.anchors = [{ ...entry, anchor_id: hash('c'), path: 'z.tsx', kind: 'element_site', owner_anchor_id: hash('b') }, entry];
  assert.equal(await buildMapDigest(map, limit), 'c08d5673dcca45d6b7d37776aca725924ac4e09f85c03a9afb6b670bd17aeb4e');
  const source = utf8.encode('éx');
  const content_hash = await sha256(source);
  const captured = new Map([['a.tsx', source]]);
  map.sources = [{ path: 'a.tsx', content_hash }];
  map.anchors = [{ ...entry, content_hash }];
  map.anchors[0].anchor_id = await anchorDigest(map.anchors[0], map.provider_digest, limit);
  map.build_map_id = await buildMapDigest(map, limit);
  await verifyBuildCaptured(map, captured, limit, 3);
  await assert.rejects(verifyBuildCaptured(map, captured, limit, 2), /byte limit/);
  await assert.rejects(verifyBuildCaptured(map, new Map([['a.tsx', utf8.encode('xyz')]]), limit, 3), /hash mismatch/);
  map.anchors[0].range.end_byte = 1;
  map.anchors[0].anchor_id = await anchorDigest(map.anchors[0], map.provider_digest, limit);
  map.build_map_id = await buildMapDigest(map, limit);
  await assert.rejects(verifyBuildCaptured(map, captured, limit, 3), /UTF-8/);
});
