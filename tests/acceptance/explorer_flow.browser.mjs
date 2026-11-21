import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

let input = '';
for await (const chunk of process.stdin) {
  input += chunk;
  assert.ok(input.length <= 16384);
}
const fixture = JSON.parse(input);
const base = new URL(fixture.origin);
assert.equal(base.hostname, '127.0.0.1');
const chrome = process.env.MCE4D_T083_CHROMIUM;
assert.equal(createHash('sha256').update(await readFile(chrome)).digest('hex'),
  '24eb0a14de64ab0c9c2e283a5512ca817db64af8fd853cdb4d818f19a8e23b9b');
const browser = await chromium.launch({ executablePath: chrome, headless: true, chromiumSandbox: false });
try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 } });
  const outside = [];
  const requests = [];
  await context.route('**/*', async route => {
    const request = route.request();
    if (new URL(request.url()).origin !== base.origin) {
      outside.push(request.method());
      await route.abort();
      return;
    }
    requests.push(new URL(request.url()).pathname);
    await route.continue();
  });
  const page = await context.newPage();
  await page.goto(base.href);
  await page.getByLabel('Mã bootstrap').fill(fixture.code);
  await page.getByRole('button', { name: 'Kết nối', exact: true }).click();
  const graph = page.locator('#graph-pane');
  const choose = name => graph.getByRole('button', { name: new RegExp('^Chọn ' + name + ',') });
  await choose('PlaceOrders').waitFor({ timeout: 20000 });
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  await choose('PlaceOrders').click();
  const flowRequest = page.waitForResponse(response => response.url().endsWith('/flow/query'), { timeout: 45000 });
  const start = performance.now();
  await page.getByRole('button', { name: 'Flow backend', exact: true }).click();
  const response = await flowRequest;
  const coldMS = performance.now() - start;
  const flow = await response.json();
  assert.equal(response.status(), 200, JSON.stringify(flow.error));
  assert.equal(flow.data.mode, 'static');
  assert.equal(flow.data.root_ref.kind, 'symbol');
  const pane = page.getByRole('region', { name: 'Flow backend' });
  const source = page.locator('#source-pane').getByLabel('Mã nguồn của snapshot');
  const route = pane.getByRole('button', { name: /Đăng ký route/ });
  await route.focus();
  await route.press('Enter');
  await source.getByText('m.HandleFunc("POST /orders", PlaceOrders)', { exact: true }).waitFor();
  const insert = pane.getByRole('button', { name: /^INSERT/ });
  await insert.click();
  await source.getByText('INSERT INTO orders (quantity) VALUES (?)', { exact: true }).waitFor();
  await graph.getByRole('button', { name: 'Danh sách', exact: true }).click();
  const row = name => graph.locator('.symbol-row').filter({ has: page.getByText(name, { exact: true }) });
  assert.equal(await row('SaveOrders').getAttribute('aria-pressed'), 'true');
  const card = insert.locator('..');
  await card.getByText('1 thực thể có bằng chứng', { exact: true }).waitFor();
  await card.getByText('Chưa xác định', { exact: true }).waitFor();
  await card.getByText('Cùng giao dịch', { exact: false }).waitFor();
  await card.getByRole('button', { name: /^schema.sql/ }).click();
  await source.getByText(fixture.schema, { exact: true }).waitFor();
  assert.ok(flow.data.steps.some(step => step.step_kind === 'sql_insert' && step.outcome_status === 'outcome_unknown' &&
    step.source_refs.some(ref => ref.content_hash === fixture.schema_hash)));
  const begin = flow.data.steps.find(step => step.step_kind === 'sql_begin');
  assert.ok(begin);
  const group = flow.data.edges.filter(edge => edge.kind === 'transaction_link' && edge.from_step_id === begin.step_id);
  assert.equal(group.length, 3);
  assert.ok(group.every(edge => edge.evidence_refs[0].source_refs.length === 2));
  await row('PlaceOrders').click();
  assert.equal(await pane.getByRole('button', { name: /^Handler/ }).getAttribute('aria-pressed'), 'true');
  const bearer = (await response.request().allHeaders()).authorization;
  const denied = await page.request.post(new URL('api/explorer/v1/flow/query', base).href, {
    headers: { Authorization: bearer, Origin: base.origin }, data: response.request().postDataJSON(),
  });
  assert.equal(denied.status(), 403);
  await pane.getByRole('button', { name: 'Về Preview' }).click();
  assert.equal(await page.locator('iframe[title="Preview"]').getAttribute('src'), null);
  assert.equal(requests.filter(path => path.endsWith('/preview/attach')).length, 0);
  assert.equal(requests.filter(path => path.endsWith('/flow/query')).length, 1);
  assert.deepEqual(outside, []);
  console.log(JSON.stringify({ case: 'T097_NATIVE_BROWSER_FLOW', status: 'PASS', browser_sha256_verified: true,
    steps: flow.data.steps.length, edges: flow.data.edges.length, coldFlowMS: Math.round(coldMS), source_and_schema: true,
    no_preview_nav: true, keyboard: true, unauthenticated_intent_denied: true, target_requests: outside.length,
    transaction_links: group.length, persistence_unknown: true }));
} finally {
  await browser.close();
}
