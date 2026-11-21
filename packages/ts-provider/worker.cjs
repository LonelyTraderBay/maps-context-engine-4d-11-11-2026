'use strict';

const fs = require('node:fs');
const { createHash } = require('node:crypto');
const disk = require('node:path');
const path = disk.posix;

const workspace = '/capture';
const library = '/compiler/lib';
const maxSourceBytes = 64 << 20;
const maxFacts = 50000;
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const identity = (kind, ...parts) => kind + '-' + sha(JSON.stringify(parts));
const utf8 = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });
const order = (a, b) => a < b ? -1 : a > b ? 1 : 0;

class UnsupportedConfig extends Error {}

function readExactly(size) {
  const data = Buffer.alloc(size);
  for (let offset = 0; offset < size;) {
    const count = fs.readSync(0, data, offset, size - offset);
    if (!count) throw new Error('PROVIDER_INPUT_EOF');
    offset += count;
  }
  return data;
}

function receive() {
  let header = '';
  while (!header.endsWith('\r\n\r\n') && header.length < 64) header += readExactly(1).toString('ascii');
  const length = Number(/^Content-Length: ([0-9]+)\r\n\r\n$/.exec(header)?.[1]);
  if (!Number.isSafeInteger(length) || length < 1 || length > 65536) throw new Error('PROVIDER_INPUT_FRAME');
  return JSON.parse(utf8.decode(readExactly(length)));
}

function loadCapture(request) {
  const raw = fs.readFileSync(request.manifest_path);
  if (sha(raw) !== request.manifest_digest) throw new Error('PROVIDER_INPUT_DIGEST');
  const manifest = JSON.parse(utf8.decode(raw));
  if (manifest.profile_id !== request.profile_id || manifest.provider_digest !== request.provider_digest)
    throw new Error('PROVIDER_INPUT_PROFILE');
  const files = new Map();
  let available = maxSourceBytes;
  for (const item of manifest.files) {
    if (item.status !== 'captured') continue;
    if (item.byte_length < 0 || item.byte_length > available) throw new Error('PROVIDER_INPUT_BUDGET');
    const absolute = path.resolve(workspace, item.path);
    if (!absolute.startsWith(workspace + '/') || absolute.slice(workspace.length + 1) !== item.path)
      throw new Error('PROVIDER_INPUT_PATH');
    const bytes = fs.readFileSync(disk.join(request.snapshot_root, item.path));
    if (bytes.length !== item.byte_length || sha(bytes) !== item.content_hash) throw new Error('PROVIDER_INPUT_SOURCE');
    available -= bytes.length;
    files.set(absolute, utf8.decode(bytes));
  }
  return { manifest, files };
}

