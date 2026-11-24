import type { PreviewSelection } from '../features/preview/pairing.ts';
import type { GraphNode, ScopeVector, SourceRef } from '../../.generated/types';

export type DetailView = 'source' | 'details';

type GraphChoice = { node: string; source: number };
type Choice = GraphChoice & { preview?: PreviewSelection; mappingSource?: SourceRef };

export type Selection = {
  scope: string | null;
  history: Choice[];
  position: number;
  detail: DetailView;
  compactPane: 'graph' | 'right';
};

export const initialSelection: Selection = {
  scope: null, history: [], position: -1, detail: 'source', compactPane: 'graph',
};

export type SelectionAction = { type: 'scope'; digest: string | null; retain?: GraphChoice } |
  { type: 'choose'; node: string } | { type: 'range'; index: number } |
  { type: 'preview'; node: string; value: PreviewSelection } |
  { type: 'mapping-range'; node: string; source: SourceRef } |
  { type: 'history'; delta: -1 | 1 } | { type: 'clear' } |
  { type: 'detail'; value: DetailView } | { type: 'pane'; value: Selection['compactPane'] };

export function selectionReducer(state: Selection, action: SelectionAction): Selection {
  const current = state.history[state.position];
  switch (action.type) {
    case 'scope':
      return state.scope === action.digest ? state :
        { ...initialSelection, scope: action.digest, compactPane: state.compactPane,
          ...(action.retain ? { history: [action.retain], position: 0, detail: state.detail } : {}) };
    case 'choose': {
      if (current?.node === action.node && !current.preview && !current.mappingSource) return state;
      const history = [...state.history.slice(0, state.position + 1), { node: action.node, source: 0 }].slice(-64);
      return { ...state, history, position: history.length - 1 };
    }
    case 'preview': {
      if (state.scope !== action.value.scopeDigest || current?.preview?.selectionID === action.value.selectionID) return state;
      const next = { node: action.node, source: 0, preview: action.value };
      const history = [...state.history.slice(0, state.position + 1), next].slice(-64);
      return { ...state, history, position: history.length - 1, detail: 'source' };
    }
    case 'mapping-range': {
      if (!current || current.node !== action.node) return state;
      const next = { node: current.node, source: current.source, mappingSource: action.source };
      return { ...state, detail: 'source',
        history: state.history.map((value, index) => index === state.position ? next : value) };
    }
    case 'range':
      if (!current || current.source === action.index && !current.preview && !current.mappingSource) return state;
      return { ...state, detail: 'source',
        history: state.history.map((value, index) => index === state.position ? { node: value.node, source: action.index } : value) };
    case 'history': {
      const next = state.position + action.delta;
      return next < 0 || next >= state.history.length ? state : { ...state, position: next };
    }
    case 'clear':
      return { ...state, position: -1, history: [] };
    case 'detail':
      return { ...state, detail: action.value };
    case 'pane':
      return { ...state, compactPane: action.value };
  }
}

export function retainGraphChoice(prior: ScopeVector | null, next: ScopeVector | null, choice: Choice | undefined,
  node: GraphNode | null, nodes: readonly GraphNode[]): GraphChoice | undefined {
  if (!choice || choice.preview || choice.mappingSource || node?.ref.kind !== 'symbol' ||
      prior?.kind !== 'single' || next?.kind !== 'single' || prior.catalog_incarnation !== next.catalog_incarnation) return;
  const [from] = prior.members;
  const [to] = next.members;
  if (!from || !to || from.generation_id === to.generation_id || from.worktree_id !== to.worktree_id ||
      from.profile_id !== to.profile_id ||
      from.index_incarnation !== to.index_incarnation || from.records_incarnation !== to.records_incarnation ||
      from.policy_digest !== to.policy_digest || from.policy_epoch !== to.policy_epoch ||
      from.binding_revision !== to.binding_revision || from.record_epoch !== to.record_epoch ||
      from.service_id !== to.service_id) return;

  const ref = node.ref;
  const source = node.source_refs[choice.source];
  if (!source) return;
  const matches = nodes.filter(item => item.ref.kind === 'symbol' && item.ref.symbol_id === ref.symbol_id &&
    item.label === node.label && item.kind === node.kind);
  const [match] = matches;
  if (!match || matches.length !== 1) return;
  const location = match.source_refs.map((value, index) => ({ value, index })).filter(({ value }) =>
    value.path === source.path && value.worktree_id === to.worktree_id && value.profile_id === to.profile_id &&
    value.generation_id === to.generation_id);
  const [retained] = location;
  if (retained && location.length === 1) return { node: match.node_id, source: retained.index };
}
