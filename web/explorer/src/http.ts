import validate from '../.generated/validators.cjs';
import { http } from '../.generated/wire.ts';
import type * as S43 from '../.generated/types';
import { parseJSON } from './json.ts';
import { canonicalOrigin, checkPathname, checkRelativePath, checkSourceRef } from './paths.ts';
import { checkPreviewStatus } from './preview.ts';
import { checkScopeVector, scopeSelection } from './scope.ts';

type Registry = typeof http;
export type HTTPRoute = keyof Registry;
export type HTTPPostRoute = Exclude<HTTPRoute, 'GET /workspace'>;
export type HTTPRequest<K extends HTTPRoute> = (typeof requestTypes)[Registry[K]['request']];
type HTTPData<K extends HTTPRoute> = Omit<(typeof responseTypes)[Registry[K]['data']], 'operation'> &
  { operation: Registry[K]['label'] };
export type HTTPResponse<K extends HTTPRoute> = Omit<S43.Response, 'data'> & { data: HTTPData<K> | null };

type Validated<F> = F extends ((value: unknown) => value is infer Result) ? Result : never;
declare const requestTypes: { [Name in Registry[HTTPRoute]['request']]: Validated<(typeof validate)[Name]> };
declare const responseTypes: { [Name in Registry[HTTPRoute]['data']]: Validated<(typeof validate)[Name]> };

export const explorerBasePath = '/api/explorer/v1';

function routeContract<K extends HTTPRoute>(route: K): Registry[K] {
  if (!Object.hasOwn(http, route)) throw new Error('Unregistered Explorer route');
  return http[route];
}

export function httpLimits(route: HTTPRoute): Readonly<{ requestBytes: number; responseBytes: number }> {
  const { requestBytes, responseBytes } = routeContract(route);
  return { requestBytes, responseBytes };
}

export function decodeWorkspaceQuery(query: URLSearchParams, bodyLength: number): S43.StatusRequest {
  if (bodyLength !== 0) throw new Error('GET /workspace does not take a body');
  const pairs = [...query.entries()];
  if (pairs.length === 0) return {};
  const first = pairs[0]!;
  if (pairs.length !== 1 || first[0] !== 'verify' || (first[1] !== 'none' && first[1] !== 'workspace')) {
    throw new Error('Invalid workspace query');
  }
  return { verify: first[1] };
}

function graphRef(ref: S43.GraphRef): void {
  if (ref.kind === 'file') checkRelativePath(ref.path);
}

function mappingRef(ref: S43.GraphRef | S43.DOMAnchorRef, buildMap: string): void {
  if ('kind' in ref) graphRef(ref);
  if ('build_map_id' in ref && ref.build_map_id !== buildMap) throw new Error('Mapping build changed');
}

export function decodeHTTPRequest<K extends HTTPPostRoute>(
  route: K, bytes: Uint8Array, explicitBindings: number,
): HTTPRequest<K> {
  const entry = routeContract(route);
  if (route === ('GET /workspace' as HTTPRoute)) throw new Error('Use the workspace query decoder');
  const value = parseJSON(bytes, entry.requestBytes, true);
  const check = validate[entry.request];
  if (!check(value)) throw new Error('Invalid Explorer request');
  if ('scope' in value) scopeSelection(value.scope ?? {}, explicitBindings);
  else if (entry.request === 'GraphQueryRequest' || entry.request === 'FlowQueryRequest' ||
      entry.request === 'PackRequest' || entry.request === 'MappingResolveRequest' ||
      entry.request === 'PreviewAttachRequest') scopeSelection({}, explicitBindings);
  if ('root' in value && value.root.kind === 'ref') graphRef(value.root.ref);
  if ('selector' in value && value.selector.kind === 'ref') graphRef(value.selector.ref);
  if ('source_ref' in value) checkSourceRef(value.source_ref);
  if ('root_ref' in value) {
    graphRef(value.root_ref);
    if ((value.mode === 'observed') !== (value.artifact_id !== undefined)) {
      throw new Error('Observed flows require an artifact');
    }
  }
  if (entry.request === 'MappingResolveRequest' && validate.MappingResolveRequest(value)) {
    mappingRef(value.ref, value.build_map_id);
  }
  if ('origin' in value && 'path' in value) {
    canonicalOrigin(value.origin);
    checkPathname(value.path);
  }
  // The generated validator chosen by K was applied above.
  return value as HTTPRequest<K>;
}

