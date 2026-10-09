import { create } from "zustand";
import { api, ApiError, type Arrangement, type Run, type Workspace } from "./api";
import { allPaths, normalize, arrangeByName, removeFiles, type Step } from "./lib/arrangement";
import { emptyConnection, type ConnectionState, type RunMode } from "./lib/model";
import { queryClient } from "./queryClient";
import { notifyError } from "./toasts";

/**
 * The arrangement (steps, disabled files, detached files) lives in the
 * workspace's arrangement file; run results stay in this browser.
 */
interface Persisted {
  connections: Record<string, ConnectionState>;
  /** Files from a connection directory that the user moved out of it. */
  detached: string[];
  selected?: string;
}

interface State extends Persisted {
  storageKey?: string;
  /** Revision of the arrangement file this state was read from or last saved as. */
  arrangementRevision?: string;
  /** Connections whose configured driver the user accepted over the arranged one. */
  acceptedDrivers: string[];
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
  acceptDriver: (name: string) => void;
}

/** Older versions also kept the arrangement here; it seeds a workspace without a file. */
function load(key: string): Persisted {
  try {
    const raw = localStorage.getItem(key);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<Persisted>;
      const connections: Record<string, ConnectionState> = {};
      for (const [name, value] of Object.entries(parsed.connections ?? {})) {
        connections[name] = { ...emptyConnection(), ...value };
      }
      return { connections, detached: parsed.detached ?? [], selected: parsed.selected };
    }
  } catch {
    // Unreadable storage falls back to an empty state.
  }
  return { connections: {}, detached: [] };
}

function applyArrangement(state: Persisted, arrangement: Arrangement): Persisted {
  const connections: Record<string, ConnectionState> = {};
  const names = new Set([
    ...Object.keys(state.connections),
    ...Object.keys(arrangement.connections),
  ]);
  for (const name of names) {
    const arranged = arrangement.connections[name];
    connections[name] = {
      ...(state.connections[name] ?? emptyConnection()),
      steps: arranged?.steps ?? [],
      disabled: arranged?.disabled ?? [],
    };
  }
  return { ...state, connections, detached: arrangement.detached ?? [] };
}

/** Connections that own a directory: configured here or arranged in the file. */
function connectionNames(workspace: Workspace) {
  const names = workspace.connections.map((connection) => connection.name);
  for (const name of Object.keys(workspace.arrangement?.connections ?? {})) {
    if (!names.includes(name)) names.push(name);
  }
  return names;
}

