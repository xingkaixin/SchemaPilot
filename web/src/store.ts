import { create } from "zustand";
import type { Run, Workspace } from "./api";
import { allPaths, normalize, removeFiles, serial, type Step } from "./lib/arrangement";
import { emptyConnection, type ConnectionState, type RunMode } from "./lib/model";

interface Persisted {
  connections: Record<string, ConnectionState>;
  /** Files from a connection directory that the user moved out of it. */
  detached: string[];
  selected?: string;
}

interface State extends Persisted {
  storageKey?: string;
  workspace?: Workspace;
  selectedConnection?: string;
  selectedFile?: string;
  checked: string[];

  sync: (workspace: Workspace) => void;
  select: (connection?: string) => void;
  selectFile: (path?: string) => void;
  setChecked: (paths: string[]) => void;
  assign: (paths: string[], connection: string) => void;
  unassign: (connection: string, path: string) => void;
  arrange: (connection: string, change: (steps: Step[]) => Step[]) => void;
  setDisabled: (connection: string, paths: string[], disabled: boolean) => void;
  clear: (connection: string) => void;
  prepareRun: (connection: string, mode: RunMode, paths: string[]) => void;
  applyRun: (connection: string, run: Run) => void;
  markInterrupted: (connection: string) => void;
  renameConnection: (previous: string, next: string) => void;
  removeConnection: (name: string) => void;
}

function load(key: string): Persisted {
  try {
    const raw = localStorage.getItem(key);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<Persisted>;
      return {
        connections: parsed.connections ?? {},
        detached: parsed.detached ?? [],
        selected: parsed.selected,
      };
    }
  } catch {
    // Unreadable storage falls back to an empty arrangement.
  }
  return { connections: {}, detached: [] };
}

function reconcile(state: Persisted, workspace: Workspace): Persisted {
  const names = workspace.connections.map((connection) => connection.name);
  const connections: Record<string, ConnectionState> = {};
  for (const [name, value] of Object.entries(state.connections)) {
    // A config that failed to parse lists no connections; keep their state.
    if (names.includes(name) || workspace.configError) connections[name] = value;
  }
  for (const name of names) connections[name] ??= emptyConnection();

  const arranged = new Set(Object.values(connections).flatMap((value) => allPaths(value.steps)));
  for (const name of names) {
    const fresh = workspace.files
      .filter((file) => file.connection === name && !arranged.has(file.path))
      .filter((file) => !state.detached.includes(file.path))
      .map((file) => file.path);
    if (fresh.length > 0) {
      connections[name] = {
        ...connections[name],
        steps: [...connections[name].steps, ...serial(fresh)],
      };
    }
  }
  return { connections, detached: state.detached };
}

// A run the server no longer knows about (it restarted) can never finish.
function interrupt(state: ConnectionState): ConnectionState {
  if (state.lastRun?.status !== "running") return state;
  const results = { ...state.results };
  for (const [path, result] of Object.entries(results)) {
    if (result.status === "running") {
      results[path] = { ...result, status: "cancelled", message: "服务已重启，执行被中断" };
    }
  }
  return { ...state, results, lastRun: { ...state.lastRun, status: "cancelled" } };
}

function without(state: ConnectionState, paths: string[]): ConnectionState {
  const results = { ...state.results };
  for (const path of paths) delete results[path];
  return {
    ...state,
    steps: normalize(removeFiles(state.steps, paths)),
    disabled: state.disabled.filter((path) => !paths.includes(path)),
    results,
  };
}