function compilerHost(ts, compilerRoot, files) {
  const libRoot = disk.join(compilerRoot, 'lib');
  const libraries = new Set(fs.readdirSync(libRoot).filter(name => /^lib\..+\.d\.ts$/.test(name)));
  const directories = new Map();
  const sourceObjects = new Map();
  function entry(name) {
    if (!directories.has(name)) directories.set(name, { files: [], directories: [] });
    return directories.get(name);
  }
  function register(name) {
    const dir = path.dirname(name);
    entry(dir).files.push(path.basename(name));
    for (let child = dir; child !== '/'; child = path.dirname(child)) {
      const list = entry(path.dirname(child)).directories;
      const childName = path.basename(child);
      if (!list.includes(childName)) list.push(childName);
    }
  }
  for (const name of files.keys()) register(name);
  for (const name of libraries) register(library + '/' + name);
  for (const value of directories.values()) {
    value.files.sort(order);
    value.directories.sort(order);
  }
  function libName(name) {
    return path.dirname(name) === library && libraries.has(path.basename(name)) ? path.basename(name) : null;
  }
  function readFile(name) {
    const normalized = path.normalize(name);
    const captured = files.get(normalized);
    if (captured !== undefined) return captured;
    const lib = libName(normalized);
    return lib === null ? undefined : fs.readFileSync(disk.join(libRoot, lib), 'utf8');
  }
  const filesystem = {
    readFile,
    fileExists: name => files.has(path.normalize(name)) || libName(path.normalize(name)) !== null,
    directoryExists: name => directories.has(path.normalize(name)),
    getDirectories: name => (directories.get(path.normalize(name))?.directories ?? []).map(child => path.join(name, child)),
  };
  const roots = new Map();
  const active = new Set();
  function relativeConfig(parent, value, reference) {
    if (typeof value !== 'string' || !(value.startsWith('./') || value.startsWith('../')))
      throw new UnsupportedConfig('TS_CONFIG_OUTSIDE_CAPTURE');
    const found = path.resolve(path.dirname(parent), value);
    return reference && !found.endsWith('.json') ? path.join(found, 'tsconfig.json') : found;
  }
  function inspect(name) {
    if (active.has(name)) throw new UnsupportedConfig('TS_CONFIG_CYCLE');
    if (roots.has(name)) return;
    if (!files.has(name) || !name.startsWith(workspace + '/') || !name.endsWith('.json') || roots.size + active.size >= 64)
      throw new UnsupportedConfig('TS_CONFIG_OUTSIDE_CAPTURE');
    active.add(name);
    const result = ts.parseConfigFileTextToJson(name, files.get(name));
    if (result.error || !result.config || typeof result.config !== 'object') throw new UnsupportedConfig('TS_CONFIG_INVALID');
    const config = result.config;
    if (Object.hasOwn(config.compilerOptions ?? {}, 'plugins') || config.typeAcquisition?.enable)
      throw new UnsupportedConfig('TS_CONFIG_PLUGINS_UNSUPPORTED');
    if (config.extends !== undefined) inspect(relativeConfig(name, config.extends, false));
    if (config.references !== undefined && !Array.isArray(config.references))
      throw new UnsupportedConfig('TS_CONFIG_INVALID');
    for (const reference of config.references ?? []) inspect(relativeConfig(name, reference?.path, true));
    active.delete(name);
    roots.set(name, config);
  }
  const configHost = {
    ...filesystem,
    useCaseSensitiveFileNames: true,
    getCurrentDirectory: () => workspace,
    onUnRecoverableConfigFileDiagnostic: () => { throw new UnsupportedConfig('TS_CONFIG_INVALID'); },
    readDirectory: (dir, extensions, excludes, includes, depth) => ts.matchFiles(
      dir, extensions, excludes, includes, true, workspace, depth,
      name => directories.get(path.normalize(name)) ?? { files: [], directories: [] }, name => path.normalize(name)),
  };
  function parse(name, overrides = {}) {
    inspect(name);
    const result = ts.getParsedCommandLineOfConfigFile(name, overrides, configHost);
    if (!result || result.errors.length) throw new UnsupportedConfig('TS_CONFIG_INVALID');
    return result;
  }
  return {
    open(name) {
      // A Program owns mutable TypeScript compiler objects, never share them
      // between effective projects even when input text is shared.
      sourceObjects.clear();
      const parsed = parse(name, { noEmit: true });
      return ts.createProgram({
        rootNames: parsed.fileNames, options: parsed.options, projectReferences: parsed.projectReferences,
        host: {
          ...filesystem,
          getSourceFile(filename, option) {
            if (!sourceObjects.has(filename)) {
              const text = readFile(filename);
              if (text === undefined) return undefined;
              sourceObjects.set(filename, ts.createSourceFile(filename, text, option, true));
            }
            return sourceObjects.get(filename);
          },
          getDefaultLibFileName: options => library + '/' + ts.getDefaultLibFileName(options),
          getDefaultLibLocation: () => library,
          getCurrentDirectory: () => workspace,
          getCanonicalFileName: name => path.normalize(name),
          useCaseSensitiveFileNames: () => true,
          getNewLine: () => '\n',
          getParsedCommandLine: name => parse(name),
          writeFile: () => { throw new Error('PROVIDER_EMIT_DENIED'); },
        },
      });
    },
  };
}

function offsets(text) {
  const positions = new Uint32Array(text.length + 1);
  let bytes = 0;
  for (let unit = 0; unit < text.length;) {
    positions[unit] = bytes;
    const point = text.codePointAt(unit);
    if (point > 0xffff) {
      positions[unit + 1] = 0xffffffff;
      unit += 2;
      bytes += 4;
    } else {
      unit++;
      bytes += point < 0x80 ? 1 : point < 0x800 ? 2 : 3;
    }
  }
  positions[text.length] = bytes;
  return node => {
    const start = positions[node.getStart()];
    const end = positions[node.getEnd()];
    if (start === undefined || end === undefined || end < start || end === 0xffffffff)
      throw new Error('PROVIDER_UTF8_RANGE');
    return { start_byte: start, end_byte: end };
  };
}

