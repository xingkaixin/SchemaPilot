import { Database, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useDeleteDatabase, useSaveDatabase, useTestDatabase } from "../hooks";
import type { DatabaseDriver, DatabaseProfile } from "../types";
import { ActionButton, BaseSelect, EmptyState, FieldLabel, Modal, StatusMark } from "./ui";

export function DatabaseProfilesPanel({ databases }: { databases: DatabaseProfile[] }) {
  const testDatabase = useTestDatabase();
  const deleteDatabase = useDeleteDatabase();
  const [editing, setEditing] = useState<DatabaseProfile | null>(null);
  const [deleting, setDeleting] = useState<DatabaseProfile | null>(null);
  const [creating, setCreating] = useState(false);
  const resetDatabaseTest = testDatabase.reset;
  useEffect(() => resetDatabaseTest(), [databases, resetDatabaseTest]);

  return (
    <div className="sidebar-content connections-list">
      <div className="connection-toolbar">
        <div>
          <div className="section-caption">Destinations</div>
          <span className="muted-copy">saved to databases.toml</span>
        </div>
        <ActionButton onClick={() => setCreating(true)}>
          <Plus size={13} /> Add profile
        </ActionButton>
      </div>
      {databases.length === 0 ? (
        <EmptyState
          title="No database profiles"
          detail="Add a destination before assigning migration nodes."
        />
      ) : null}
      {databases.map((database) => {
        const isTesting = testDatabase.isPending && testDatabase.variables === database.name;
        const result =
          testDatabase.data && testDatabase.variables === database.name ? testDatabase.data : null;
        return (
          <article className="connection-card" key={database.name}>
            <header>
              <span className={`database-led database-led--${database.driver}`}>
                <Database size={14} />
              </span>
              <code>{database.name}</code>
              <span className="driver-badge">{database.driver}</span>
            </header>
            <div className="connection-dsn">{displayDsn(database.dsn)}</div>
            <div className="connection-footer">
              <StatusMark status={result ? "succeeded" : database.status} />
              <div className="spacer" />
              <button
                className="icon-button connection-icon-button"
                aria-label={`Edit ${database.name}`}
                onClick={() => setEditing(database)}
              >
                <Pencil size={13} />
              </button>
              <button
                className="icon-button connection-icon-button"
                aria-label={`Delete ${database.name}`}
                onClick={() => {
                  deleteDatabase.reset();
                  setDeleting(database);
                }}
              >
                <Trash2 size={13} />
              </button>
              <ActionButton onClick={() => testDatabase.mutate(database.name)} disabled={isTesting}>
                {isTesting ? <RefreshCw className="spin" size={13} /> : "Test"}
              </ActionButton>
            </div>
            {result ? (
              <div className="connection-result" role="status" aria-live="polite">
                Reachable in {result.latency_ms} ms
              </div>
            ) : null}
            {testDatabase.isError && testDatabase.variables === database.name ? (
              <div className="connection-result connection-result--error" role="alert">
                Test failed:{" "}
                {testDatabase.error instanceof Error ? testDatabase.error.message : "Unknown error"}
              </div>
            ) : null}
          </article>
        );
      })}
      <div className="credentials-note">
        Keep secrets in environment variables. Commit only TOML profiles that reference those
        variables.
      </div>
      {creating || editing ? (
        <DatabaseProfileEditor
          profile={editing}
          databaseNames={databases.map((database) => database.name)}
          onOpenChange={(open) => {
            if (!open) {
              setCreating(false);
              setEditing(null);
            }
          }}
        />
      ) : null}
      <Modal
        open={Boolean(deleting)}
        onOpenChange={(open) => {
          if (!open) {
            setDeleting(null);
            deleteDatabase.reset();
          }
        }}
        title={`Delete ${deleting?.name ?? "database profile"}?`}
        description="Nodes using this profile will no longer be runnable until they are reassigned."
      >
        {deleteDatabase.isError ? (
          <div className="profile-delete-error mutation-error" role="alert">
            {deleteDatabase.error instanceof Error
              ? deleteDatabase.error.message
              : "Could not delete profile."}
          </div>
        ) : null}
        <div className="dialog-footer">
          <div className="spacer" />
          <ActionButton onClick={() => setDeleting(null)}>Cancel</ActionButton>
          <ActionButton
            tone="danger"
            disabled={!deleting || deleteDatabase.isPending}
            onClick={() => {
              if (!deleting) return;
              deleteDatabase.mutate(deleting.name, { onSuccess: () => setDeleting(null) });
            }}
          >
            {deleteDatabase.isPending ? "Deleting…" : "Delete profile"}
          </ActionButton>
        </div>
      </Modal>
    </div>
  );
}

function displayDsn(dsn: string) {
  if (!dsn) return "DSN supplied by environment";
  return dsn.replace(/:\/\/[^@/]+@/, "://••••@");
}

