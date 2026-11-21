'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');
const { join } = require('node:path');
const { compilerHost, offsets } = require('./worker.cjs');
const { analyzeFrontend } = require('./frontend.cjs');

const root = join(process.env.MCE4D_T044_COMPILER, 'compiler');
const ts = require(join(root, 'lib/typescript.js'));
assert.equal(ts.version, '5.9.3');
assert.equal(process.permission.has('child'), false);

const supports = {
  '/capture/tsconfig.json': '{"compilerOptions":{"target":"es2022","module":"esnext","moduleResolution":"bundler","jsx":"preserve","types":[],"lib":["es2022","dom"]},"files":["routes.tsx","router.d.ts","page.tsx"]}',
  '/capture/router.d.ts': 'declare module "react-router-dom" { export function Route(value: object): JSX.Element; export function Routes(value: object): JSX.Element; }\n' +
    'declare namespace JSX { interface Element {} interface IntrinsicElements { main: {} } }',
  '/capture/page.tsx': 'export function Page() { return <main />; }',
};

function run(source) {
  const files = new Map(Object.entries({ ...supports, '/capture/routes.tsx': source }));
  const program = compilerHost(ts, root, files).open('/capture/tsconfig.json');
  return analyzeFrontend(ts, program, files.keys(), offsets);
}

function slice(source, site) {
  return Buffer.from(source).subarray(site.start_byte, site.end_byte).toString('utf8');
}

test('C056 binds name-resolved components and exact fetch literal syntax', () => {
  const source = [
    'import { Route as Entry, Routes } from "react-router-dom";',
    'import { Page as LocalPage } from "./page";',
    'const text = "🌱";',
    'export const router = <Routes><Entry path="/items" element={<LocalPage />} /></Routes>;',
    'export function load() { return fetch("/api/items", { method: "POST" }); }',
    'export function read() { return fetch("/api/items"); }',
  ].join('\r\n');
  const result = run(source);
  assert.deepEqual(result.gaps, []);
  assert.deepEqual(result.routes.map(item => item.path), ['/items']);
  assert.equal(slice(source, result.routes[0].site), '"/items"');
  assert.equal(result.routes[0].component.path, 'page.tsx');
  assert.equal(slice(supports['/capture/page.tsx'], result.routes[0].component), 'Page');
  assert.deepEqual(result.clients.map(item => [item.method, item.path]), [['POST', '/api/items'], ['GET', '/api/items']]);
  assert.equal(slice(source, result.clients[0].site), 'fetch("/api/items", { method: "POST" })');
  assert.deepEqual(run(source), result);
});

test('C056 keeps computed routes, nested routes and shadowed fetch unresolved', () => {
  const source = [
    'import { Route as Entry, Routes } from "react-router-dom";',
    'import { Page } from "./page";',
    'const path = "/api/items";',
    'function Route() { return <main />; }',
    'export const router = <><Route /><Routes><Entry path={path} element={<Page />} />',
    '<Entry path="/outer" element={<Page />}><Entry path="/inner" element={<Page />} /></Entry>',
    '</Routes></>;',
    'export function missing() { return fetch(path); }',
    'export function local() { const fetch = (value: string) => value; return fetch("/api/items"); }',
  ].join('\n');
  const result = run(source);
  assert.deepEqual(result.routes, []);
  assert.deepEqual(result.clients, []);
  assert.deepEqual(result.gaps, ['FRONTEND_CLIENT_UNRESOLVED', 'FRONTEND_ROUTE_UNRESOLVED']);
});
