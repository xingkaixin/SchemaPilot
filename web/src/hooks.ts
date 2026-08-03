import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import {
  fetchProject,
  fetchRun,
  fetchRuns,
  fetchScript,
  resumeRun,
  saveGraph,
  saveScript,
  startRun,
  testDatabase,
} from "./lib/api";
import { useMigratorStore } from "./store";

export function useProjectQuery() {
  const query = useQuery({ queryKey: ["project"], queryFn: fetchProject, staleTime: 15_000 });
  const synchronizeGraph = useMigratorStore((state) => state.synchronizeGraph);
  useEffect(() => {
    if (query.data) synchronizeGraph(query.data.graph, query.data.fingerprint);
  }, [query.data, synchronizeGraph]);
  return query;
}

export function useSaveGraph() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (graph: Parameters<typeof saveGraph>[0]) => {
      const fingerprint = useMigratorStore.getState().graphFingerprint;
      return saveGraph(graph, fingerprint);
    },
    onSuccess: (project) => {
      queryClient.setQueryData(["project"], project);
      useMigratorStore.getState().commitGraph(project.graph, project.fingerprint);
    },
  });
}

export function useScriptQuery(path: string | null) {
  return useQuery({
    queryKey: ["script", path],
    queryFn: () => fetchScript(path as string),
    enabled: Boolean(path),
    staleTime: Infinity,
  });
}

export function useSaveScript() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ path, sql, checksum }: { path: string; sql: string; checksum?: string }) =>
      saveScript(path, sql, checksum),
    onSuccess: (script, variables) => {
      queryClient.setQueryData(["script", variables.path], script);
      queryClient.invalidateQueries({ queryKey: ["project"] });
    },
  });
}

export function useRunsQuery() {
  return useQuery({ queryKey: ["runs"], queryFn: fetchRuns, refetchInterval: 5_000 });
}

export function useRunQuery(runId: string | null) {
  return useQuery({
    queryKey: ["run", runId],
    queryFn: () => fetchRun(runId as string),
    enabled: Boolean(runId),
    refetchInterval: (query) => {
      const status = query.state.data?.run.status;
      return status === "running" || status === "pending" ? 1_000 : false;
    },
  });
}

export function useStartRun() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: startRun,
    onSuccess: (run) => {
      useMigratorStore.getState().setActiveRunId(run.id);
      queryClient.invalidateQueries({ queryKey: ["runs"] });
    },
  });
}

export function useResumeRun() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, force }: { id: string; force: boolean }) => resumeRun(id, force),
    onSuccess: (run) => {
      useMigratorStore.getState().setActiveRunId(run.id);
      queryClient.invalidateQueries({ queryKey: ["runs"] });
      queryClient.invalidateQueries({ queryKey: ["run", run.id] });
    },
  });
}

export function useTestDatabase() {
  return useMutation({ mutationFn: testDatabase });
}