interface ProfileDraft {
  name: string;
  driver: DatabaseDriver;
  dsn: string;
  maxOpenConnections: string;
  connectionTimeout: string;
}

function DatabaseProfileEditor({
  profile,
  databaseNames,
  onOpenChange,
}: {
  profile: DatabaseProfile | null;
  databaseNames: string[];
  onOpenChange: (open: boolean) => void;
}) {
  const saveDatabase = useSaveDatabase();
  const [draft, setDraft] = useState<ProfileDraft>(() => profileDraft(profile));
  const [error, setError] = useState<string | null>(null);
  const isEditing = Boolean(profile);
  const submit = async () => {
    const name = draft.name.trim();
    if (!/^[A-Za-z][A-Za-z0-9_-]*$/.test(name)) {
      setError("Use a name starting with a letter, followed by letters, numbers, _ or -.");
      return;
    }
    if (!isEditing && !draft.dsn.trim()) {
      setError("A DSN is required when creating a profile.");
      return;
    }
    if (!isEditing && databaseNames.includes(name)) {
      setError(`Profile ${name} already exists. Use Edit to change it.`);
      return;
    }
    const maxOpenConnections = draft.maxOpenConnections.trim()
      ? Number(draft.maxOpenConnections)
      : 4;
    if (!Number.isInteger(maxOpenConnections) || maxOpenConnections < 1) {
      setError("Max connections must be a positive whole number.");
      return;
    }
    setError(null);
    try {
      await saveDatabase.mutateAsync({
        name,
        driver: draft.driver,
        dsn: draft.dsn,
        maxOpenConnections,
        connectionTimeout: draft.connectionTimeout.trim() || "5s",
      });
      onOpenChange(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not save profile.");
    }
  };
  return (
    <Modal
      open
      onOpenChange={onOpenChange}
      title={isEditing ? `Edit ${profile?.name}` : "Add database profile"}
      description="The DSN can reference an environment variable."
      className="profile-editor-modal"
    >
      <div className="profile-editor-form">
        <FieldLabel htmlFor="profile-name">Name</FieldLabel>
        <input
          id="profile-name"
          className="field-input"
          value={draft.name}
          disabled={isEditing}
          onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))}
          autoComplete="off"
          spellCheck={false}
        />
        <BaseSelect
          label="Driver"
          value={draft.driver}
          onValueChange={(driver) =>
            setDraft((current) => ({ ...current, driver: driver as DatabaseDriver }))
          }
          options={[
            { value: "postgres", label: "PostgreSQL" },
            { value: "mysql", label: "MySQL" },
            { value: "sqlserver", label: "SQL Server" },
          ]}
        />
        <FieldLabel htmlFor="profile-dsn">DSN</FieldLabel>
        <input
          id="profile-dsn"
          className="field-input"
          value={draft.dsn}
          onChange={(event) => setDraft((current) => ({ ...current, dsn: event.target.value }))}
          placeholder={isEditing ? "Leave blank to keep the current DSN" : "${DATABASE_URL}"}
          autoComplete="new-password"
          spellCheck={false}
        />
        <div className="profile-editor-grid">
          <div>
            <FieldLabel htmlFor="profile-max-connections">Max connections</FieldLabel>
            <input
              id="profile-max-connections"
              className="field-input"
              type="number"
              min="1"
              value={draft.maxOpenConnections}
              onChange={(event) =>
                setDraft((current) => ({ ...current, maxOpenConnections: event.target.value }))
              }
            />
          </div>
          <div>
            <FieldLabel htmlFor="profile-timeout">Connection timeout</FieldLabel>
            <input
              id="profile-timeout"
              className="field-input"
              value={draft.connectionTimeout}
              onChange={(event) =>
                setDraft((current) => ({ ...current, connectionTimeout: event.target.value }))
              }
              placeholder="10s"
              autoComplete="off"
            />
          </div>
        </div>
        {error ? (
          <div className="mutation-error" role="alert">
            {error}
          </div>
        ) : null}
      </div>
      <div className="dialog-footer">
        <div className="spacer" />
        <ActionButton onClick={() => onOpenChange(false)}>Cancel</ActionButton>
        <ActionButton tone="purple" disabled={saveDatabase.isPending} onClick={() => void submit()}>
          {saveDatabase.isPending ? "Saving…" : "Save profile"}
        </ActionButton>
      </div>
    </Modal>
  );
}

function profileDraft(profile: DatabaseProfile | null): ProfileDraft {
  return {
    name: profile?.name ?? "",
    driver: profile?.driver ?? "postgres",
    dsn: "",
    maxOpenConnections: profile?.maxOpenConnections?.toString() ?? "4",
    connectionTimeout: profile?.connectionTimeout ?? "5s",
  };
}
