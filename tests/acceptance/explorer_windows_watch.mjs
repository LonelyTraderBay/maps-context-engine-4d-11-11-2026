import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { createInterface } from 'node:readline';
import { chromium } from 'playwright';

const input = createInterface({ input: process.stdin, crlfDelay: Infinity });
const lines = input[Symbol.asyncIterator]();
const { url, code, worktree, profile } = JSON.parse((await lines.next()).value);
const base = new URL(url);
assert.equal(base.origin, 'http://127.0.0.1:' + base.port);
assert.equal(base.pathname, '/');
const api = '/api/explorer/v1';
const goCoverage = process.argv[3];
let browser;

try {
  browser = await chromium.launch({ executablePath: process.argv[2], headless: true, timeout: 15000 });
  const context = await browser.newContext({ viewport: { width: 1440, height: 880 },
    serviceWorkers: 'block', acceptDownloads: false });
  const unexpected = [];
  await context.route('**/*', route => {
    if (new URL(route.request().url()).origin === base.origin) return route.continue();
    unexpected.push(new URL(route.request().url()).origin);
    return route.abort();
  });
  const page = await context.newPage();
  page.setDefaultTimeout(18000);
  const errors = [];
  const graphReplies = [];
  const sourceReplies = [];
  const onRoute = (reply, route) => new URL(reply.url()).pathname === api + route;
  page.on('pageerror', error => errors.push(error.message));
  page.on('response', reply => {
    if (onRoute(reply, '/graph/query')) graphReplies.push(reply);
    if (onRoute(reply, '/source/read')) sourceReplies.push(reply);
  });
  const served = await page.goto(base.href);
  assert.equal(served.status(), 200);
  assert.match(served.headers()['content-security-policy'], /connect-src 'self'/);
  const bootstrap = page.waitForResponse(reply => new URL(reply.url()).pathname === api + '/session/bootstrap');
  await page.getByLabel('Mã bootstrap').fill(code);
  await page.getByRole('button', { name: 'Kết nối', exact: true }).click();
  const session = await bootstrap;
  assert.equal(session.status(), 200);
  const bearer = (await session.json()).data.bearer;
  assert.ok(bearer, 'accepted Explorer session');
  const graph = page.locator('#graph-pane');
  const source = page.locator('#source-pane');
  const codeBlock = source.locator('.source-code');
  const select = name => graph.getByRole('button', { name: new RegExp('^Chọn ' + name + ',') });
  const ready = () => page.waitForFunction(() =>
    [...document.querySelectorAll('button')].some(button => button.textContent === 'Tải lại' && !button.disabled));
  let selected;

  for (const expectedPhase of ['initial', 'add', 'edit', 'delete']) {
    const { phase, generation, present, absent } = JSON.parse((await lines.next()).value);
    assert.equal(phase, expectedPhase);
    // The app must adopt the generation with no click or page reload.
    await page.locator('.binding dd[title="' + generation + '"]').waitFor();
    await ready();
    const replies = graphReplies.filter(reply =>
      reply.request().postDataJSON().scope.pinned_vector.members[0].generation_id === generation);
    assert.ok(replies.length > 0, 'AppShell queried the published graph on its own');
    const byID = new Map();
    const edges = [];
    let last;
    for (const reply of replies) {
      assert.equal(reply.status(), 200, 'real Explorer graph response');
      last = await reply.json();
      const pin = reply.request().postDataJSON().scope.pinned_vector;
      assert.equal(last.scope.vector_digest, pin.vector_digest);
      assert.equal(last.scope.members.length, 1);
      const member = last.scope.members[0];
      assert.equal(member.worktree_id, worktree);
      assert.equal(member.profile_id, profile);
      assert.equal(member.generation_id, generation);
      // Edge pages carry endpoint nodes even when an earlier page listed them.
      for (const node of last.data.nodes) {
        if (byID.has(node.node_id)) assert.deepEqual(byID.get(node.node_id), node);
        byID.set(node.node_id, node);
      }
      edges.push(...last.data.edges);
    }
    assert.equal(last.pagination.next_cursor, null, 'the entire small fixture graph was read');
    assert.equal(last.pagination.enumeration_complete, true);
    const nodes = [...byID.values()];
    if (goCoverage === 'fallback') {
      assert.equal(last.coverage.status, 'partial');
      assert.ok(last.coverage.gaps.some(gap => gap.code === 'TYPED_ANALYSIS_UNAVAILABLE'));
      assert.ok(edges.every(edge => edge.evidence_refs.every(evidence => evidence.kind !== 'type_resolved')));
    }
    if (goCoverage === 'typed') {
      assert.ok(last.coverage.capabilities.some(cap => cap.name === 'go_semantics' && cap.provider_id === 'embedded-go-captured'));
      const from = phase === 'add' || phase === 'edit' ? 'ExplorerWatchMarker' : 'StableCaller';
      const site = from === 'StableCaller' ? 'marker.go' : 'watch_explorer.go';
      assert.ok(edges.some(edge => edge.relation_kind === 'CALLS_DIRECT' &&
        edge.resolution_status === 'exact_in_supported_scope' &&
        byID.get(edge.from_node_id)?.label === from && byID.get(edge.to_node_id)?.label === 'PublishMarker' &&
        edge.evidence_refs.some(evidence => evidence.kind === 'type_resolved' &&
          evidence.source_refs.some(ref => ref.generation_id === generation && ref.path === site))),
      'expected a type-resolved call in the graph for the new generation');
    }
    for (const file of absent) {
      assert.ok(!nodes.some(node => node.label === file.name), 'removed symbol absent from active graph');
      if (phase === 'delete') {
        assert.ok(!nodes.some(node => node.source_refs.some(ref => ref.path === file.path)), 'deleted path absent from active graph');
      }
    }
    function expectedSource(file) {
      const matches = nodes.filter(node => node.label === file.name && node.source_refs.some(ref => ref.path === file.path));
      assert.equal(matches.length, 1, 'one graph node for ' + file.name + ' in ' + file.path);
      const [node] = matches;
      const refs = node.source_refs.filter(ref => ref.path === file.path);
      assert.equal(refs.length, 1, 'one source range in the fixture file');
      const [ref] = refs;
      const bytes = Buffer.from(file.body, 'utf8');
      assert.equal(ref.path, file.path);
      assert.equal(ref.generation_id, generation);
      assert.equal(ref.worktree_id, worktree);
      assert.equal(ref.profile_id, profile);
      const hash = createHash('sha256').update(bytes).digest('hex');
      assert.equal(ref.content_hash, hash);
      const { start_byte: start, end_byte: end } = ref.range;
      assert.ok(0 <= start && start < end && end <= bytes.length);
      const sliced = bytes.subarray(start, end).toString('utf8');
      assert.ok(sliced.includes(file.name));
      return { node, ref, bytes, hash, start, end, sliced };
    }

    const matchesSource = (reply, ref) => {
      if (!onRoute(reply, '/source/read')) return false;
      const sent = reply.request().postDataJSON().source_ref;
      return sent.path === ref.path && sent.generation_id === ref.generation_id &&
        sent.range.start_byte === ref.range.start_byte && sent.range.end_byte === ref.range.end_byte;
    };
    async function checkSource(response, { ref, hash, start, end, sliced }) {
      assert.equal(response.status(), 200);
      const result = (await response.json()).data;
      assert.deepEqual(result.source_ref, ref);
      assert.equal(result.content, sliced);
      await page.waitForFunction(text => document.querySelector('.source-code')?.textContent === text, sliced);
      assert.equal(await source.locator('.file-path').textContent(), ref.path);
      assert.equal(await source.locator('.digest').getAttribute('title'), hash);
      await source.getByText(`Byte [${start}, ${end})`, { exact: true }).waitFor();
    }

    const retained = selected && present.find(file => file.path === selected.path && file.name === selected.name);
    if (retained) {
      const expected = expectedSource(retained);
      const reply = sourceReplies.find(reply => matchesSource(reply, expected.ref)) ??
        await page.waitForResponse(reply => matchesSource(reply, expected.ref));
      await checkSource(reply, expected);
      selected = retained;
    } else {
      await source.getByText('Chưa chọn ký hiệu', { exact: true }).waitFor();
      assert.equal(await codeBlock.count(), 0, 'previous snapshot source clears when the node disappears');
      selected = undefined;
    }

    for (const file of present) {
      const expected = expectedSource(file);
      const { node, ref, bytes } = expected;
      if (selected !== file) {
        const pending = page.waitForResponse(reply => matchesSource(reply, ref));
        await graph.getByTestId('rf__node-' + node.node_id)
          .getByRole('button', { name: 'Chọn ' + file.name + ', ' + node.kind, exact: true }).click();
        await checkSource(await pending, expected);
        selected = file;
      }
      const fullRef = { ...ref, range: { start_byte: 0, end_byte: bytes.length } };
      const captured = await context.request.post(base.origin + api + '/source/read', {
        headers: { Authorization: 'Bearer ' + bearer, Origin: base.origin, 'X-MCE4D-Intent': 'explorer' },
        data: { source_ref: fullRef },
      });
      assert.equal(captured.status(), 200);
      const full = (await captured.json()).data;
      assert.deepEqual(full.source_ref, fullRef);
      assert.deepEqual(Buffer.from(full.content, 'utf8'), bytes, 'preserve full UTF-8 bytes and line endings');
    }
    for (const file of absent) {
      assert.equal(await select(file.name).count(), 0, 'removed symbol absent from displayed graph');
    }
    if (phase === 'initial') {
      const graphCount = graphReplies.length;
      const sourceCount = sourceReplies.length;
      const idle = await page.waitForResponse(reply => onRoute(reply, '/workspace') && reply.ok());
      assert.equal((await idle.json()).scope.members[0].generation_id, generation);
      assert.equal(graphReplies.length, graphCount, 'stable generation does not rebuild graph');
      assert.equal(sourceReplies.length, sourceCount, 'stable generation does not reread source');
    }
    assert.deepEqual(errors, []);
    assert.deepEqual(unexpected, []);
    console.log(phase + ' ' + generation + (retained ? ' retained-source' : ' cleared-source'));
  }
} catch (error) {
  // The bootstrap secret stays in the test pipe even on a failed browser action.
  console.error(String(error.stack ?? error).replaceAll(code, '[REDACTED]'));
  process.exitCode = 1;
} finally {
  input.close();
  process.stdin.pause();
  await browser?.close();
}
