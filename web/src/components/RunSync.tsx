import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { useStore } from "../store";

/** Mirrors the server's latest run of a connection into the persisted results, polling while it runs. */
export function RunSync({ name }: { name: string }) {
  const applyRun = useStore((store) => store.applyRun);
  const markInterrupted = useStore((store) => store.markInterrupted);
  const run = useQuery({
    queryKey: ["run", name],
    queryFn: () => api.run(name),
    refetchInterval: (query) => (query.state.data?.status === "running" ? 700 : false),
  });

  useEffect(() => {
    if (run.data) applyRun(name, run.data);
    else if (run.data === null) markInterrupted(name);
  }, [run.data, name, applyRun, markInterrupted]);

  return null;
}
