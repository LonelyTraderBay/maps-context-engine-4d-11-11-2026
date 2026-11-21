import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';

export async function exerciseManualMappings(page, lines, base) {
  await page.getByRole('button', { name: 'Danh sách', exact: true }).click();
  await page.getByRole('button', { name: 'Hồ sơ mapping' }).click();
  const panel = page.getByRole('region', { name: 'Mapping thủ công' });
  const row = title => page.locator('.symbol-row').filter({ has: page.getByText(title, { exact: true }) }).first();
  await row('Show').click();
  await panel.getByRole('button', { name: 'Dùng nút đang chọn làm đích' }).click();
  await row('Read').click();
  await panel.getByLabel('Lý do đề xuất').fill('Synthetic manual relation');
  const proposed = page.waitForResponse(r => r.url().endsWith('/mapping/propose') && r.ok());
  await panel.getByRole('button', { name: 'Đề xuất mapping' }).click();
  const proposal = await proposed;
  const original = await proposal.json();
  assert.equal(original.data.declared_status, 'proposed');
  const request = proposal.request();
  const body = request.postDataJSON();
  assert.equal(body.source_ref.path, 'b.go');
  assert.equal(Object.hasOwn(body, 'actor'), false);
  const headers = { Authorization: (await request.allHeaders()).authorization, Origin: base.origin, 'X-MCE4D-Intent': 'explorer' };
  async function api(path, data) {
    const response = await page.request.post(new URL('/api/explorer/v1/' + path, base).href, { headers, data });
    return [response.status(), await response.json()];
  }
  const [sameStatus, same] = await api('mapping/propose', body);
  assert.equal(sameStatus, 200, same.error?.code);
  assert.equal(same.data.record_id, original.data.record_id);
  assert.equal(same.data.version, original.data.version);
  const [conflictStatus, conflict] = await api('mapping/propose', { ...body, rationale: 'Conflicting synthetic receipt' });
  assert.equal(conflictStatus, 409);
  assert.equal(conflict.error.code, 'RECORD_CONFLICT');
  const [forgedStatus] = await api('mapping/propose', { ...body, actor: 'fixture-foreign-actor' });
  assert.equal(forgedStatus, 400);

  const listed = panel.getByRole('list', { name: 'Hồ sơ mapping' });
  const opened = page.waitForResponse(r => r.url().endsWith('/records/read'));
  await listed.getByRole('button', { name: 'b.go · Chờ duyệt · v1' }).click();
  const detail = await (await opened).json();
  assert.equal(detail.data?.ui_mapping?.rationale, 'Synthetic manual relation', detail.error?.code);
  await panel.getByText('Synthetic manual relation', { exact: true }).waitFor();
  await panel.getByLabel('Lý do duyệt').fill('Reviewed captured source');
  const accepted = page.waitForResponse(r => r.url().endsWith('/mapping/review') && r.ok());
  await panel.getByRole('button', { name: 'Duyệt mapping', exact: true }).click();
  const approval = await (await accepted).json();
  assert.equal(approval.data.effective_status, 'approved');
  assert.equal(approval.data.version, 2);
  await listed.getByRole('button', { name: 'b.go · Đã duyệt · v2' }).waitFor();

  const [, current] = await api('records/list', { record_kind: 'ui_mapping', limit: 10 });
  const [secondStatus, second] = await api('mapping/propose', {
    ...body, scope: { pinned_vector: current.scope }, rationale: 'Second synthetic manual relation', client_mutation_id: randomUUID(),
  });
  assert.equal(secondStatus, 200, second.error?.code);
  const [, listAfterProposal] = await api('records/list', { record_kind: 'ui_mapping', limit: 1 });
  assert.ok(listAfterProposal.pagination.next_cursor);
  const [nextStatus, nextPage] = await api('records/list', {
    record_kind: 'ui_mapping', limit: 1, cursor: listAfterProposal.pagination.next_cursor,
  });
  assert.equal(nextStatus, 200, nextPage.error?.code);
  assert.notEqual(nextPage.data.records[0].record_id, listAfterProposal.data.records[0].record_id);
  const review = { scope: { pinned_vector: listAfterProposal.scope }, record_id: second.data.record_id,
    expected_version: 1, action: 'approve', reason: 'Concurrent review fixture' };
  const competing = await Promise.all([
    api('mapping/review', { ...review, client_mutation_id: randomUUID() }),
    api('mapping/review', { ...review, client_mutation_id: randomUUID() }),
  ]);
  assert.deepEqual(competing.map(result => result[0]).sort(), [200, 409]);
  assert.equal(competing.find(result => result[0] === 409)[1].error.code, 'RECORD_CONFLICT');
  const [, secondHead] = await api('records/read', { record_id: second.data.record_id });
  assert.equal(secondHead.data.record.version, 2);
  const [revokedStatus, revoked] = await api('mapping/review', { ...review, scope: { pinned_vector: secondHead.scope },
    expected_version: 2, action: 'revoke', reason: 'Synthetic revocation', client_mutation_id: randomUUID() });
  assert.equal(revokedStatus, 200, revoked.error?.code);
  assert.equal(revoked.data.effective_status, 'revoked');
  const [oldCursorStatus, oldCursor] = await api('records/list', {
    record_kind: 'ui_mapping', limit: 1, cursor: listAfterProposal.pagination.next_cursor,
  });
  assert.equal(oldCursorStatus, 410);
  assert.equal(oldCursor.error.code, 'CURSOR_EXPIRED');
  console.log(JSON.stringify({ case: 'T092_RECORDS_REVIEWED', status: 'PASS', replay: true,
    one_competing_approval: true, actor_field_rejected: true, old_cursor_expired: true }));

  assert.equal((await lines.next()).value, 'source-drift');
  const [driftStatus, drift] = await api('records/read', { record_id: original.data.record_id });
  assert.equal(driftStatus, 200, drift.error?.code);
  assert.equal(drift.data.record.declared_status, 'approved');
  assert.equal(drift.data.record.effective_status, 'needs_review');
  assert.equal(drift.data.record.provenance_status, 'source_changed');
  assert.equal(drift.data.ui_mapping, null);
  console.log(JSON.stringify({ case: 'T092_RECORDS_DRIFT', status: 'PASS', provenance: 'source_changed' }));

  assert.equal((await lines.next()).value, 'gc-complete');
  const [gcStatus, gc] = await api('records/read', { record_id: original.data.record_id });
  assert.equal(gcStatus, 200, gc.error?.code);
  assert.equal(gc.data.record.version, 2);
  assert.equal(gc.data.record.effective_status, 'needs_review');
  assert.equal(gc.data.record.provenance_status, 'snapshot_expired');
  assert.equal(gc.data.ui_mapping, null);
  console.log(JSON.stringify({ case: 'T092_RECORDS_GC', status: 'PASS', provenance: 'snapshot_expired' }));
}