function walk(ts, root, visit) {
  const pending = [root];
  while (pending.length) {
    const node = pending.pop();
    visit(node);
    const children = [];
    ts.forEachChild(node, child => { children.push(child); });
    for (let index = children.length - 1; index >= 0; index--) pending.push(children[index]);
  }
}

function declarationKind(ts, node) {
  if (ts.isFunctionDeclaration(node)) return 'function';
  if (ts.isMethodDeclaration(node) || ts.isMethodSignature(node)) return 'method';
  if (ts.isInterfaceDeclaration(node)) return 'interface';
  if (ts.isClassDeclaration(node) || ts.isTypeAliasDeclaration(node) || ts.isEnumDeclaration(node)) return 'type';
  if (ts.isPropertyDeclaration(node) || ts.isPropertySignature(node)) return 'field';
  if (ts.isVariableDeclaration(node)) return 'variable';
  if (ts.isParameter(node)) return 'parameter';
  return null;
}

function analyzeProject(ts, projectPath, program, capture, request, facts, allGaps) {
  const profileID = request.profile_id;
  const packageKey = identity('tspkg', profileID, projectPath);
  const sources = program.getSourceFiles()
    .filter(file => capture.files.has(file.fileName) && !file.fileName.endsWith('.json'))
    .sort((a, b) => order(a.fileName, b.fileName));
  const declarations = new Map();
  const checks = new Map();
  const typed = request.profile.analysis_mode === 'typed_when_available';
  const checker = typed ? program.getTypeChecker() : null;
  const gaps = new Set();
  const diagnostics = typed ? ts.getPreEmitDiagnostics(program) : program.getSyntacticDiagnostics();
  if (diagnostics.length) gaps.add('TS_COMPILER_DIAGNOSTICS');
  if (!typed) gaps.add('TS_SYNTAX_ONLY');
  for (const source of sources) {
    const filename = source.fileName.slice(workspace.length + 1);
    const renderRange = offsets(source.text);
    checks.set(source, renderRange);
    const fileID = identity('tssym', packageKey, filename, 'file');
    const file = { symbol_id: fileID, path: filename, package_key: packageKey,
      kind: 'file', name: path.basename(filename).slice(0, 512), qualified_name: filename.slice(0, 2048),
      scope_key: fileID, range: { start_byte: 0, end_byte: Buffer.byteLength(source.text) }, signature: '', exported: false };
    declarations.set(source, file);
    append(facts, 'symbols', file);
    walk(ts, source, node => {
      const kind = declarationKind(ts, node);
      if (!kind || !node.name || !(ts.isIdentifier(node.name) || ts.isPrivateIdentifier(node.name))) return;
      const owner = enclosing(node);
      const range = renderRange(node.name);
      const name = node.name.text.slice(0, 512);
      const signature = kind === 'function' || kind === 'method' ? 'arity:' + node.parameters.length : '';
      const id = identity('tssym', packageKey, filename, kind, owner.symbol_id, range.start_byte);
      const exported = !!node.modifiers?.some(part => part.kind === ts.SyntaxKind.ExportKeyword) ||
        !!node.parent?.parent?.modifiers?.some(part => part.kind === ts.SyntaxKind.ExportKeyword);
      const item = { symbol_id: id, path: filename, package_key: packageKey, kind, name,
        qualified_name: (owner.qualified_name + '#' + name).slice(0, 2048),
        scope_key: lexicalScope(node, owner, renderRange), range, signature, exported };
      declarations.set(node, item);
      append(facts, 'symbols', item);
      edge(source, node.name, owner, item, 'CONTAINS', 'syntax_exact');
    });
  }
  function enclosing(node) {
    for (let current = node.parent; current; current = current.parent) {
      const result = declarations.get(current);
      if (result) return result;
    }
    return declarations.get(node.getSourceFile());
  }
  function lexicalScope(node, owner, renderRange) {
    for (let scope = node.parent; scope; scope = scope.parent) {
      if (declarations.get(scope) === owner) break;
      if (ts.isBlock(scope) || ts.isModuleBlock(scope) || ts.isCaseBlock(scope) ||
          ts.isCatchClause(scope) || ts.isForStatement(scope) || ts.isForOfStatement(scope) ||
          ts.isForInStatement(scope)) {
        const range = renderRange(scope);
        return identity('tsscope', owner.symbol_id, range.start_byte, range.end_byte);
      }
    }
    return owner.symbol_id;
  }
  function targetOf(symbol) {
    if (!symbol) return null;
    if (symbol.flags & ts.SymbolFlags.Alias) symbol = checker.getAliasedSymbol(symbol);
    const targets = [...new Set((symbol?.declarations ?? []).map(node => declarations.get(node)).filter(Boolean))];
    return targets.length === 1 ? targets[0] : null;
  }
  function edge(source, site, owner, target, kind, evidence, label = null) {
    const range = checks.get(source)(site);
    append(facts, 'relations', { relation_id: identity('tsrel', packageKey, owner.symbol_id, target?.symbol_id ?? '',
      kind, range.start_byte, range.end_byte, owner.path), source_symbol_id: owner.symbol_id,
      target_symbol_id: target?.symbol_id ?? null, kind, evidence_kind: evidence,
      site_path: source.fileName.slice(workspace.length + 1), range, reason: label === null ? '' : 'binding_not_proven',
      unresolved_label: label });
  }
  if (typed) for (const source of sources) {
    walk(ts, source, node => {
      if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier) {
        const resolved = targetOf(checker.getSymbolAtLocation(node.moduleSpecifier));
        if (resolved) edge(source, node.moduleSpecifier, enclosing(node), resolved, 'IMPORTS', 'type_resolved');
        else {
          gaps.add('TS_UNRESOLVED_IMPORT');
          edge(source, node.moduleSpecifier, enclosing(node), null, 'UNRESOLVED_REF', 'unresolved', 'import');
        }
      }
      if (ts.isExportSpecifier(node) || ts.isImportSpecifier(node)) {
        const resolved = targetOf(checker.getSymbolAtLocation(node.name));
        if (resolved) edge(source, node.name, enclosing(node), resolved, 'REFERS_TO', 'type_resolved');
      }
      if (!ts.isCallExpression(node)) return;
      const site = node.expression;
      if (site.kind === ts.SyntaxKind.ImportKeyword) {
        gaps.add('TS_DYNAMIC_IMPORT');
        edge(source, site, enclosing(node), null, 'UNRESOLVED_REF', 'unresolved', 'dynamic import');
        return;
      }
      const type = checker.getTypeAtLocation(site);
      const declaration = checker.getResolvedSignature(node)?.declaration;
      const target = declaration ? declarations.get(declaration) : null;
      if (!(type.flags & ts.TypeFlags.Any) && target && ts.isFunctionDeclaration(declaration)) {
        edge(source, site, enclosing(node), target, 'CALLS_DIRECT', 'type_resolved');
      } else {
        gaps.add('TS_UNRESOLVED_CALL');
        edge(source, site, enclosing(node), null, 'UNRESOLVED_CALL', 'unresolved', 'call');
      }
    });
  }
  const diagnosticIDs = new Set();
  for (const diagnostic of diagnostics.slice(0, 512)) {
    if (diagnostic.file && !capture.files.has(diagnostic.file.fileName)) continue;
    const filename = diagnostic.file ? diagnostic.file.fileName.slice(workspace.length + 1) : null;
    const id = identity('tsdiag', packageKey, diagnostic.code, filename, diagnostic.start ?? 0, diagnostic.length ?? 0);
    if (diagnosticIDs.has(id)) continue;
    diagnosticIDs.add(id);
    append(facts, 'diagnostics', {
      diagnostic_id: id,
      path: filename, package_key: packageKey, code: 'TS' + diagnostic.code,
      severity: diagnostic.category === ts.DiagnosticCategory.Error ? 'error' : 'warning',
      message: 'TypeScript diagnostic ' + diagnostic.code, scope_description: 'Captured TS/JS project',
    });
  }
  append(facts, 'packages', { package_key: packageKey, import_path: projectPath, module_key: packageKey,
    variant: 'production', input_digest: capture.manifest.snapshot_digest,
    semantic_status: typed && gaps.size === 0 ? 'complete_in_profile' : 'partial' });
  for (const gap of gaps) allGaps.add(gap);
}

