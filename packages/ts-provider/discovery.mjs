import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join, posix as path } from 'node:path';

const [compilerDir, deniedFile] = process.argv.slice(2);
assert.equal(process.version, 'v24.22.3');
assert.equal(process.permission.has('fs.read', deniedFile), false);
assert.equal(process.permission.has('fs.write', deniedFile), false);
assert.equal(process.permission.has('child'), false);
const ts = createRequire(import.meta.url)(join(compilerDir, 'lib/typescript.js'));
assert.equal(ts.version, '5.9.3');

const json = value => JSON.stringify(value);
const root = '/captured';
const compilerLib = '/compiler/lib';
const shared = {
  strict: true, target: 'ES2022', module: 'ESNext', moduleResolution: 'Bundler',
  jsx: 'preserve', lib: ['es2022'], types: [],
};
const appConfig = {
  extends: '../common.json',
  compilerOptions: { allowJs: true, checkJs: true, paths: { '@app/*': ['./src/*'], '@shared/api': ['../lib/dist/api.d.ts'] } },
  files: ['src/app.ts', 'src/dynamic.js', 'src/view.tsx', 'src/legacy.jsx', 'src/jsx.d.ts'],
  references: [{ path: '../lib' }],
};
const config = '/captured/app/tsconfig.json';
const sharedConfig = '/captured/lib/tsconfig.json';
const target = new Map(Object.entries({
  [config]: '// Only captured JSONC data is passed to TypeScript.\n' + json(appConfig),
  [sharedConfig]: json({ extends: '../common.json', compilerOptions: { composite: true, rootDir: 'src', outDir: 'dist' }, files: ['src/api.ts'] }),
  '/captured/common.json': json({ compilerOptions: shared }),
  '/captured/lib/src/api.ts': 'export function identity(value: number): number { return value; }',
  '/captured/lib/dist/api.d.ts': 'export declare function identity(value: number): number;',
  '/captured/app/package.json': json({ type: 'module', scripts: { prepare: 'node canary.cjs' } }),
  '/captured/app/src/local.ts': 'export function total(): number { return 1; }',
  '/captured/app/src/other.ts': 'export function total(): string { return "other"; }',
  '/captured/app/src/index.ts': 'export { total as summarize } from "./local.js";',
  '/captured/app/src/app.ts': [
    'import { summarize } from "@app/index";',
    'import { total as other } from "./other.js";',
    'import { identity } from "@shared/api";',
    'export const glyph = "🌱";',
    'export const first = summarize();',
    'export const second = other();',
    'export const third = identity(first);',
  ].join('\r\n'),
  '/captured/app/src/dynamic.js': [
    '/** @type {*} */ export const opaque = JSON.parse("{}");',
    'export const runtime = opaque.invoke();',
    '/** @param {string} name */',
    'export function load(name) { return import(name); }',
  ].join('\n'),
  '/captured/app/src/view.tsx': 'export const view = <badge/>;',
  '/captured/app/src/legacy.jsx': 'export const legacy = <badge/>;',
  '/captured/app/src/jsx.d.ts': 'declare namespace JSX { interface IntrinsicElements { badge: {} } }',
}));
const libraries = new Map(readdirSync(join(compilerDir, 'lib')).filter(name => /^lib\..+\.d\.ts$/.test(name))
  .map(name => [compilerLib + '/' + name, readFileSync(join(compilerDir, 'lib', name), 'utf8')]));
const files = new Map([...target, ...libraries]);
const directories = new Set();
for (const name of files.keys()) {
  for (let parent = path.dirname(name); parent !== '/'; parent = path.dirname(parent)) directories.add(parent);
}
const reads = new Set();
const read = name => {
  const key = path.normalize(name);
  reads.add(key);
  return files.get(key);
};
const fileExists = name => files.has(path.normalize(name));
const directoryExists = name => directories.has(path.normalize(name));
const noWrite = () => assert.fail('Compiler discovery cannot emit files');
const parseHost = {
  useCaseSensitiveFileNames: true, fileExists, readFile: read,
  getCurrentDirectory: () => root,
  readDirectory: () => assert.fail('Discovery fixture requires explicit file lists'),
  onUnRecoverableConfigFileDiagnostic: diagnostic => assert.fail('CONFIG_PARSE ' + diagnostic.code),
};

function parseCapturedConfig(name, active = new Set()) {
  assert.ok(name.startsWith(root + '/') && files.has(name), 'CONFIG_OUTSIDE_CAPTURE');
  assert.ok(!active.has(name), 'CONFIG_CYCLE');
  active.add(name);
  const result = ts.parseConfigFileTextToJson(name, read(name));
  assert.equal(result.error, undefined, 'CONFIG_PARSE');
  assert.ok(!Object.hasOwn(result.config.compilerOptions ?? {}, 'plugins'), 'CONFIG_PLUGINS_UNSUPPORTED');
  if (result.config.extends !== undefined) {
    const base = result.config.extends;
    assert.ok(typeof base === 'string' && (base.startsWith('./') || base.startsWith('../')) && base.endsWith('.json'),
      'CONFIG_EXTENDS_UNSUPPORTED');
    parseCapturedConfig(path.resolve(path.dirname(name), base), active);
  }
  for (const ref of result.config.references ?? []) {
    assert.ok(typeof ref.path === 'string' && (ref.path.startsWith('./') || ref.path.startsWith('../')),
      'CONFIG_REFERENCE_UNSUPPORTED');
    const resolved = path.resolve(path.dirname(name), ref.path);
    parseCapturedConfig(resolved.endsWith('.json') ? resolved : resolved + '/tsconfig.json', active);
  }
  active.delete(name);
}

