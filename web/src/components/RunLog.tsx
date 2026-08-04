import { AlertCircle, CheckCircle2, CircleDot, Pause, Play, RotateCcw } from "lucide-react";
import { useEffect, useState } from "react";
import { useResumeRun, useRunQuery, useRunsQuery, useStartRun } from "../hooks";
import { executionBlocker } from "../lib/execution";
import { useMigratorStore } from "../store";
import type { RunSnapshot as RunSnapshotData } from "../types";
import { ActionButton, EmptyState, Modal, StatusMark } from "./ui";

export function RunLog({
  isDirty = false,
  ready = false,
  problems = [],
}: {
  isDirty?: boolean;
  ready?: boolean;
  problems?: string[];
}) {
  const consoleOpen = useMigratorStore((state) => state.consoleOpen);
  const activeRunId = useMigratorStore((state) => state.activeRunId);
  const setActiveRunId = useMigratorStore((state) => state.setActiveRunId);
  const runs = useRunsQuery();
  const activeRun = useRunQuery(activeRunId);
  const startRun = useStartRun();
  const resumeRun = useResumeRun();
  const [forceOpen, setForceOpen] = useState(false);
  useEffect(() => {
    if (!activeRunId && runs.data?.[0]) setActiveRunId(runs.data[0].id);
  }, [activeRunId, runs.data, setActiveRunId]);
  if (!consoleOpen) return null;
  const snapshot = activeRun.data;
  const canResume = snapshot?.run.status === "failed" || snapshot?.run.status === "cancelled";
  const checksumMismatch = snapshot ? hasChecksumMismatch(snapshot) : false;
  const runBlocker = executionBlocker({ ready, isDirty, problems });
  return (
    <section className="run-log" aria-label="Migration run log">
      <div className="run-log-header">
        <div className="eyebrow">Execution trail</div>
        <div className="run-log-title">Run log</div>
        <div className="spacer" />
        {snapshot ? <StatusMark status={snapshot.run.status} /> : null}
        <ActionButton
          tone="purple"
          disabled={Boolean(runBlocker) || startRun.isPending}
          title={runBlocker || undefined}
          onClick={() => startRun.mutate(false)}
        >
          <Play size={13} /> {startRun.isPending ? "Starting…" : "Run"}
        </ActionButton>
        {runBlocker ? <span className="run-blocker">{runBlocker}</span> : null}
        {canResume && snapshot ? (
          <ActionButton
            tone="yellow"
            disabled={Boolean(runBlocker) || resumeRun.isPending}
            title={runBlocker || undefined}
            onClick={() => resumeRun.mutate({ id: snapshot.run.id, force: false })}
          >
            <RotateCcw size={13} /> Resume
          </ActionButton>
        ) : null}
        {canResume && snapshot && checksumMismatch ? (
          <ActionButton
            tone="danger"
            disabled={Boolean(runBlocker) || resumeRun.isPending}
            title={runBlocker || undefined}
            onClick={() => setForceOpen(true)}
          >
            Force resume
          </ActionButton>
        ) : null}
        {startRun.isError || resumeRun.isError ? (
          <span className="mutation-error" role="alert">
            {startRun.isError && startRun.error instanceof Error
              ? startRun.error.message
              : resumeRun.isError && resumeRun.error instanceof Error
                ? resumeRun.error.message
                : "Operation failed"}
          </span>
        ) : null}
      </div>
      <div className="run-log-body">
        {runs.isLoading ? (
          <div className="run-log-state" role="status" aria-live="polite">
            Loading run history…
          </div>
        ) : runs.isError ? (
          <div className="run-log-state run-log-state--error">
            Run history unavailable:{" "}
            {runs.error instanceof Error ? runs.error.message : "Unknown error"}
          </div>
        ) : null}
        {!snapshot && !runs.isLoading && !runs.isError ? (
          <EmptyState
            title="No migration runs yet"
            detail="Run the graph to stream node and script transitions here."
          />
        ) : null}
        {snapshot ? <RunSnapshot snapshot={snapshot} /> : null}
      </div>
      {snapshot ? (
        <Modal
          open={forceOpen}
          onOpenChange={setForceOpen}
          title="Force this migration resume?"
          description="A previously executed SQL file no longer matches its recorded checksum."
        >
          <div className="force-resume-copy">
            Force resume executes the changed file against the target database and records its new
            checksum. Review the SQL and database state before continuing.
          </div>
          <div className="dialog-footer">
            <div className="spacer" />
            <ActionButton onClick={() => setForceOpen(false)}>Cancel</ActionButton>
            <ActionButton
              tone="danger"
              disabled={resumeRun.isPending}
              onClick={() => {
                resumeRun.mutate(
                  { id: snapshot.run.id, force: true },
                  { onSuccess: () => setForceOpen(false) },
                );
              }}
            >
              {resumeRun.isPending ? "Resuming…" : "Force resume"}
            </ActionButton>
          </div>
        </Modal>
      ) : null}
    </section>
  );
}

