import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { createInterface } from 'node:readline';
import { chromium } from 'playwright';
import { exerciseManualMappings } from './manual_mappings.browser.mjs';
import { exerciseSelectionContext } from './selection_context.browser.mjs';

const stream = createInterface({ input: process.stdin });
const lines = stream[Symbol.asyncIterator]();
const data = JSON.parse((await lines.next()).value);
const base = new URL(data.explorer);
const peer = new URL(data.peer);
assert.equal(base.hostname, '127.0.0.1');
assert.equal(peer.hostname, '127.0.0.1');
assert.notEqual(base.origin, peer.origin);
const browserPath = process.env.MCE4D_T086_CHROMIUM;
assert.equal(createHash('sha256').update(await readFile(browserPath)).digest('hex'),
  '24eb0a14de64ab0c9c2e283a5512ca817db64af8fd853cdb4d818f19a8e23b9b');
const browser = await chromium.launch({ executablePath: browserPath, headless: true, chromiumSandbox: false, timeout: 15_000 });
try {
  const context = await browser.newContext({ viewport: { width: 1080, height: 720 }, serviceWorkers: 'block',
    acceptDownloads: false, hasTouch: !!data.interact });
  const refused = [];
  await context.route('**/*', route => {
    const requested = new URL(route.request().url()).origin;
    if (requested === base.origin || requested === peer.origin) return route.continue();
    refused.push(requested);
    return route.abort();
  });
  const page = await context.newPage();
  page.setDefaultTimeout(16_000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const doc = await page.goto(base.href);
  assert.ok(doc.headers()['content-security-policy'].includes('frame-src ' + peer.origin));
  await page.getByLabel('Mã bootstrap').fill(data.code);
  await page.getByRole('button', { name: 'Kết nối', exact: true }).click();
  if (data.records) {
    await exerciseManualMappings(page, lines, base);
  } else {
    await exercisePreview(page, lines, data, peer, refused, errors);
  }
} finally {
  await browser.close();
  stream.close();
}

async function exercisePreview(page, lines, data, peer, refused, errors) {
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  const dial = page.getByRole('region', { name: 'Preview' }).or(page.getByRole('dialog', { name: 'Preview' }));
  await dial.getByLabel('Địa chỉ').fill(peer.href);
  await dial.getByLabel('Mã build map').fill(data.build);
  await dial.getByLabel('Hồ sơ quyền').fill(data.permission);
  const statusResponse = page.waitForResponse(response => response.url().endsWith('/preview/status') && response.ok());
  await dial.getByRole('button', { name: 'Ghép preview' }).click();
  await dial.getByText('Preview sẵn sàng ở chế độ Inspect.').waitFor();
  const frame = dial.locator('iframe');
  assert.equal(await frame.evaluate(element => element.inert && element.tabIndex === -1 &&
    getComputedStyle(element).pointerEvents === 'none'), true);
  let target = page.frame({ url: peer.href });
  assert.ok(target);
  await target.waitForFunction(() => Number(document.documentElement.dataset.heartbeat) > 0);
  assert.ok(Number(await target.locator('html').getAttribute('data-background-ticks')) > 0);
  assert.equal(await target.locator('html').getAttribute('data-bound'), 'true');
  assert.equal(await target.locator('html').getAttribute('data-unexpected'), null);
  const status = await (await statusResponse).json();
  assert.equal(status.data.state, 'bound');
  assert.equal(status.data.revision, 2);
  assert.equal(status.data.permission_version, 1);
  assert.equal(status.data.interaction_allowed, false);
  assert.equal(Object.hasOwn(status.data, 'pairing_nonce'), false);
  if (data.interact) {
    const shield = dial.getByRole('button', { name: 'Vùng chọn Inspect' });
    const counter = () => target.locator('html').getAttribute('data-actions');
    const action = target.locator('#fixture-action');
    const box = await action.boundingBox();
    assert.ok(box);
    const x = box.x + box.width / 2;
    const y = box.y + box.height / 2;
    await page.mouse.click(x, y);
    await page.mouse.dblclick(x, y);
    await page.mouse.click(x, y, { button: 'right' });
    await page.touchscreen.tap(x, y);
    await shield.focus();
    await page.keyboard.press('Enter');
    await page.keyboard.press('Space');
    assert.equal(await counter(), '0');
    assert.equal(await target.locator('html').getAttribute('data-submits'), '0');
    assert.equal(await target.locator('html').getAttribute('data-action-fetches'), '0');
    assert.equal(await frame.evaluate(element => element.inert && element.tabIndex === -1), true);
    let ackUnderShield = false;
    await page.route('**/api/explorer/v1/preview/ack', async route => {
      ackUnderShield = await frame.evaluate(element => element.inert && element.tabIndex === -1);
      await route.continue();
    });
    const activate = () => dial.getByRole('button', { name: 'Bật Interact' }).click();
    await activate();
    await dial.getByText('Interact đang bật trong môi trường thử nghiệm.').waitFor();
    assert.equal(ackUnderShield, true);
    assert.equal(await frame.evaluate(element => element.inert), false);
    await action.click();
    assert.equal(await counter(), '1');
    assert.equal(await target.locator('html').getAttribute('data-action-fetches'), '1');
    let modeDisarmedFirst = false;
    await page.route('**/api/explorer/v1/preview/mode', async route => {
      const body = route.request().postDataJSON();
      if (body.mode === 'inspect') modeDisarmedFirst = await frame.evaluate(element => element.inert);
      await route.continue();
    });
    await dial.getByRole('button', { name: 'Về Inspect' }).click();
    await dial.getByText('Preview sẵn sàng ở chế độ Inspect.').waitFor();
    assert.equal(modeDisarmedFirst, true);
    await page.mouse.click(x, y);
    assert.equal(await counter(), '1');
    const stale = dial.getByText('Preview chưa sẵn sàng. Kiểm tra build và quyền rồi ghép lại.');
    const attach = async () => {
      await dial.getByRole('button', { name: 'Ghép preview' }).click();
      await dial.getByText('Preview sẵn sàng ở chế độ Inspect.').waitFor();
      target = page.frame({ url: peer.href });
      assert.ok(target);
      assert.equal(await frame.evaluate(element => element.inert), true);
    };
    const ackHeld = Promise.withResolvers();
    const ackRelease = Promise.withResolvers();
    await page.route('**/api/explorer/v1/preview/ack', async route => {
      const response = await route.fetch();
      assert.equal(response.status(), 200);
      ackHeld.resolve();
      await ackRelease.promise;
      await route.fulfill({ response }).catch(() => {});
    }, { times: 1 });
    await activate();
    await ackHeld.promise;
    try {
      assert.equal(await frame.evaluate(element => element.inert), true);
      await dial.getByRole('button', { name: 'Về Inspect' }).click();
      await stale.waitFor();
    } finally {
      ackRelease.resolve();
    }
    assert.equal(await frame.getAttribute('src'), null);
    await attach();
    await activate();
    await dial.getByText('Interact đang bật trong môi trường thử nghiệm.').waitFor();
    await target.waitForFunction(() => Number(document.documentElement.dataset.heartbeat) >= 1);
    const serverAlive = page.waitForResponse(response => response.url().endsWith('/preview/status') && response.ok());
    await target.evaluate(() => window.stopFixtureSDK());
    await serverAlive;
    await stale.waitFor();
    assert.equal(await frame.evaluate(element => element.inert), true);
    assert.equal(await frame.getAttribute('src'), null);
    await attach();
    await activate();
    await dial.getByText('Interact đang bật trong môi trường thử nghiệm.').waitFor();
    console.log(JSON.stringify({ case: 'T088_BROWSER_INTERACT', status: 'PASS', inspectActions: 0, inspectFetches: 0,
      backgroundContinuedInInspect: true,
      interactActions: 1, ackUnderShield, modeDisarmedFirst, staleAckUnderShield: true, detachedDespiteLiveServer: true }));
  } else if (data.benchmark) {
    const { workspaceRow, click } = await preparePick(page, target);
    await measurePreview(page, target, workspaceRow, click, data);
  } else if (data.pick) {
    const { workspaceRow, click } = await preparePick(page, target);
    let paused;
    const pausedResolve = new Promise(resolve => { paused = resolve; });
    let release;
    const resumeResolve = new Promise(resolve => { release = resolve; });
    const endpoint = '**/api/explorer/v1/mapping/resolve';
    await page.route(endpoint, async route => {
      const response = await route.fetch();
      paused();
      await resumeResolve;
      await route.fulfill({ response });
    }, { times: 1 });
    await click();
    await pausedResolve;
    try {
      await workspaceRow.click();
    } finally {
      release();
    }
    await dial.getByText('Đang đối chiếu với index…').waitFor({ state: 'hidden' });
    assert.equal(await workspaceRow.getAttribute('aria-pressed'), 'true');
    await page.unroute(endpoint);
    const resolveCall = page.waitForResponse(response => response.url().endsWith('/mapping/resolve') && response.ok());
    const graphCall = page.waitForResponse(response => response.url().endsWith('/graph/query') && response.ok() &&
      response.request().postDataJSON().root.kind === 'ref');
    await click();
    const resolved = await (await resolveCall).json();
    assert.equal(resolved.data.mapping_status, 'exact_in_supported_scope');
    assert.equal(resolved.data.matches[0].anchor_ref.anchor_id, data.anchor);
    assert.equal(resolved.data.matches[0].target_ref.path, 'entry.tsx');
    assert.equal(resolved.data.matches[0].source_refs[0].path, 'entry.tsx');
    assert.equal(Object.hasOwn(resolved.data, 'instance_refs'), false);
    await page.locator('.symbol-row[aria-pressed="true"]').filter({ hasText: 'entry.tsx' }).waitFor();
    await page.getByText('Bằng chứng build · Anchor khớp · 100 instance đã thấy.').waitFor();
    await page.getByLabel('Mã nguồn của snapshot').getByText('<button', { exact: true }).waitFor();
    assert.equal(await target.locator('html').getAttribute('data-actions'), '0');
    assert.equal(await target.locator('html').getAttribute('data-submits'), '0');
    assert.equal(await target.locator('html').getAttribute('data-action-fetches'), '0');
    await page.getByRole('button', { name: 'Lựa chọn trước' }).click();
    assert.equal(await workspaceRow.getAttribute('aria-pressed'), 'true');
    await page.getByRole('button', { name: 'Lựa chọn sau' }).click();
    await page.locator('.symbol-row[aria-pressed="true"]').filter({ hasText: 'entry.tsx' }).waitFor();
    if (data.context_case) {
      await exerciseSelectionContext(page, lines, base, resolved);
    } else if (data.reverse) {
      const reversed = page.waitForResponse(response => response.url().endsWith('/mapping/resolve'));
      await dial.getByRole('button', { name: 'Hiện nút trong preview' }).click();
      const first = await reversed;
      const firstPage = await first.json();
      assert.equal(first.status(), 200, firstPage.error?.code);
      assert.equal(firstPage.data.mapping_status, 'candidate');
      assert.equal(firstPage.pagination.enumeration_complete, false);
      assert.ok(firstPage.pagination.next_cursor);
      const request = first.request();
      const body = request.postDataJSON();
      const authorization = (await request.allHeaders()).authorization;
      const api = (input) => page.request.post(new URL('/api/explorer/v1/mapping/resolve', base).href, {
        headers: { Authorization: authorization, Origin: base.origin, 'X-MCE4D-Intent': 'explorer' }, data: input,
      });
      const wrongRef = { ...body.ref, content_hash: '0'.repeat(64) };
      const changed = await api({ ...body, ref: wrongRef, cursor: firstPage.pagination.next_cursor });
      assert.equal((await changed.json()).error.code, 'CURSOR_INVALID');
      const small = await api({ ...body, cursor: firstPage.pagination.next_cursor, limit: 1, max_output_bytes: 4096 });
      const oversized = await small.json();
      assert.equal(small.status(), 413);
      assert.equal(oversized.error.code, 'RESULT_TOO_LARGE');
      assert.equal(oversized.pagination.next_cursor, null);
      assert.equal(oversized.error.details[0].source_refs[0].path, 'entry.tsx');
      const unique = new Set(firstPage.data.matches.map(match => match.match_id));
      let highest = firstPage.data.matches.at(-1).anchor_ref.anchor_id;
      let cursor = firstPage.pagination.next_cursor;
      let pages = 1;
      let pending = firstPage.pagination.required_pending;
      while (cursor !== null) {
        assert.ok(pages < data.match_count, 'Each continuation must retire at least one pair');
        const response = await api({ ...body, cursor, limit: pages === 1 ? 1 : 41, max_output_bytes: pages === 1 ? 8192 : 24576 });
        const pageData = await response.json();
        assert.equal(response.status(), 200, pageData.error?.code);
        assert.equal(pageData.data.mapping_status, 'candidate');
        assert.ok(pageData.budget.serialized_bytes <= pageData.budget.max_output_bytes);
        assert.ok(pageData.pagination.returned_count > 0);
        for (const match of pageData.data.matches) {
          assert.deepEqual(match.target_ref, body.ref);
          assert.ok(match.anchor_ref.anchor_id > highest);
          assert.ok(!unique.has(match.match_id));
          unique.add(match.match_id);
          highest = match.anchor_ref.anchor_id;
        }
        assert.equal(pending - pageData.data.matches.length, pageData.pagination.required_pending);
        pending = pageData.pagination.required_pending;
        assert.notEqual(pageData.pagination.next_cursor, cursor);
        cursor = pageData.pagination.next_cursor;
        pages++;
      }
      assert.equal(pending, 0);
      assert.equal(unique.size, data.match_count);
      let visible = firstPage;
      for (let i = 0; !visible.data.matches.some(match => match.anchor_ref.anchor_id === data.anchor); i++) {
        assert.ok(i < data.match_count);
        const next = page.waitForResponse(response => response.url().endsWith('/mapping/resolve') && response.ok());
        await dial.getByRole('button', { name: 'Trang nguồn tiếp theo' }).click();
        visible = await (await next).json();
      }
      const sites = dial.getByRole('list', { name: 'Vị trí nguồn của nút' });
      const activeMatch = visible.data.matches.find(match => match.anchor_ref.anchor_id === data.anchor);
      const absent = visible.data.matches.find(match => match.anchor_ref.anchor_id !== data.anchor);
      const siteButton = match => sites.getByRole('button', {
        name: 'entry.tsx · byte ' + match.source_refs[0].range.start_byte, exact: true,
      });
      // Every published site in this file is indexed. Only the fixture button's site has mounted instances.
      if (absent && absent.source_refs[0].range.start_byte !== activeMatch.source_refs[0].range.start_byte) {
        await siteButton(absent).first().click();
        await dial.getByText('Vị trí này chưa được render trong preview hiện tại.').waitFor();
      }
      await siteButton(activeMatch).first().click();
      await siteButton(activeMatch).locator('..').getByRole('button', { name: 'Đọc vùng này' }).first().click();
      await page.getByLabel('Mã nguồn của snapshot').getByText('<button', { exact: true }).waitFor();
      const instances = dial.getByRole('list', { name: 'Instance trong preview' });
      await instances.getByRole('button', { name: 'Instance 1 · Đang hiển thị', exact: true }).click();
      await dial.locator('.preview-marker').first().waitFor();
      const scroll = instances.getByRole('button', { name: 'Cuộn đến' });
      for (let i = 0; await scroll.count() === 0; i++) {
        assert.ok(i < 4);
        const next = dial.getByRole('button', { name: 'Trang instance tiếp theo' });
        await next.click();
        await dial.getByText('Đang đối chiếu với index…').waitFor({ state: 'hidden' });
      }
      assert.equal(await target.locator('html').getAttribute('data-scrolls'), '0');
      const position = await target.locator('html').evaluate(() => window.scrollY);
      await scroll.first().click();
      const message = data.scroll ? 'Đã cuộn đến instance. Tải lại danh sách để xem vị trí mới.' :
        'Phiên hiện tại chưa cấp quyền cuộn preview.';
      await dial.getByText(message).waitFor();
      assert.equal(await target.locator('html').getAttribute('data-scrolls'), data.scroll ? '1' : '0');
      const afterScroll = await target.locator('html').evaluate(() => window.scrollY);
      assert.equal(afterScroll > position, data.scroll);
      assert.equal(await target.locator('html').getAttribute('data-actions'), '0');
      assert.equal(await target.locator('html').getAttribute('data-submits'), '0');
      console.log(JSON.stringify({ case: 'T090_BROWSER_REVERSE', status: 'PASS', pairs: unique.size, pages,
        changed_filter_rejected: true, oversized_descriptor: true, exact_source_highlight: true,
        scroll: data.scroll ? 'allowed' : 'denied', target_actions: 0 }));
    } else if (data.hmr) {
      await target.locator('#fixture-action36').evaluate(element => element.remove());
      await dial.getByText('Instance đã rời khỏi preview.').waitFor();
      let shielded = false;
      await page.route('**/api/explorer/v1/preview/invalidate', async route => {
        if (route.request().postDataJSON().reason === 'hmr') {
          shielded = await frame.evaluate(element => element.inert && element.tabIndex === -1);
        }
        await route.continue();
      });
      const invalidation = page.waitForResponse(response => response.url().endsWith('/preview/invalidate') && response.ok());
      assert.equal(await target.locator('html').evaluate((_element, id) => window.changeFixtureBuild(id), data.hmr), true);
      const awaiting = dial.getByText('Đang chờ map của build mới và binding đúng snapshot. Ghép lại sau khi import.');
      await awaiting.waitFor();
      await invalidation;
      assert.equal(shielded, true);
      assert.equal(await frame.getAttribute('src'), null);
      assert.equal(await dial.getByLabel('Mã build map').inputValue(), data.hmr);
      const rejected = page.waitForResponse(response => response.url().endsWith('/preview/attach') && response.status() === 409);
      const reattach = () => dial.getByRole('button', { name: 'Ghép preview' }).click();
      await reattach();
      assert.equal((await (await rejected).json()).error.code, 'BUILD_MAP_MISSING');
      await awaiting.waitFor();
      assert.equal(await frame.getAttribute('src'), null);
      console.log(JSON.stringify({ case: 'T091_AWAITING_MAP', status: 'PASS', unmounted: true,
        shielded, missing_map_refuses_frame: true }));
      assert.equal((await lines.next()).value, 'map-ready');
      await reattach();
      const ready = dial.getByText('Preview sẵn sàng ở chế độ Inspect.');
      await ready.waitFor();
      const replacement = page.frame({ url: peer.href });
      assert.ok(replacement);
      assert.equal(await replacement.locator('html').getAttribute('data-build-map'), data.hmr);
      assert.equal(await frame.evaluate(element => element.inert && element.tabIndex === -1), true);
      const navigated = page.waitForResponse(response => response.url().endsWith('/preview/invalidate') &&
        response.request().postDataJSON().reason === 'navigation' && response.ok());
      await replacement.locator('html').evaluate(() => { window.location.assign('/app/next'); });
      await dial.getByText('Preview chưa sẵn sàng. Kiểm tra build và quyền rồi ghép lại.').waitFor();
      await navigated;
      assert.equal(await frame.getAttribute('src'), null);
      await reattach();
      await ready.waitFor();
      const finalFrame = page.frame({ url: peer.href });
      assert.ok(finalFrame);
      assert.equal(await finalFrame.locator('html').getAttribute('data-actions'), '0');
      console.log(JSON.stringify({ case: 'T091_BROWSER_HMR', status: 'PASS', matching_binding_required: true,
        navigation_detached: true, explicit_pairing: true, target_actions: 0 }));
    } else {
      const graphRequest = (await graphCall).request();
      const graphInput = graphRequest.postDataJSON();
      const invalid = await page.request.post(graphRequest.url(), {
        headers: { Authorization: (await graphRequest.allHeaders()).authorization, Origin: base.origin, 'X-MCE4D-Intent': 'explorer' },
        data: { ...graphInput, root: { ...graphInput.root, ref: { ...graphInput.root.ref, content_hash: '0'.repeat(64) } } },
      });
      const denied = await invalid.json();
      assert.equal(denied.error.code, 'SOURCE_PRECONDITION_FAILED');
      assert.equal(denied.data, null);
      console.log(JSON.stringify({ case: 'T089_BROWSER_PICK', status: 'PASS', target_instance: 37, instances: 100,
        graph_and_source_match: true, subsequent_graph_selection_preserved: true, target_actions: 0,
        wrong_file_hash_rejected: true }));
    }
  } else {
    console.log(JSON.stringify({ case: 'T087_BROWSER_BOUND', status: 'PASS', heartbeat: true }));
  }
  assert.equal((await lines.next()).value, 'revoked');
  await dial.getByText('Preview chưa sẵn sàng. Kiểm tra build và quyền rồi ghép lại.').waitFor();
  assert.equal(await frame.getAttribute('src'), null);
  assert.deepEqual(refused, []);
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({ case: data.pick ? 'T089_BROWSER_REVOKED' : data.interact ? 'T088_BROWSER_REVOKED' : 'T087_BROWSER_REVOKED',
    status: 'PASS', removed_frame: true }));
}

