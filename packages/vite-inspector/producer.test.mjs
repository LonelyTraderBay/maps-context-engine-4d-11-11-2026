import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import sourceMap from 'source-map-js';
import { build, createServer } from 'vite';
import { viteInspector } from './index.mjs';
import { instrumentSource } from './producer.mjs';

const hash = 'a'.repeat(64);
const id = '11111111-1111-4111-8111-111111111111';
const source = [
  'const plant = "🌱";',
  'function read() { return plant; }',
  'function Label() { return <span>{read()}</span>; }',
  'export function Page() { return <section><Label/><button onClick={read}>Item</button></section>; }',
  'export function Spread(props: object) { return <div {...props} />; }',
].join('\r\n');

const producerConfig = root => ({ enabled: true, root, worktreeID: id, profileID: hash,
  providerDigest: hash, buildConfigDigest: hash, inputManifestDigest: hash });

test('source anchors retain UTF-8 sites, ownership and source maps', async () => {
  const result = await instrumentSource('entry.tsx', source, hash);
  const { code, map } = result.transformed;
  assert.equal(code.replace(/ data-mce4d-anchor="[0-9a-f]{64}"/g, ''), source);
  assert.equal([...code.matchAll(/ data-mce4d-anchor="/g)].length, 3);
  assert.match(code, /<Label\/>/);
  assert.match(code, /<div \{\.\.\.props\} \/>/);
  const sliced = item => Buffer.from(source).subarray(item.range.start_byte, item.range.end_byte).toString('utf8');
  const click = result.anchors.find(item => item.kind === 'event_binding_site');
  const owner = result.anchors.find(item => item.anchor_id === click.owner_anchor_id);
  assert.equal(sliced(click), 'onClick={read}');
  assert.equal(sliced(owner), '<button');
  assert.equal(owner.kind, 'element_site');
  assert.ok(result.anchors.some(item => item.kind === 'component_render_site' && sliced(item) === '<Label'));
  assert.ok(result.anchors.some(item => item.kind === 'component_definition' && sliced(item) === 'Page'));
  assert.deepEqual(await instrumentSource('entry.tsx', source, hash), result);
  const consumer = new sourceMap.SourceMapConsumer(map);
  const generated = code.split('\r\n')[3].indexOf('onClick');
  assert.equal(consumer.originalPositionFor({ line: 4, column: generated }).column, source.split('\r\n')[3].indexOf('onClick'));
});

test('Vite development regenerates loaded-module manifest and production stays unchanged', async () => {
  const root = await mkdtemp(join(tmpdir(), 'mce4d-vite-'));
  const output = await mkdtemp(join(tmpdir(), 'mce4d-maps-'));
  const entry = join(root, 'entry.tsx');
  await writeFile(entry, source, { mode: 0o600, flag: 'wx' });
  const inspector = viteInspector(producerConfig(root));
  const server = await createServer({ root, configFile: false, logLevel: 'silent', plugins: [inspector],
    server: { middlewareMode: true, ws: false, watch: null }, optimizeDeps: { noDiscovery: true, include: [] } });
  try {
    const transformed = await server.transformRequest('/entry.tsx');
    assert.match(transformed.code, /data-mce4d-anchor/);
    const manifest = await inspector.api.buildManifest();
    const exported = await inspector.api.exportManifest(output);
    assert.equal(await inspector.api.exportManifest(output), exported);
    assert.deepEqual(JSON.parse(await readFile(exported, 'utf8')), manifest);
    await writeFile(exported, '{}\n');
    await assert.rejects(inspector.api.exportManifest(output), /Committed build manifest differs/);
    assert.equal(await readFile(exported, 'utf8'), '{}\n');
    await assert.rejects(inspector.api.exportManifest(root), /outside the target root/);
    assert.equal(manifest.module_coverage, 'loaded_modules_only');
    assert.equal(manifest.sources.length, 1);
    assert.ok(!Object.hasOwn(manifest, 'source_generation_id'));
    const changed = '// source edit\r\n' + source;
    await writeFile(entry, changed);
    inspector.handleHotUpdate({ file: entry });
    assert.equal((await inspector.api.buildManifest()).anchors.length, 0);
    await inspector.transform(changed, entry, {});
    const revised = await inspector.api.buildManifest();
    assert.notEqual(revised.build_map_id, manifest.build_map_id);
    assert.ok(revised.anchors.every(item => !manifest.anchors.some(old => old.anchor_id === item.anchor_id)));
    assert.equal(await inspector.transform('/* pre-transformed */' + changed, entry, {}), null);
    if (process.env.MCE4D_T085_MANIFEST_OUT) {
      await writeFile(join(process.env.MCE4D_T085_MANIFEST_OUT, 'fixture-source.tsx'), changed, { flag: 'wx' });
      await writeFile(join(process.env.MCE4D_T085_MANIFEST_OUT, 'fixture-build.json'), JSON.stringify(revised), { flag: 'wx' });
    }
  } finally { await server.close(); }
  const options = { root, configFile: false, mode: 'production', logLevel: 'silent',
    build: { write: false, lib: { entry, formats: ['es'], fileName: 'fixture' } } };
  const pinned = viteInspector(producerConfig(root));
  const baseline = await build(options);
  const withPlugin = await build({ ...options, plugins: [pinned] });
  const js = bundle => (Array.isArray(bundle) ? bundle[0] : bundle).output.find(chunk => chunk.type === 'chunk').code;
  assert.equal(js(withPlugin), js(baseline));
  assert.doesNotMatch(js(withPlugin), /mce4d-anchor|build_map_id|mce4d-vite-inspector/);
  assert.equal(await pinned.api.buildManifest(), null);
  assert.equal(await pinned.api.exportManifest(output), null);
  const ssrOptions = { ...options, build: { write: false, ssr: entry } };
  const ssrBaseline = await build(ssrOptions);
  const ssrWithPlugin = await build({ ...ssrOptions, plugins: [viteInspector(producerConfig(root))] });
  assert.equal(js(ssrWithPlugin), js(ssrBaseline));
  assert.equal(await readFile(entry, 'utf8'), '// source edit\r\n' + source);
});
