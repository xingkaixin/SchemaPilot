import { create } from "zustand";
import type {
  ActivePanel,
  ErrorPolicy,
  GraphDirection,
  MigrationGraph,
  MigrationNode,
} from "./types";

export type GraphDraft = MigrationGraph;
interface MigratorStore {
  graphDraft: GraphDraft | null;
  graphBaseline: GraphDraft | null;
  graphFingerprint?: string;
  databaseFingerprint?: string;
  selectedNodeId: string | null;
  selectedScriptPath: string | null;
  graphDirection: GraphDirection;
  activePanel: ActivePanel;
  consoleOpen: boolean;
  activeRunId: string | null;
  setGraphDraft: (graph: GraphDraft) => void;
  synchronizeGraph: (graph: GraphDraft, fingerprint?: string) => void;
  commitGraph: (graph: GraphDraft, fingerprint?: string) => void;
  setDatabaseFingerprint: (fingerprint?: string) => void;
  updateNode: (nodeName: string, update: Partial<MigrationNode>) => void;
  updateGraph: (update: Partial<GraphDraft>) => void;
  moveScript: (nodeName: string, fromIndex: number, toIndex: number) => void;
  addScript: (nodeName: string, path: string) => void;
  removeScript: (nodeName: string, path: string) => void;
  addDependency: (source: string, target: string) => void;
  removeDependency: (source: string, target: string) => void;
  selectNode: (nodeName: string | null) => void;
  selectScript: (path: string | null) => void;
  setGraphDirection: (direction: GraphDirection) => void;
  setActivePanel: (activePanel: ActivePanel) => void;
  toggleConsole: () => void;
  setActiveRunId: (id: string | null) => void;
}

const cloneGraph = (graph: GraphDraft): GraphDraft => structuredClone(graph);

export const useMigratorStore = create<MigratorStore>((set) => ({
  graphDraft: null,
  graphBaseline: null,
  graphFingerprint: undefined,
  databaseFingerprint: undefined,
  selectedNodeId: null,
  selectedScriptPath: null,
  graphDirection: "right",
  activePanel: "graph",
  consoleOpen: true,
  activeRunId: null,
  setGraphDraft: (graphDraft) => set({ graphDraft: cloneGraph(graphDraft) }),
  synchronizeGraph: (graph, fingerprint) =>
    set((state) => {
      if (
        !state.graphDraft ||
        !state.graphBaseline ||
        graphsEqual(state.graphDraft, state.graphBaseline)
      ) {
        return {
          graphDraft: cloneGraph(graph),
          graphBaseline: cloneGraph(graph),
          graphFingerprint: fingerprint,
        };
      }
      if (graphsEqual(graph, state.graphBaseline)) {
        return { graphFingerprint: fingerprint };
      }
      return state;
    }),
  commitGraph: (graph, fingerprint) =>
    set({
      graphDraft: cloneGraph(graph),
      graphBaseline: cloneGraph(graph),
      graphFingerprint: fingerprint,
    }),
  setDatabaseFingerprint: (databaseFingerprint) => set({ databaseFingerprint }),
  updateNode: (nodeName, update) =>
    set((state) => {
      if (!state.graphDraft) return state;
      const graphDraft = cloneGraph(state.graphDraft);
      const node = graphDraft.nodes.find((entry) => entry.name === nodeName);
      if (!node) return state;
      const renamedTo = update.name && update.name !== nodeName ? update.name : null;
      Object.assign(node, update);
      if (renamedTo) {
        graphDraft.nodes.forEach((candidate) => {
          candidate.dependsOn = candidate.dependsOn.map((dependency) =>
            dependency === nodeName ? renamedTo : dependency,
          );
        });
      }
      return {
        graphDraft,
        selectedNodeId:
          renamedTo && state.selectedNodeId === nodeName ? renamedTo : state.selectedNodeId,
      };
    }),
  updateGraph: (update) =>
    set((state) => (state.graphDraft ? { graphDraft: { ...state.graphDraft, ...update } } : state)),
  moveScript: (nodeName, fromIndex, toIndex) =>
    set((state) => {
      if (!state.graphDraft || fromIndex === toIndex) return state;
      const graphDraft = cloneGraph(state.graphDraft);
      const node = graphDraft.nodes.find((entry) => entry.name === nodeName);
      if (
        !node ||
        fromIndex < 0 ||
        toIndex < 0 ||
        fromIndex >= node.scripts.length ||
        toIndex >= node.scripts.length
      ) {
        return state;
      }
      const [script] = node.scripts.splice(fromIndex, 1);
      if (script) node.scripts.splice(toIndex, 0, script);
      return { graphDraft };
    }),
  addScript: (nodeName, path) =>
    set((state) => {
      if (!state.graphDraft) return state;
      const graphDraft = cloneGraph(state.graphDraft);
      const node = graphDraft.nodes.find((entry) => entry.name === nodeName);
      if (node && !node.scripts.some((script) => script.path === path)) {
        node.scripts.push({ path });
      }
      return { graphDraft };
    }),
  removeScript: (nodeName, path) =>
    set((state) => {
      if (!state.graphDraft) return state;
      const graphDraft = cloneGraph(state.graphDraft);
      const node = graphDraft.nodes.find((entry) => entry.name === nodeName);
      if (node && node.scripts.length > 1) {
        node.scripts = node.scripts.filter((script) => script.path !== path);
      }
      return {
        graphDraft,
        selectedScriptPath: state.selectedScriptPath === path ? null : state.selectedScriptPath,
      };
    }),
  addDependency: (source, target) =>
    set((state) => {
      if (!state.graphDraft || source === target) return state;
      const graphDraft = cloneGraph(state.graphDraft);
      const node = graphDraft.nodes.find((entry) => entry.name === target);
      if (node && !node.dependsOn.includes(source)) node.dependsOn.push(source);
      return { graphDraft };
    }),
  removeDependency: (source, target) =>
    set((state) => {
      if (!state.graphDraft) return state;
      const graphDraft = cloneGraph(state.graphDraft);
      const node = graphDraft.nodes.find((entry) => entry.name === target);
      if (node) node.dependsOn = node.dependsOn.filter((dependency) => dependency !== source);
      return { graphDraft };
    }),
  selectNode: (selectedNodeId) => set({ selectedNodeId, selectedScriptPath: null }),
  selectScript: (selectedScriptPath) => set({ selectedScriptPath }),
  setGraphDirection: (graphDirection) => set({ graphDirection }),
  setActivePanel: (activePanel) => set({ activePanel }),
  toggleConsole: () => set((state) => ({ consoleOpen: !state.consoleOpen })),
  setActiveRunId: (activeRunId) => set({ activeRunId }),
}));

export const effectivePolicy = (node: MigrationNode, graph: GraphDraft): ErrorPolicy =>
  node.onError ?? graph.onError;

export const graphsEqual = (left: MigrationGraph, right: MigrationGraph) =>
  JSON.stringify(left) === JSON.stringify(right);
