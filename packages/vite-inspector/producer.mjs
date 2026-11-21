import ts from 'typescript';
import sourceMap from 'source-map-js';
import { canonicalDigest, compareText, sha256 } from '../../web/explorer/src/canonical.ts';
import { checkRelativePath } from '../../web/explorer/src/paths.ts';

const { SourceMapGenerator } = sourceMap;
const utf8 = new TextEncoder();
const maxSourceBytes = 128 << 10;
const maxAnchors = 8192;
const maxManifestBytes = 4 << 20;
const hashPattern = /^[a-f0-9]{64}$/;
const uuidPattern = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/;
const attributeName = 'data-mce4d-anchor';

function sourceOffsets(text) {
  const offsets = new Uint32Array(text.length + 1);
  let bytes = 0;
  for (let unit = 0; unit < text.length;) {
    offsets[unit] = bytes;
    const point = text.codePointAt(unit);
    if (point > 0xffff) {
      offsets[unit + 1] = 0xffffffff;
      unit += 2;
      bytes += 4;
    } else {
      bytes += point < 0x80 ? 1 : point < 0x800 ? 2 : 3;
      unit++;
    }
  }
  offsets[text.length] = bytes;
  return offsets;
}

function compareAnchor(a, b) {
  return compareText(a.path, b.path) || a.range.start_byte - b.range.start_byte ||
    a.range.end_byte - b.range.end_byte || compareText(a.kind, b.kind) ||
    compareText(a.anchor_id, b.anchor_id);
}

function makeSourceMap(path, code, insertions) {
  const output = [];
  const generator = new SourceMapGenerator({ file: path });
  generator.setSourceContent(path, code);
  let previous = 0;
  let offset = 0;
  let originalLine = 1;
  let originalColumn = 0;
  let generatedColumn = 0;

  for (let i = 0; i <= code.length; i++) {
    if (offset < insertions.length && insertions[offset].at === i) {
      const { value } = insertions[offset++];
      output.push(code.slice(previous, i), value);
      previous = i;
      generator.addMapping({ generated: { line: originalLine, column: generatedColumn } });
      generatedColumn += value.length;
    }
    generator.addMapping({ generated: { line: originalLine, column: generatedColumn },
      original: { line: originalLine, column: originalColumn }, source: path });
    if (i === code.length) break;
    const char = code[i];
    if (char === '\n' || char === '\r' && code[i + 1] !== '\n' || char === '\u2028' || char === '\u2029') {
      originalLine++;
      originalColumn = generatedColumn = 0;
    } else {
      originalColumn++;
      generatedColumn++;
    }
  }
  output.push(code.slice(previous));
  return { code: output.join(''), map: generator.toJSON() };
}

function componentName(node) {
  const jsx = value => {
    while (value && ts.isParenthesizedExpression(value)) value = value.expression;
    return value && (ts.isJsxElement(value) || ts.isJsxSelfClosingElement(value) || ts.isJsxFragment(value));
  };
  const renders = body => body && (jsx(body) ||
    ts.isBlock(body) && body.statements.some(statement => ts.isReturnStatement(statement) && jsx(statement.expression)));
  if (ts.isFunctionDeclaration(node) && renders(node.body)) return node.name;
  if (ts.isVariableDeclaration(node) && node.initializer &&
      (ts.isArrowFunction(node.initializer) || ts.isFunctionExpression(node.initializer)) && renders(node.initializer.body))
    return node.name;
  return null;
}

