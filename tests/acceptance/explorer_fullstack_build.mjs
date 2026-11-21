import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile, realpath } from 'node:fs/promises';
import { join } from 'node:path';
import { build } from 'vite';

const [source, expected] = process.argv.slice(2);
assert.match(expected, /^[0-9a-f]{64}$/);
const root = await realpath(source);
const entry = join(root, 'app', 'orders.tsx');
assert.equal(await realpath(entry), entry);
const hash = data => createHash('sha256').update(data).digest('hex');
assert.equal(hash(await readFile(entry)), expected);
const built = await build({
  configFile: false, root, logLevel: 'silent', publicDir: false,
  esbuild: { jsx: 'automatic' },
  build: {
    write: false, minify: false, target: 'es2022',
    lib: { entry, formats: ['es'], fileName: 'orders-fixture' },
    rollupOptions: { external: ['react-router-dom', 'react/jsx-runtime'] },
  },
});
const [output] = Array.isArray(built) ? built : [built];
const chunk = output.output.find(value => value.type === 'chunk' && value.isEntry);
assert.ok(chunk?.modules[entry]);
assert.match(chunk.code, /fetch\("\/backend\/orders", \{ method: "POST" \}\)/);
assert.equal(hash(await readFile(entry)), expected);
console.log(JSON.stringify({ fixture_build: 'vite_offline', source_sha256: expected,
  module_sha256: hash(chunk.code), target_scripts_started: 0 }));