function parseProject(name, overrides = {}) {
  parseCapturedConfig(name);
  return ts.getParsedCommandLineOfConfigFile(name, overrides, parseHost);
}

function loadProject(name) {
  const parsed = parseProject(name, { noEmit: true });
  assert.deepEqual(parsed.errors.map(item => item.code), []);
  const program = ts.createProgram({
    rootNames: parsed.fileNames, options: parsed.options, projectReferences: parsed.projectReferences,
    host: {
      fileExists, readFile: read, directoryExists,
      getSourceFile(name, languageVersion) {
        const contents = read(name);
        return contents === undefined ? undefined : ts.createSourceFile(name, contents, languageVersion, true);
      },
      getCurrentDirectory: () => root,
      getDefaultLibFileName: options => compilerLib + '/' + ts.getDefaultLibFileName(options),
      getDefaultLibLocation: () => compilerLib,
      getCanonicalFileName: name => path.normalize(name),
      useCaseSensitiveFileNames: () => true,
      getNewLine: () => '\n',
      writeFile: noWrite,
      getParsedCommandLine: parseProject,
    },
  });
  return program;
}

function declaration(program, file, name) {
  const result = program.getSourceFile(file)?.statements.flatMap(statement =>
    ts.isVariableStatement(statement) ? statement.declarationList.declarations : []
  ).find(variable => variable.name.getText() === name);
  assert.ok(result, 'expected declared variable');
  return result;
}

const program = loadProject(config);
assert.deepEqual(ts.getPreEmitDiagnostics(program).map(item => item.code), []);
const checker = program.getTypeChecker();
assert.equal(program.getResolvedProjectReferences()[0].sourceFile.fileName, sharedConfig);
assert.deepEqual(loadProject(sharedConfig).getRootFileNames(), ['/captured/lib/src/api.ts']);
const app = '/captured/app/src/app.ts';
const bindings = [
  ['first', '/captured/app/src/local.ts', 'number'],
  ['second', '/captured/app/src/other.ts', 'string'],
  ['third', '/captured/lib/dist/api.d.ts', 'number'],
];
for (const [name, expected, kind] of bindings) {
  const binding = declaration(program, app, name);
  assert.equal(checker.getResolvedSignature(binding.initializer)?.declaration?.getSourceFile().fileName, expected);
  assert.equal(checker.typeToString(checker.getTypeAtLocation(binding)), kind);
}
const symbol = declaration(program, app, 'first').name;
const source = symbol.getSourceFile().text;
const offset = symbol.getStart();
const bytes = Buffer.from(source, 'utf8');
const start = Buffer.byteLength(source.slice(0, offset));
const end = Buffer.byteLength(source.slice(0, symbol.getEnd()));
assert.ok(start > offset);
assert.equal(bytes.subarray(start, end).toString('utf8'), 'first');

const js = '/captured/app/src/dynamic.js';
const anyCall = declaration(program, js, 'runtime').initializer;
assert.ok(checker.getTypeAtLocation(anyCall.expression).flags & ts.TypeFlags.Any);
assert.equal(checker.getResolvedSignature(anyCall)?.declaration, undefined);
const dynamic = program.getSourceFile(js).statements.find(ts.isFunctionDeclaration).body.statements[0].expression;
assert.equal(dynamic.expression.kind, ts.SyntaxKind.ImportKeyword);
assert.equal(ts.isStringLiteralLike(dynamic.arguments[0]), false);
for (const extension of ['view.tsx', 'legacy.jsx']) {
  const tree = program.getSourceFile('/captured/app/src/' + extension);
  assert.ok(ts.isJsxSelfClosingElement(tree.statements[0].declarationList.declarations[0].initializer));
}
console.log(JSON.stringify({ check: 'C055_COMPILER', status: 'PASS', compiler: ts.version, node: process.version,
  bindings: bindings.length, reexport: true, project_reference_with_captured_declaration: true,
  jsx_and_tsx: true, any_and_dynamic_unresolved: true, utf16_and_utf8_distinct: true }));

files.delete('/captured/lib/dist/api.d.ts');
const incomplete = loadProject(config);
assert.ok(ts.getPreEmitDiagnostics(incomplete).some(item => item.code === 2307 || item.code === 6305));
assert.equal(incomplete.getTypeChecker().getResolvedSignature(declaration(incomplete, app, 'third').initializer)?.declaration, undefined);
files.set(config, json({ ...appConfig, compilerOptions: { ...appConfig.compilerOptions, plugins: [{ name: 'fixture-canary' }] } }));
assert.throws(() => loadProject(config), /CONFIG_PLUGINS_UNSUPPORTED/);
files.set(config, json(appConfig));
files.set('/captured/common.json', json({ compilerOptions: { ...shared, plugins: [{ name: 'fixture-canary' }] } }));
assert.throws(() => loadProject(config), /CONFIG_PLUGINS_UNSUPPORTED/);
files.set(config, json({ ...appConfig, extends: '../../outside.json' }));
assert.throws(() => loadProject(config), /CONFIG_OUTSIDE_CAPTURE/);
files.set(config, json({ ...appConfig, extends: 'unapproved-package/tsconfig.json' }));
assert.throws(() => loadProject(config), /CONFIG_EXTENDS_UNSUPPORTED/);
assert.ok([...reads].every(name => name.startsWith(root + '/') || name.startsWith(compilerLib + '/')));
assert.ok([...reads].every(name => !name.includes('canary')));
console.log(JSON.stringify({ check: 'C055_ADMISSION', status: 'PASS', missing_reference_unresolved: true,
  direct_and_inherited_plugin_rejected: true, outside_and_package_extends_rejected: true,
  child_process_allowed: false, target_fs_access_allowed: false, emissions: 0 }));
