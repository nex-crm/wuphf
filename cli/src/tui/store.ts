/**
 * Central state management for the TUI.
 * Simple EventEmitter-based store -- no external state libs.
 */

// ── Types ──────────────────────────────────────────────────────────

export interface ViewEntry {
  name: string;
  props?: Record<string, unknown>;
}

export interface PickerItem {
  command: string;
  label: string;
  detail: string;
}

export interface NavState {
  objectSlug?: string;
  objectName?: string;
  recordId?: string;
  recordName?: string;
}

export interface SessionInfo {
  tokensUsed: number;
  costUsd: number;
  model: string;
  startTime: number;
}

export interface TuiState {
  mode: "normal" | "insert";
  viewStack: ViewEntry[];
  nav: NavState;
  pickerItems: PickerItem[] | null;
  pickerCursor: number;
  content: string;
  loading: boolean;
  loadingHint: string;
  inputValue: string;
  inputHistory: string[];
  historyIndex: number;
  scrollOffset: number;
  lastKey: string;
  lastKeyTime: number;
  session: SessionInfo | null;
}

// ── Actions ────────────────────────────────────────────────────────

export type Action =
  | { type: "PUSH_VIEW"; view: ViewEntry }
  | { type: "POP_VIEW" }
  | { type: "SET_MODE"; mode: TuiState["mode"] }
  | { type: "SET_CONTENT"; content: string }
  | { type: "SET_LOADING"; loading: boolean; hint?: string }
  | { type: "SET_INPUT"; value: string }
  | { type: "SET_PICKER"; items: PickerItem[] | null; cursor?: number }
  | { type: "NAVIGATE"; nav: Partial<NavState> }
  | { type: "SCROLL"; offset: number }
  | { type: "PUSH_HISTORY"; command: string }
  | { type: "SET_HISTORY_INDEX"; index: number }
  | { type: "SET_PICKER_CURSOR"; cursor: number }
  | { type: "SET_LAST_KEY"; key: string; time: number }
  | { type: "SET_SESSION"; session: Partial<SessionInfo> };

export type Dispatch = (action: Action) => void;
export type Listener = () => void;

// ── Constants ──────────────────────────────────────────────────────

const MAX_VIEW_STACK = 20;
const MAX_HISTORY = 100;

// ── Initial state ──────────────────────────────────────────────────

function initialState(): TuiState {
  return {
    mode: "normal",
    viewStack: [{ name: "home" }],
    nav: {},
    pickerItems: null,
    pickerCursor: 0,
    content: "",
    loading: false,
    loadingHint: "",
    inputValue: "",
    inputHistory: [],
    historyIndex: -1,
    scrollOffset: 0,
    lastKey: "",
    lastKeyTime: 0,
    session: null,
  };
}

// ── Reducer ────────────────────────────────────────────────────────

function reduce(state: TuiState, action: Action): TuiState {
  switch (action.type) {
    case "PUSH_VIEW": {
      const stack = [...state.viewStack, action.view];
      // Trim to max depth -- drop oldest (keep home at index 0)
      const trimmed =
        stack.length > MAX_VIEW_STACK
          ? [stack[0], ...stack.slice(stack.length - MAX_VIEW_STACK + 1)]
          : stack;
      return { ...state, viewStack: trimmed, scrollOffset: 0 };
    }

    case "POP_VIEW": {
      if (state.viewStack.length <= 1) return state; // never pop home
      return {
        ...state,
        viewStack: state.viewStack.slice(0, -1),
        scrollOffset: 0,
      };
    }

    case "SET_MODE":
      return { ...state, mode: action.mode };

    case "SET_CONTENT":
      return { ...state, content: action.content };

    case "SET_LOADING":
      return {
        ...state,
        loading: action.loading,
        loadingHint: action.hint ?? "",
      };

    case "SET_INPUT":
      return { ...state, inputValue: action.value };

    case "SET_PICKER":
      return {
        ...state,
        pickerItems: action.items,
        pickerCursor: action.cursor ?? 0,
      };

    case "NAVIGATE":
      return { ...state, nav: { ...state.nav, ...action.nav } };

    case "SCROLL":
      return { ...state, scrollOffset: Math.max(0, action.offset) };

    case "PUSH_HISTORY": {
      const history = [...state.inputHistory, action.command].slice(
        -MAX_HISTORY,
      );
      return { ...state, inputHistory: history, historyIndex: -1 };
    }

    case "SET_HISTORY_INDEX":
      return { ...state, historyIndex: action.index };

    case "SET_PICKER_CURSOR":
      return { ...state, pickerCursor: action.cursor };

    case "SET_LAST_KEY":
      return { ...state, lastKey: action.key, lastKeyTime: action.time };

    case "SET_SESSION": {
      const prev = state.session ?? {
        tokensUsed: 0,
        costUsd: 0,
        model: "",
        startTime: Date.now(),
      };
      return { ...state, session: { ...prev, ...action.session } };
    }

    default:
      return state;
  }
}

// ── Store factory ──────────────────────────────────────────────────

export interface Store {
  getState: () => TuiState;
  setState: (partial: Partial<TuiState>) => void;
  subscribe: (listener: Listener) => () => void;
  dispatch: Dispatch;
}

export function createStore(): Store {
  let state = initialState();
  const listeners = new Set<Listener>();

  function notify(): void {
    for (const fn of listeners) fn();
  }

  return {
    getState() {
      return state;
    },

    setState(partial) {
      state = { ...state, ...partial };
      notify();
    },

    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },

    dispatch(action) {
      state = reduce(state, action);
      notify();
    },
  };
}