// Source identity is always computed from the original string supplied after
// byte comparison at the owner boundary. The transform never wraps handlers.
export async function instrumentSource(path, code, providerDigest) {
  checkRelativePath(path);
  if (!hashPattern.test(providerDigest) || typeof code !== 'string' || !/\.[jt]sx$/.test(path) ||
      code.length > maxSourceBytes || !code.isWellFormed()) throw new Error('Unsupported JSX source');
  const bytes = utf8.encode(code);
  if (bytes.length > maxSourceBytes) throw new Error('JSX source exceeds budget');
  const contentHash = await sha256(bytes);
  const scriptKind = path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.JSX;
  const source = ts.createSourceFile(path, code, ts.ScriptTarget.Latest, true, scriptKind);
  if (source.parseDiagnostics.length) throw new Error('Cannot anchor a syntactically invalid JSX source');
  const offsets = sourceOffsets(code);
  const entries = [];
  const owners = new WeakMap();
  const insertions = [];
  function entry(kind, start, end, owner) {
    if (entries.length >= maxAnchors || offsets[start] === 0xffffffff || offsets[end] === 0xffffffff)
      throw new Error('JSX anchor limit or invalid UTF-8 boundary');
    const value = { kind, path, content_hash: contentHash,
      range: { start_byte: offsets[start], end_byte: offsets[end] }, owner };
    entries.push(value);
    return value;
  }
  function findOwner(node) {
    for (let parent = node.parent; parent; parent = parent.parent) {
      const found = owners.get(parent);
      if (found) return found;
    }
    return null;
  }
  function visit(node) {
    const name = componentName(node);
    if (name && ts.isIdentifier(name) && /^[A-Z]/.test(name.text))
      owners.set(node, entry('component_definition', name.getStart(source), name.getEnd(), findOwner(node)));
    if (!(ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node))) return;
    if (!ts.isIdentifier(node.tagName)) return;
    const isHost = /^[a-z][A-Za-z0-9]*$/.test(node.tagName.text);
    const isComponent = /^[A-Z]/.test(node.tagName.text);
    if (!isHost && !isComponent) return;
    if (isHost && node.attributes.properties.some(attr => !ts.isJsxAttribute(attr) || attr.name.getText(source) === attributeName))
      return;
    const parent = ts.isJsxOpeningElement(node) ? node.parent : node;
    const site = entry(isHost ? 'element_site' : 'component_render_site', node.getStart(source), node.tagName.getEnd(), findOwner(node));
    owners.set(parent, site);
    if (!isHost) return;
    insertions.push({ at: node.tagName.getEnd(), anchor: site });
    for (const attr of node.attributes.properties) {
      if (ts.isJsxAttribute(attr) && /^on[A-Z]/.test(attr.name.getText(source)) && attr.initializer &&
          ts.isJsxExpression(attr.initializer) && attr.initializer.expression)
        entry('event_binding_site', attr.getStart(source), attr.getEnd(), site);
    }
  }
  const pending = [source];
  while (pending.length) {
    const node = pending.pop();
    visit(node);
    const children = [];
    ts.forEachChild(node, child => { children.push(child); });
    for (let i = children.length - 1; i >= 0; i--) pending.push(children[i]);
  }
  const ids = new Map();
  for (const anchor of entries) {
    ids.set(anchor, await canonicalDigest('anchor-v1', { path, content_hash: contentHash, kind: anchor.kind,
      start_byte: anchor.range.start_byte, end_byte: anchor.range.end_byte, provider_digest: providerDigest }, maxManifestBytes));
  }
  const anchors = entries.map(({ owner, ...anchor }, i) => ({
    ...anchor, anchor_id: ids.get(entries[i]), owner_anchor_id: owner === null ? null : ids.get(owner),
  })).sort(compareAnchor);
  const attributes = insertions.map(({ at, anchor }) => ({ at, value: ` ${attributeName}="${ids.get(anchor)}"` })).sort((a, b) => a.at - b.at);
  return {
    source: { path, content_hash: contentHash }, anchors,
    transformed: attributes.length ? makeSourceMap(path, code, attributes) : null,
  };
}

export async function createBuildManifest(config, modules) {
  if (!uuidPattern.test(config.worktreeID) || !hashPattern.test(config.profileID) ||
      !hashPattern.test(config.providerDigest) || !hashPattern.test(config.buildConfigDigest) ||
      !hashPattern.test(config.inputManifestDigest) || !['development', 'test'].includes(config.mode) ||
      modules.size > 4096)
    throw new Error('Invalid build manifest configuration');
  const sources = [];
  const anchors = [];
  for (const [path, entry] of modules) {
    if (path !== entry.source.path || anchors.length + entry.anchors.length > maxAnchors)
      throw new Error('Build coverage exceeds budget');
    sources.push(entry.source);
    anchors.push(...entry.anchors);
  }
  sources.sort((a, b) => compareText(a.path, b.path));
  anchors.sort(compareAnchor);
  const manifest = { schema_version: 1, worktree_id: config.worktreeID, profile_id: config.profileID,
    provider_version: '1.0.0', provider_digest: config.providerDigest, build_config_digest: config.buildConfigDigest,
    input_manifest_digest: config.inputManifestDigest, mode: config.mode, module_coverage: 'loaded_modules_only', sources, anchors };
  const buildID = await canonicalDigest('build-map-v1', manifest, maxManifestBytes);
  return { ...manifest, build_map_id: buildID };
}
