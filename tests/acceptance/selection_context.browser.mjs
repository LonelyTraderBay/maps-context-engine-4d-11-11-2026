import assert from 'node:assert/strict';

export async function exerciseSelectionContext(page, lines, base, mapping) {
  const panel = page.getByRole('region', { name: 'Context của lựa chọn' });
  const requested = [];
  page.on('request', request => {
    if (request.url().endsWith('/context/pack')) requested.push(request);
  });
  await panel.getByRole('button', { name: 'Xem locator' }).click();
  const anchorLocator = JSON.parse(await panel.getByLabel('Locator của lựa chọn').inputValue());
  const source = mapping.data.matches[0].source_refs[0];
  assert.deepEqual(anchorLocator.selector, { kind: 'source_position', path: source.path, content_hash: source.content_hash,
    byte_offset: source.range.start_byte });
  assert.equal(anchorLocator.scope.pinned_vector.vector_digest, mapping.scope.vector_digest);
  const graphRow = page.locator('.symbol-row').filter({ has: page.getByText('Inner', { exact: true }) }).first();
  await graphRow.click();
  await panel.getByRole('button', { name: 'Xem locator' }).click();
  const locator = JSON.parse(await panel.getByLabel('Locator của lựa chọn').inputValue());
  assert.equal(locator.selector.kind, 'ref');
  assert.equal(locator.selector.ref.kind, 'symbol');
  assert.equal(locator.selector.service_id, null);
  assert.equal(locator.scope.pinned_vector.vector_digest, mapping.scope.vector_digest);
  assert.deepEqual(requested, []);
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: base.origin });
  await panel.getByRole('button', { name: 'Sao chép locator' }).click();
  await panel.getByText('Đã sao chép locator.', { exact: true }).waitFor();
  assert.deepEqual(JSON.parse(await page.evaluate(() => navigator.clipboard.readText())), locator);
  const exported = page.waitForEvent('download');
  await panel.getByRole('button', { name: 'Tải locator' }).click();
  assert.equal((await exported).suggestedFilename(), 'mce4d-selection.json');
  await panel.getByLabel('Ngân sách').selectOption('12288');
  const result = page.waitForResponse(response => response.url().endsWith('/context/pack'));
  await panel.getByRole('button', { name: 'Tạo context pack' }).click();
  const served = await result;
  const raw = await served.body();
  const envelope = JSON.parse(raw);
  assert.equal(served.status(), 200, envelope.error?.code);
  assert.equal(envelope.scope.vector_digest, locator.scope.pinned_vector.vector_digest);
  assert.deepEqual(envelope.data.target_refs[0], locator.selector.ref);
  assert.equal(envelope.budget.serialized_bytes, raw.length);
  assert.ok(envelope.budget.serialized_bytes <= 12288);
  assert.equal(envelope.pagination.truncated, true);
  assert.ok(envelope.pagination.required_pending > 0);
  assert.ok(envelope.pagination.next_cursor);
  assert.ok(envelope.data.items.some(item => item.source_refs.some(ref => ref.path === 'a.go')));
  await panel.getByText('Còn nội dung chờ', { exact: false }).waitFor();
  assert.equal(requested.length, 1);
  const request = served.request();
  assert.deepEqual(request.postDataJSON(), { ...locator, max_output_bytes: 12288 });
  const headers = { Authorization: (await request.allHeaders()).authorization, Origin: base.origin, 'X-MCE4D-Intent': 'explorer' };
  console.log(JSON.stringify({ case: 'T093_CONTEXT_PAGE', status: 'PASS', target: 'Inner',
    anchor_locator: true, typed_reference: true, local_copy_export: true,
    bytes: raw.length, required_pending: envelope.pagination.required_pending }));

  assert.equal((await lines.next()).value, 'context-gc');
  const expired = await page.request.post(new URL('/api/explorer/v1/context/pack', base).href, {
    headers, data: request.postDataJSON(),
  });
  const ended = await expired.json();
  assert.equal(expired.status(), 410, ended.error?.code);
  assert.equal(ended.error.code, 'SNAPSHOT_EXPIRED');
  assert.equal(ended.data, null);
  console.log(JSON.stringify({ case: 'T093_CONTEXT_EXPIRED', status: 'PASS', active_fallback: false }));
}