export function checkPagination(page: S43.Pagination): void {
  const { required_pending: required, optional_pending: optional, enumeration_complete: complete } = page;
  const pending = required === null || optional === null || required > 0 || optional > 0 || !complete;
  if (page.truncated !== pending || (!complete && required === 0 && optional === 0) ||
      (!pending && page.next_cursor !== null)) {
    throw new Error('Inconsistent pagination');
  }
}

function checkEdges(ids: Set<string>, count: number, edges: Array<[string, string, string]>): void {
  if (ids.size !== count || new Set(edges.map(([id]) => id)).size !== edges.length ||
      edges.some(([, from, to]) => !ids.has(from) || !ids.has(to))) {
    throw new Error('Invalid graph or flow slice');
  }
}

function checkData(data: S43.ResponseData, page: S43.Pagination): void {
  switch (data.operation) {
    case 'preview_status':
      checkPreviewStatus(data);
      break;
    case 'source_read':
      checkSourceRef(data.source_ref);
      break;
    case 'graph_query':
      checkEdges(new Set(data.nodes.map(node => node.node_id)), data.nodes.length,
        data.edges.map(edge => [edge.edge_id, edge.from_node_id, edge.to_node_id]));
      for (const node of data.nodes) {
        graphRef(node.ref);
        node.source_refs.forEach(checkSourceRef);
      }
      break;
    case 'flow_query':
      checkEdges(new Set(data.steps.map(step => step.step_id)), data.steps.length,
        data.edges.map(edge => [edge.edge_id, edge.from_step_id, edge.to_step_id]));
      for (const step of data.steps) {
        step.canonical_refs.forEach(graphRef);
        step.source_refs.forEach(checkSourceRef);
      }
      for (const edge of data.edges) if (edge.condition_source_ref) checkSourceRef(edge.condition_source_ref);
      break;
    case 'mapping_resolve': {
      mappingRef(data.requested_ref, data.build_map_id);
      const known = new Set<string>();
      for (const match of data.matches) {
        if (known.has(match.match_id) || match.anchor_ref.build_map_id !== data.build_map_id ||
            match.anchor_ref.binding_id !== data.binding_id) throw new Error('Inconsistent mapping match');
        known.add(match.match_id);
        if (match.target_ref) graphRef(match.target_ref);
        match.source_refs.forEach(checkSourceRef);
      }
      if ((page.truncated && page.next_cursor === null) ||
          (data.mapping_status === 'exact_in_supported_scope' && (page.truncated || data.matches.length > 1))) {
        throw new Error('Incomplete mapping result');
      }
      break;
    }
  }
}

export function decodeHTTPResponse<K extends HTTPRoute>(route: K, bytes: Uint8Array): HTTPResponse<K> {
  const entry = routeContract(route);
  const value = parseJSON(bytes, entry.responseBytes, true);
  if (!validate.Response(value)) throw new Error('Invalid Explorer response');
  if (value.scope) checkScopeVector(value.scope);
  checkPagination(value.pagination);
  const failed = value.status === 'blocked' || value.status === 'error';
  if ((failed && (value.error === null || value.readiness !== 'blocked' || value.data !== null)) ||
      (value.status === 'ok' && value.error !== null)) {
    throw new Error('Inconsistent response status');
  }
  const budget = value.budget;
  if (budget.delivery_mode !== 'http' || budget.max_output_bytes > entry.responseBytes ||
      budget.serialized_bytes > budget.max_output_bytes || bytes.length > budget.max_output_bytes ||
      (budget.token_count === null) !== (budget.tokenizer_id === null) ||
      (budget.token_estimate !== null && !budget.estimate_only)) {
    throw new Error('Invalid HTTP response budget');
  }
  if (entry.label === 'status' && value.freshness.scope === 'returned_files') throw new Error('Invalid status freshness');
  if (entry.label === 'bootstrap' && (value.scope !== null || value.freshness.status !== 'not_checked' ||
      value.coverage.status !== 'unknown' || value.readiness !== 'blocked')) throw new Error('Invalid bootstrap context');
  if (value.data !== null) {
    if (value.data.operation !== entry.label || !validate[entry.data](value.data)) throw new Error('HTTP operation mismatch');
    checkData(value.data, value.pagination);
  }
  // The closed request label and data validator for K were checked above.
  return value as HTTPResponse<K>;
}
