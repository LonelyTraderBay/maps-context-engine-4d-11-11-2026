'use strict';
// Independent primitive oracle for the specification vectors; not a wire JSON parser.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
function utf8(value) {
  if (!value.isWellFormed()) throw new Error('unpaired surrogate');
  return Buffer.from(value, 'utf8');
}
function encode(value, depth = 0) {
  if (depth > 32) throw new Error('depth');
  if (value === null) return Buffer.from('n');
  if (typeof value === 'boolean') return Buffer.from(value ? 't' : 'f');
  if (typeof value === 'number') {
    if (!Number.isSafeInteger(value)) throw new Error('integer');
    return Buffer.from(`i${Object.is(value, -0) ? 0 : value};`);
  }
  if (typeof value === 'string') {
    const raw = utf8(value);
    return Buffer.concat([Buffer.from(`s${raw.length}:`), raw]);
  }
  if (Array.isArray(value)) return Buffer.concat([Buffer.from(`a${value.length}:`), ...value.map(v => encode(v, depth + 1))]);
  if (typeof value === 'object' && Object.getPrototypeOf(value) === Object.prototype) {
    const keys = Object.keys(value);
    if (keys.length > 4096) throw new Error('keys');
    keys.sort((a, b) => Buffer.compare(utf8(a), utf8(b)));
    return Buffer.concat([Buffer.from(`o${keys.length}:`), ...keys.flatMap(k => [encode(k, depth + 1), encode(value[k], depth + 1)])]);
  }
  throw new Error('unsupported value');
}
const spec = fs.readFileSync(path.join(__dirname, '..', '..', 'Maps-Context-Engine-4D_SPEC.md'), 'utf8');
const section = spec.split('<!-- BEGIN MCE4D_HASH_VECTORS_V1 -->')[1].split('<!-- END MCE4D_HASH_VECTORS_V1 -->')[0];
const vectors = JSON.parse(section.split('```json')[1].split('```')[0]).valid;
if (vectors.length !== 14) throw new Error('wrong vector count');
for (const item of vectors) {
  const preimage = Buffer.concat([Buffer.from('MCE4D\0C14N1\0'), encode(item.domain), encode(item.value)]);
  if (preimage.toString('hex') !== item.encoding_hex || crypto.createHash('sha256').update(preimage).digest('hex') !== item.digest) throw new Error(item.case);
}
for (const invalid of [1.5, Number.MAX_SAFE_INTEGER + 1, '\ud800']) {
  let rejected = false;
  try { encode(invalid); } catch { rejected = true; }
  if (!rejected) throw new Error('accepted invalid primitive');
}
process.stdout.write('PASS: 14 normative vectors and invalid primitives (Node reference)\n');