function reconcile(state: Persisted, workspace: Workspace): Persisted {
  const names = connectionNames(workspace);
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
        steps: [...connections[name].steps, ...arrangeByName(fresh)],
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
    acceptedDrivers: [],

    sync: (workspace) =>
      set((state) => {
        const storageKey = `schemapilot:${workspace.root}`;
        const reopened = state.storageKey !== storageKey;
        let base: Persisted = reopened ? load(storageKey) : state;
        let arrangementRevision = reopened ? undefined : state.arrangementRevision;
        const changedElsewhere =
          workspace.arrangementRevision !== arrangementRevision &&
          !seenRevisions.has(workspace.arrangementRevision);
        if (!workspace.arrangementError && (reopened || changedElsewhere)) {
          if (workspace.arrangement) base = applyArrangement(base, workspace.arrangement);
          arrangementRevision = workspace.arrangementRevision;
          seenRevisions.add(arrangementRevision);
          lastSaved = undefined;
        }
        const next = reconcile(base, workspace);
        const names = workspace.connections.map((connection) => connection.name);
        const preferred =
          state.storageKey === storageKey ? state.selectedConnection : base.selected;
        const selectedConnection = preferred && names.includes(preferred) ? preferred : names[0];
        const existing = new Set(workspace.files.map((file) => file.path));
        return {
          ...next,
          storageKey,
          arrangementRevision,
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
        connections[connection] = { ...target, steps: [...target.steps, ...arrangeByName(paths)] };
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
        const { id, status, startedAt, finishedAt, error, version } = run;
        return {
          ...state,
          results,
          lastRun: { id, status, startedAt, finishedAt, error, version },
        };
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

    acceptDriver: (name) => {
      set((state) => ({ acceptedDrivers: [...state.acceptedDrivers, name] }));
      void saveArrangement();
    },

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

/** Revisions this tab has read or written; an older response carrying one is stale. */
const seenRevisions = new Set<string>();
/** The arrangement as last read from or written to the file, serialized. */
let lastSaved: string | undefined;

function toArrangement(state: State): Arrangement {
  const workspace = state.workspace!;
  const connections: Arrangement["connections"] = {};
  for (const [name, value] of sortedEntries(state.connections)) {
    if (value.steps.length === 0 && value.disabled.length === 0) continue;
    // Keep the driver the files were arranged for until the user accepts a
    // differently configured one; a new arrangement takes the configured one.
    const configured = workspace.connections.find((connection) => connection.name === name)?.driver;
    const arranged = workspace.arrangement?.connections[name]?.driver;
    const driver = state.acceptedDrivers.includes(name) ? configured : (arranged ?? configured);
    connections[name] = { driver, steps: value.steps, disabled: value.disabled };
  }
  return { version: 1, connections, detached: state.detached };
}

let storageTimer: ReturnType<typeof setTimeout> | undefined;
let arrangementTimer: ReturnType<typeof setTimeout> | undefined;

useStore.subscribe((state, previous) => {
  if (!state.storageKey || !state.workspace) return;
  if (
    state.connections !== previous.connections ||
    state.selectedConnection !== previous.selectedConnection
  ) {
    clearTimeout(storageTimer);
    storageTimer = setTimeout(() => {
      try {
        const { connections, selectedConnection } = useStore.getState();
        const results: Persisted["connections"] = {};
        for (const [name, value] of Object.entries(connections)) {
          results[name] = { ...value, steps: [], disabled: [] };
        }
        const persisted: Persisted = {
          connections: results,
          detached: [],
          selected: selectedConnection,
        };
        localStorage.setItem(state.storageKey!, JSON.stringify(persisted));
      } catch {
        // Storage may be full or blocked; results then live in memory only.
      }
    }, 300);
  }
  if (state.connections !== previous.connections || state.detached !== previous.detached) {
    clearTimeout(arrangementTimer);
    arrangementTimer = setTimeout(() => void saveArrangement(), 300);
  }
});

async function saveArrangement() {
  const state = useStore.getState();
  const workspace = state.workspace;
  if (!workspace || workspace.arrangementError) return;
  const arrangement = toArrangement(state);
  const serialized = JSON.stringify(arrangement);
  if (lastSaved === undefined && !workspace.arrangement) {
    // Without a file, only create one once there is something to keep.
    if (Object.keys(arrangement.connections).length === 0 && arrangement.detached?.length === 0)
      return;
  }
  if (serialized === lastSaved || (lastSaved === undefined && sameAsFile(arrangement, workspace))) {
    lastSaved = serialized;
    return;
  }
  try {
    const { revision } = await api.saveArrangement(arrangement, state.arrangementRevision ?? "");
    seenRevisions.add(revision);
    lastSaved = serialized;
    useStore.setState({ arrangementRevision: revision });
  } catch (error) {
    if (error instanceof ApiError && error.status === 409) {
      // Someone else changed the file: take theirs instead of overwriting it.
      notifyError("编排已在别处修改，已重新载入", error);
      useStore.setState({ arrangementRevision: undefined });
      seenRevisions.clear();
      await queryClient.invalidateQueries({ queryKey: ["workspace"] });
    } else {
      notifyError("保存编排失败", error);
    }
  }
}

function sameAsFile(arrangement: Arrangement, workspace: Workspace) {
  return (
    workspace.arrangement !== null &&
    JSON.stringify(arrangement) === JSON.stringify(toComparable(workspace.arrangement))
  );
}

/** The file as toArrangement would write it, for comparison. */
function toComparable(arrangement: Arrangement): Arrangement {
  const connections: Arrangement["connections"] = {};
  for (const [name, value] of sortedEntries(arrangement.connections)) {
    connections[name] = {
      driver: value.driver,
      steps: value.steps,
      disabled: value.disabled ?? [],
    };
  }
  return { version: 1, connections, detached: arrangement.detached ?? [] };
}

function sortedEntries<T>(record: Record<string, T>) {
  return Object.entries(record).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
}