function append(facts, kind, entry) {
  if (facts.count++ >= maxFacts) throw new Error('PROVIDER_FACT_BUDGET');
  facts[kind].push(entry);
}

function sendFrame(bytes, limit) {
  if (bytes.length > limit) throw new Error('PROVIDER_OUTPUT_FRAME');
  for (const data of [Buffer.from('Content-Length: ' + bytes.length + '\r\n\r\n'), bytes]) {
    for (let offset = 0; offset < data.length;) {
      const size = fs.writeSync(1, data, offset, data.length - offset);
      if (size < 1) throw new Error('PROVIDER_OUTPUT_EOF');
      offset += size;
    }
  }
}

function response(request, snapshotDigest, facts, gaps, version) {
  let sequence = 1;
  function frame(value) {
    return Buffer.from(JSON.stringify({ provider_protocol: 1, job_id: request.job_id, sequence, ...value }));
  }
  function send(bytes) {
    sendFrame(bytes, request.max_frame_bytes);
    sequence++;
  }
  const empty = { packages: [], symbols: [], relations: [], diagnostics: [] };
  function batch(kind, entries) {
    if (!entries.length) return;
    const bytes = frame({ type: 'analyze_batch', ...empty, [kind]: entries });
    if (bytes.length <= request.max_frame_bytes) send(bytes);
    else {
      if (entries.length === 1) throw new Error('PROVIDER_OUTPUT_ITEM');
      const half = Math.floor(entries.length / 2);
      batch(kind, entries.slice(0, half));
      batch(kind, entries.slice(half));
    }
  }
  for (const kind of Object.keys(empty)) {
    for (let first = 0; first < facts[kind].length; first += 256) batch(kind, facts[kind].slice(first, first + 256));
  }
  const seen = facts.symbols.length > 0;
  const typed = request.profile.analysis_mode === 'typed_when_available';
  const status = seen && typed && gaps.size === 0 ? 'complete_in_supported_scope' : 'partial';
  send(frame({ type: 'analyze_done', status, input_digest: snapshotDigest,
    capabilities: seen ? [{ name: typed ? 'ts_static_types' : 'ts_syntax', provider_id: request.provider_id,
      provider_version: version, status: status === 'partial' ? 'partial' : 'supported_scope_complete' }] : [],
    gaps: [...gaps].sort(order).map(code => ({ code, reason: 'Coverage within the captured TS/JS project is incomplete.',
      count: null, source_refs: [] })), error: null }));
}

