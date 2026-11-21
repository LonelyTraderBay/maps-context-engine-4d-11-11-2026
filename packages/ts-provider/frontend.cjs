'use strict';

// The caller supplies a Program built from the captured TS/JS project and its
// pinned compiler libraries. This module only reads compiler objects.
function analyzeFrontend(ts, program, capturedPaths, offsets) {
  const checker = program.getTypeChecker();
  const routes = [];
  const clients = [];
  const gaps = new Set();
  const capture = new Set(capturedPaths);
  const sourceOffsets = new Map();
  const maxRecords = 4096;

  function add(items, item) {
    if (items.length >= maxRecords) throw new Error('FRONTEND_FACT_LIMIT');
    items.push(item);
  }
  function range(node) {
    const source = node.getSourceFile();
    if (!source.fileName.startsWith('/capture/')) throw new Error('FRONTEND_SOURCE_SCOPE');
    if (!sourceOffsets.has(source)) sourceOffsets.set(source, offsets(source.text));
    return { path: source.fileName.slice('/capture/'.length), ...sourceOffsets.get(source)(node) };
  }
  function namedImport(node, name) {
    if (!ts.isIdentifier(node)) return false;
    const symbol = checker.getSymbolAtLocation(node);
    if (symbol?.declarations?.length !== 1) return false;
    const declaration = symbol.declarations[0];
    if (!ts.isImportSpecifier(declaration) || declaration.isTypeOnly ||
        (declaration.propertyName ?? declaration.name).text !== name) return false;
    const statement = declaration.parent.parent.parent;
    return ts.isImportDeclaration(statement) && !statement.importClause?.isTypeOnly &&
      ts.isStringLiteral(statement.moduleSpecifier) && statement.moduleSpecifier.text === 'react-router-dom';
  }
  function jsxTag(node) {
    if (!node) return null;
    if (ts.isJsxElement(node)) return node.openingElement.tagName;
    if (ts.isJsxSelfClosingElement(node)) return node.tagName;
    return null;
  }
  function pathLiteral(text) {
    return text.length <= 1000 && /^\/(?:[a-zA-Z0-9_-]+(?:\/[a-zA-Z0-9_-]+)*)?$/.test(text);
  }
  function resolveComponent(element) {
    const tag = jsxTag(element);
    if (!tag || !ts.isIdentifier(tag) || !/^[A-Z]/.test(tag.text)) return null;
    let symbol = checker.getSymbolAtLocation(tag);
    if (symbol?.flags & ts.SymbolFlags.Alias) symbol = checker.getAliasedSymbol(symbol);
    if (symbol?.declarations?.length !== 1) return null;
    const declaration = symbol.declarations[0];
    if (!capture.has(declaration.getSourceFile().fileName)) return null;
    if (ts.isFunctionDeclaration(declaration) && declaration.body && declaration.name)
      return range(declaration.name);
    if (ts.isVariableDeclaration(declaration) && declaration.initializer &&
        (ts.isArrowFunction(declaration.initializer) || ts.isFunctionExpression(declaration.initializer)))
      return range(declaration.name);
    return null;
  }
  function readRoute(node) {
    if (!namedImport(node.tagName, 'Route')) return;
    const parent = ts.isJsxOpeningElement(node) ? node.parent.parent : node.parent;
    const attributes = node.attributes.properties;
    const props = new Map();
    for (const attr of attributes) {
      if (!ts.isJsxAttribute(attr) || !ts.isIdentifier(attr.name) || props.has(attr.name.text)) {
        gaps.add('FRONTEND_ROUTE_PROPERTIES_UNRESOLVED');
        return;
      }
      props.set(attr.name.text, attr.initializer);
    }
    const path = props.get('path');
    const element = props.get('element');
    const component = element && ts.isJsxExpression(element) ? resolveComponent(element.expression) : null;
    if (!parent || !ts.isJsxElement(parent) || !namedImport(parent.openingElement.tagName, 'Routes') ||
        [...props.keys()].some(name => !['path', 'element'].includes(name)) ||
        !path || !ts.isStringLiteral(path) || !pathLiteral(path.text) || !component ||
        ts.isJsxOpeningElement(node) && node.parent.children.some(child => !ts.isJsxText(child) || child.getText().trim() !== '')) {
      gaps.add('FRONTEND_ROUTE_UNRESOLVED');
      return;
    }
    add(routes, { path: path.text, site: range(path), component });
  }
  function readClient(node) {
    const identifier = node.expression;
    if (!ts.isIdentifier(identifier) || identifier.text !== 'fetch') return;
    const symbol = checker.getSymbolAtLocation(identifier);
    if (!symbol?.declarations?.length || symbol.declarations.some(decl =>
      !/^\/compiler\/lib\/lib\.(?:dom|webworker)\.d\.ts$/.test(decl.getSourceFile().fileName))) {
      gaps.add('FRONTEND_CLIENT_UNRESOLVED');
      return;
    }
    const [path, options] = node.arguments;
    let method = 'GET';
    if (!path || !ts.isStringLiteral(path) || !pathLiteral(path.text) || node.arguments.length > 2) {
      gaps.add('FRONTEND_CLIENT_UNRESOLVED');
      return;
    }
    if (options) {
      if (!ts.isObjectLiteralExpression(options) || options.properties.length !== 1) {
        gaps.add('FRONTEND_CLIENT_UNRESOLVED');
        return;
      }
      const property = options.properties[0];
      if (!ts.isPropertyAssignment(property) || property.name.getText() !== 'method' ||
          !ts.isStringLiteral(property.initializer) || !['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].includes(property.initializer.text)) {
        gaps.add('FRONTEND_CLIENT_UNRESOLVED');
        return;
      }
      method = property.initializer.text;
    }
    add(clients, { method, path: path.text, site: range(node) });
  }

  for (const source of program.getSourceFiles()) {
    if (!capture.has(source.fileName) || !/\.[cm]?[jt]sx?$/.test(source.fileName) || source.isDeclarationFile) continue;
    const pending = [source];
    while (pending.length) {
      const node = pending.pop();
      if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) readRoute(node);
      if (ts.isCallExpression(node)) readClient(node);
      const children = [];
      ts.forEachChild(node, child => { children.push(child); });
      for (let index = children.length - 1; index >= 0; index--) pending.push(children[index]);
    }
  }
  function compare(a, b) {
    if (a.site.path !== b.site.path) return a.site.path < b.site.path ? -1 : 1;
    return a.site.start_byte - b.site.start_byte;
  }
  return { routes: routes.sort(compare), clients: clients.sort(compare), gaps: [...gaps].sort() };
}

if (require.main !== module) module.exports = { analyzeFrontend };
