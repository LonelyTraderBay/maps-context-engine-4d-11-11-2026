import type { PreviewSelection } from '../features/preview/pairing.ts';
import type { SourceRef } from '../../.generated/types';

export type DetailView = 'source' | 'details';

type Choice = { node: string; source: number; preview?: PreviewSelection; mappingSource?: SourceRef };

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

export type SelectionAction = { type: 'scope'; digest: string | null } |
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
        { ...initialSelection, scope: action.digest, compactPane: state.compactPane };
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