function main() {
  const compilerRoot = process.env.MCE4D_COMPILER_ROOT;
  if (!compilerRoot || process.version !== 'v24.22.3' || process.permission.has('child'))
    throw new Error('PROVIDER_RUNTIME');
  const ts = require(disk.join(compilerRoot, 'lib/typescript.js'));
  if (ts.version !== '5.9.3') throw new Error('PROVIDER_COMPILER_VERSION');
  const request = receive();
  if (request.type !== 'analyze_start' || request.profile.allow_project_plugins ||
      request.profile.language !== 'typescript_javascript') throw new Error('PROVIDER_PROFILE');
  const capture = loadCapture(request);
  const host = compilerHost(ts, compilerRoot, capture.files);
  const facts = { count: 0, packages: [], symbols: [], relations: [], diagnostics: [] };
  const gaps = new Set();
  const frontend = process.env.MCE4D_PROVIDER_MODE === 'frontend';
  const routes = [];
  const clients = [];
  if (capture.manifest.files.some(file => file.status !== 'captured')) gaps.add('TS_CAPTURE_INCOMPLETE');
  for (const config of request.profile.tsconfig_paths.slice().sort(order)) {
    const program = (() => {
      try { return host.open(workspace + '/' + config); }
      catch (err) {
        if (!(err instanceof UnsupportedConfig)) throw err;
        gaps.add(err.message);
        return null;
      }
    })();
    if (!program) continue;
    if (!frontend) analyzeProject(ts, config, program, capture, request, facts, gaps);
    else if (request.profile.analysis_mode === 'syntax') gaps.add('TS_SYNTAX_ONLY');
    else {
      const result = analyzeFrontend(ts, program, capture.files.keys(), offsets);
      if (routes.length + result.routes.length > 4096 || clients.length + result.clients.length > 4096)
        throw new Error('FRONTEND_FACT_LIMIT');
      for (const item of result.routes) routes.push({ config, ...item });
      for (const item of result.clients) clients.push({ config, ...item });
      for (const gap of result.gaps) gaps.add(gap);
    }
  }
  if (frontend) {
    const output = { frontend_protocol: 1, job_id: request.job_id,
      input_digest: capture.manifest.snapshot_digest, routes, clients, gaps: [...gaps].sort(order) };
    sendFrame(Buffer.from(JSON.stringify(output)), request.max_frame_bytes);
    return;
  }
  response(request, capture.manifest.snapshot_digest, facts, gaps, ts.version);
}

if (require.main === module) {
  try { main(); }
  catch {
    // Only a bounded category reaches stderr. Captured text never does.
    process.stderr.write('TS_PROVIDER_FAILED\n');
    process.exitCode = 2;
  }
}

module.exports = { compilerHost, offsets };
