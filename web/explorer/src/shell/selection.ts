export type DetailView = 'source' | 'details';

type Choice = { node: string; source: number };

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

type Action = { type: 'scope'; digest: string | null } |
  { type: 'choose'; node: string } | { type: 'range'; index: number } |
  { type: 'history'; delta: -1 | 1 } | { type: 'clear' } |
  { type: 'detail'; value: DetailView } | { type: 'pane'; value: Selection['compactPane'] };

export function selectionReducer(state: Selection, action: Action): Selection {
  const current = state.history[state.position];
  switch (action.type) {
    case 'scope':
      return state.scope === action.digest ? state :
        { ...initialSelection, scope: action.digest, compactPane: state.compactPane };
    case 'choose': {
      if (current?.node === action.node) return state;
      const history = [...state.history.slice(0, state.position + 1), { node: action.node, source: 0 }].slice(-64);
      return { ...state, history, position: history.length - 1 };
    }
    case 'range':
      if (!current || current.source === action.index) return state;
      return { ...state, detail: 'source',
        history: state.history.map((value, index) => index === state.position ? { ...value, source: action.index } : value) };
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