export const useStore = create<State>()((set, get) => {
  const update = (connection: string, change: (state: ConnectionState) => ConnectionState) =>
    set((state) => {
      const current = state.connections[connection];
      if (!current) return {};
      return { connections: { ...state.connections, [connection]: change(current) } };
    });

  return {
    connections: {},
    detached: [],
    checked: [],

    sync: (workspace) =>
      set((state) => {
        const storageKey = `schemapilot:${workspace.root}`;
        const base = state.storageKey === storageKey ? state : load(storageKey);
        const next = reconcile(base, workspace);
        const names = workspace.connections.map((connection) => connection.name);
        const preferred =
          state.storageKey === storageKey ? state.selectedConnection : base.selected;
        const selectedConnection = preferred && names.includes(preferred) ? preferred : names[0];
        const existing = new Set(workspace.files.map((file) => file.path));
        return {
          ...next,
          storageKey,
          workspace,
          selectedConnection,
          checked: state.checked.filter((path) => existing.has(path)),
        };
      }),

    select: (connection) => set({ selectedConnection: connection, selectedFile: undefined }),
    selectFile: (path) => set({ selectedFile: path }),
    setChecked: (paths) => set({ checked: paths }),

    assign: (paths, connection) =>
      set((state) => {
        const connections: Record<string, ConnectionState> = {};
        for (const [name, value] of Object.entries(state.connections)) {
          connections[name] = without(value, paths);
        }
        const target = connections[connection] ?? emptyConnection();
        connections[connection] = { ...target, steps: [...target.steps, ...serial(paths)] };
        return {
          connections,
          detached: state.detached.filter((path) => !paths.includes(path)),
          checked: state.checked.filter((path) => !paths.includes(path)),
        };
      }),

    unassign: (connection, path) => {
      update(connection, (state) => without(state, [path]));
      const fromDirectory = get().workspace?.files.some(
        (file) => file.path === path && file.connection,
      );
      if (fromDirectory) set((state) => ({ detached: [...state.detached, path] }));
      if (get().selectedFile === path) set({ selectedFile: undefined });
    },

    arrange: (connection, change) =>
      update(connection, (state) => ({ ...state, steps: normalize(change(state.steps)) })),

    setDisabled: (connection, paths, disabled) =>
      update(connection, (state) => ({
        ...state,
        disabled: disabled
          ? [...new Set([...state.disabled, ...paths])]
          : state.disabled.filter((path) => !paths.includes(path)),
      })),

    clear: (connection) => {
      const own =
        get()
          .workspace?.files.filter((file) => file.connection === connection)
          .map((file) => file.path) ?? [];
      set((state) => ({
        connections: {
          ...state.connections,
          [connection]: { ...emptyConnection(), resetAt: new Date().toISOString() },
        },
        detached: state.detached.filter((path) => !own.includes(path)),
      }));
      const workspace = get().workspace;
      if (workspace) get().sync(workspace);
    },

    prepareRun: (connection, mode, paths) =>
      update(connection, (state) => {
        const results = mode === "all" ? {} : { ...state.results };
        for (const path of paths) delete results[path];
        return { ...state, results, resetAt: new Date().toISOString() };
      }),

    applyRun: (connection, run) =>
      update(connection, (state) => {
        const known = state.lastRun?.id === run.id;
        if (!known && state.resetAt && Date.parse(run.startedAt) < Date.parse(state.resetAt)) {
          return interrupt(state);
        }
        const results = { ...state.results };
        for (const file of run.files) results[file.path] = file;
        const { id, status, startedAt, finishedAt, error } = run;
        return { ...state, results, lastRun: { id, status, startedAt, finishedAt, error } };
      }),

    markInterrupted: (connection) => update(connection, interrupt),

    renameConnection: (previous, next) =>
      set((state) => {
        if (previous === next || !state.connections[previous]) return {};
        const connections = { ...state.connections, [next]: state.connections[previous] };
        delete connections[previous];
        return {
          connections,
          selectedConnection:
            state.selectedConnection === previous ? next : state.selectedConnection,
        };
      }),

    removeConnection: (name) =>
      set((state) => {
        const connections = { ...state.connections };
        delete connections[name];
        return {
          connections,
          selectedConnection:
            state.selectedConnection === name ? undefined : state.selectedConnection,
        };
      }),
  };
});

let saveTimer: ReturnType<typeof setTimeout> | undefined;
useStore.subscribe((state, previous) => {
  if (!state.storageKey) return;
  if (
    state.connections === previous.connections &&
    state.detached === previous.detached &&
    state.selectedConnection === previous.selectedConnection
  ) {
    return;
  }
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    try {
      const { connections, detached, selectedConnection } = useStore.getState();
      const persisted: Persisted = { connections, detached, selected: selectedConnection };
      localStorage.setItem(state.storageKey!, JSON.stringify(persisted));
    } catch {
      // Storage may be full or blocked; the arrangement then lives in memory only.
    }
  }, 300);
});
