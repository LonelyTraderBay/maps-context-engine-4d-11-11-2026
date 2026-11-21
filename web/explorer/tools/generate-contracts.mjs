import { mkdir, readFile, writeFile } from 'node:fs/promises';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';
import standalone from 'ajv/dist/standalone/index.js';

const contract = new URL('../../../contracts/', import.meta.url);
const load = async name => JSON.parse(await readFile(new URL(name, contract), 'utf8'));
const [schema, operations, wire] = await Promise.all([
  load('mce4d.v1.schema.json'),
  load('operations.v1.json'),
  load('explorer-wire.v1.json'),
]);
const generated = new URL('../.generated/', import.meta.url);
const banner = '// Generated from the S43 schema and wire registries.\n';
const refs = path => {
  if (!/^#\/\$defs\/[A-Za-z][A-Za-z0-9_]*$/.test(path)) throw new Error('Unexpected schema reference');
  return path.split('/').at(-1);
};

// The declarations describe structural types. Runtime validators enforce
// exclusivity, bounds, formats and negative constraints.
function toType(node) {
  if (node.$ref) return refs(node.$ref);
  if (Object.hasOwn(node, 'const')) return JSON.stringify(node.const);
  if (node.enum) return node.enum.map(value => JSON.stringify(value)).join(' | ');
  const alternatives = node.oneOf ?? node.anyOf;
  if (alternatives) return alternatives.map(toType).join(' | ');
  switch (node.type) {
    case 'integer': return 'number';
    case 'number':
    case 'string':
    case 'boolean':
    case 'null': return node.type;
    case 'array': return `Array<${toType(node.items)}>`;
    case 'object': {
      if (node.additionalProperties !== false) throw new Error('Expected closed object type');
      const required = new Set(node.required ?? []);
      const fields = Object.entries(node.properties ?? {}).map(([key, spec]) =>
        `${JSON.stringify(key)}${required.has(key) ? '' : '?'}: ${toType(spec)}`,
      );
      return fields.length === 0 ? 'Record<string, never>' : `{ ${fields.join('; ')} }`;
    }
    default: throw new Error('Unrecognized schema declaration');
  }
}

const http = {};
const names = new Set([
  'BridgeMessage', 'Response', 'BuildManifest', 'AnchorEntry',
  'PreviewPermissionProfile', 'ScopeVector', 'GraphRef', 'SourceRef',
]);
for (const [route, [request, data]] of Object.entries(operations)) {
  if (!/^(GET|POST) \//.test(route)) continue;
  const [label, requestBytes, responseBytes] = wire.http[route];
  const operation = schema.$defs[data].properties.operation;
  if (operation.const !== label && !operation.enum?.includes(label)) {
    throw new Error(`Response label disagrees with S43 for ${route}`);
  }
  http[route] = { request, data, label, requestBytes, responseBytes };
  names.add(request);
  names.add(data);
}
if (Object.keys(http).length !== Object.keys(wire.http).length) throw new Error('HTTP registry disagrees');

const bridgeTypes = schema.$defs.BridgeMessage.oneOf.map(({ $ref }) =>
  schema.$defs[refs($ref)].properties.type.const,
);
const registered = new Set(Object.values(wire.bridge).flat());
if (registered.size !== bridgeTypes.length || bridgeTypes.some(name => !registered.has(name))) {
  throw new Error('Bridge registry disagrees');
}

const ajv = new Ajv2020({ code: { source: true, lines: true }, ownProperties: true, inlineRefs: false });
addFormats(ajv, ['date-time']);
ajv.addSchema(schema);
const exports = Object.fromEntries([...names].map(name => [name, `${schema.$id}#/$defs/${name}`]));
const reachable = new Set(names);
function collectRefs(node) {
  if (node.$ref) reachable.add(refs(node.$ref));
  for (const value of Object.values(node)) if (value && typeof value === 'object') collectRefs(value);
}
// Set iteration also visits entries added by collectRefs.
for (const name of reachable) collectRefs(schema.$defs[name]);
const definitions = [...reachable].map(name => `export type ${name} = ${toType(schema.$defs[name])};\n`);
const validators = [...names].map(name => `  ${name}(value: unknown): value is S43.${name};\n`);
await mkdir(generated, { recursive: true });
await Promise.all([
  writeFile(new URL('types.d.ts', generated), banner + definitions.join('')),
  writeFile(new URL('validators.cjs', generated), banner + standalone(ajv, exports)),
  writeFile(new URL('validators.d.cts', generated), banner +
    `import type * as S43 from './types';\ndeclare const validate: {\n${validators.join('')}};\nexport = validate;\n`),
  writeFile(new URL('wire.ts', generated), banner +
    `export const http = ${JSON.stringify(http, null, 2)} as const;\n` +
    `export const bridgeDirections = ${JSON.stringify(wire.bridge, null, 2)} as const;\n`),
]);