async function preparePick(page, target) {
  await page.getByRole('button', { name: 'Danh sách', exact: true }).click();
  const box = await target.locator('#fixture-action36').boundingBox();
  assert.ok(box);
  const workspaceTitle = await page.locator('.symbol-row__title').first().textContent();
  assert.ok(workspaceTitle);
  const workspaceRow = page.locator('.symbol-row').filter({ has: page.getByText(workspaceTitle, { exact: true }) }).first();
  return { workspaceRow, click: () => page.mouse.click(box.x + box.width / 2, box.y + box.height / 2) };
}

async function measurePreview(page, target, workspaceRow, click, data) {
  const expected = data.benchmark;
  const samples = [];
  const timed = request => {
    const timing = request.timing();
    assert.ok(timing.responseEnd >= timing.requestStart && timing.requestStart >= 0, JSON.stringify(timing));
    return timing.responseEnd - timing.requestStart;
  };
  for (let i = 0; i < 21; i++) {
    await workspaceRow.click();
    await page.locator('.preview-pick-outline').waitFor({ state: 'hidden' });
    await page.evaluate(text => {
      const sample = { start: null, outline: null, selected: null };
      window.previewSample = sample;
      const shield = document.querySelector('.preview-dialog__shield');
      shield.addEventListener('click', () => { sample.start = performance.now(); }, { capture: true, once: true });
      const monitor = new MutationObserver(() => {
        if (sample.start === null) return;
        if (sample.outline === null && document.querySelector('.preview-pick-outline')) {
          sample.outline = performance.now() - sample.start;
        }
        if (document.querySelector('.symbol-row[aria-pressed="true"]')?.textContent.includes('entry.tsx') &&
            document.querySelector('.source-code')?.textContent === text) {
          sample.selected = performance.now() - sample.start;
          monitor.disconnect();
        }
      });
      monitor.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true });
    }, expected.text);
    const mapping = page.waitForResponse(response => response.url().endsWith('/mapping/resolve') && response.ok());
    const graph = page.waitForResponse(response => response.url().endsWith('/graph/query') && response.ok() &&
      response.request().postDataJSON().root.kind === 'ref');
    const source = page.waitForResponse(response => response.url().endsWith('/source/read') && response.ok() &&
      response.request().postDataJSON().source_ref.path === expected.path);
    await click();
    const mappingReply = await mapping;
    const match = (await mappingReply.json()).data.matches[0];
    assert.equal(match.anchor_ref.anchor_id, data.anchor);
    const graphReply = await graph;
    await graphReply.finished();
    const sourceReply = await source;
    const response = (await sourceReply.json()).data;
    for (const ref of [match.source_refs[0], response.source_ref]) {
      assert.equal(ref.path, expected.path);
      assert.equal(ref.content_hash, expected.content_hash);
      assert.deepEqual(ref.range, expected.range);
    }
    assert.equal(response.content, expected.text);
    await page.waitForFunction(() => window.previewSample.selected !== null);
    const sample = await page.evaluate(() => window.previewSample);
    assert.ok(sample.outline !== null && sample.outline > 0 && sample.selected >= sample.outline, JSON.stringify(sample));
    samples.push({ outline_ms: sample.outline, selected_source_ms: sample.selected,
      mapping_ms: timed(mappingReply.request()), graph_ms: timed(graphReply.request()), source_ms: timed(sourceReply.request()) });
  }
  const counters = await target.locator('html').evaluate(html => [html.dataset.actions, html.dataset.actionFetches, html.dataset.submits]);
  assert.deepEqual(counters, ['0', '0', '0']);
  const summarize = metric => {
    const times = samples.slice(1).map(sample => sample[metric]);
    return { p95_ms: times.toSorted((a, b) => a - b)[Math.ceil(times.length * 0.95) - 1], samples_ms: times };
  };
  const outline = summarize('outline_ms');
  const selection = summarize('selected_source_ms');
  const status = outline.p95_ms <= 100 && selection.p95_ms <= 500 ? 'PASS' : 'FAIL';
  console.log(JSON.stringify({ case: 'T094_PREVIEW_LATENCY', status, first_after_ready: samples[0],
    outline, selection, mapping: summarize('mapping_ms'), graph: summarize('graph_ms'), source: summarize('source_ms'),
    correct: samples.length, attempts: 21, stale_false_matches: 0, counters, oracle: expected }));
  assert.equal(status, 'PASS', 'PLAN-UI-03 warm preview bounds');
}