function RunSnapshot({
  snapshot,
}: {
  snapshot: NonNullable<ReturnType<typeof useRunQuery>["data"]>;
}) {
  const selectNode = useMigratorStore((state) => state.selectNode);
  const setActivePanel = useMigratorStore((state) => state.setActivePanel);
  return (
    <div className="run-snapshot">
      <div className="run-summary">
        <div>
          <code>{snapshot.run.id}</code>
          <span className="muted-copy"> attempt {snapshot.run.attempt}</span>
        </div>
        <span>
          {snapshot.run.completedNodes}/{snapshot.run.totalNodes || snapshot.nodes.length} nodes
          complete
        </span>
      </div>
      <div className="run-progress">
        <span
          style={{
            width: `${progressPercent(snapshot.run.completedNodes, snapshot.run.totalNodes || snapshot.nodes.length)}%`,
          }}
        />
      </div>
      <div className="run-columns">
        <div className="run-events">
          {snapshot.logs.length === 0 ? (
            <div className="muted-copy">No log entries yet.</div>
          ) : (
            snapshot.logs.map((log, index) => (
              <div
                className={`run-event run-event--${log.level}`}
                key={`${log.sequence ?? index}-${log.at}`}
              >
                <time>{formatTime(log.at)}</time>
                <span className="run-event-symbol">
                  {log.level === "error" ? (
                    <AlertCircle size={13} />
                  ) : log.level === "warn" ? (
                    <Pause size={13} />
                  ) : (
                    <CheckCircle2 size={13} />
                  )}
                </span>
                <code>{[log.node, log.script].filter(Boolean).join(" / ") || "engine"}</code>
                <span>{log.message}</span>
              </div>
            ))
          )}
        </div>
        <div className="run-node-list">
          {snapshot.nodes.map((node) => (
            <button
              className="run-node-row"
              key={`${node.name}-${node.attempt ?? 0}`}
              onClick={() => {
                selectNode(node.name);
                setActivePanel("inspector");
              }}
            >
              <CircleDot
                size={13}
                className={`run-node-icon run-node-icon--${node.status ?? "pending"}`}
              />
              <code>{node.name}</code>
              <span className="spacer" />
              <StatusMark status={node.status ?? "pending"} />
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function progressPercent(done: number, total: number) {
  return total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0;
}
function formatTime(input: string) {
  const date = new Date(input);
  return Number.isNaN(date.getTime())
    ? input
    : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function hasChecksumMismatch(snapshot: RunSnapshotData) {
  const messages = [
    snapshot.run.error,
    ...snapshot.nodes.flatMap((node) => [
      node.error,
      ...node.scripts.map((script) => script.error),
    ]),
    ...snapshot.logs.map((log) => log.message),
  ];
  return messages.some((message) => message?.toLowerCase().includes("checksum mismatch"));
}
