import { useCallback, useEffect, useState } from "react";
import type { ApiClient } from "../api/client";
import type { BackupInfo, BackupVerifyResult } from "../api/types";

interface Props {
  client: Pick<ApiClient, "listBackups" | "createBackup" | "downloadBackup" | "verifyBackup">;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(1)} ${units[unit]}`;
}

export function BackupsView({ client }: Props) {
  const [backups, setBackups] = useState<BackupInfo[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [downloading, setDownloading] = useState<Record<string, boolean>>({});
  const [verifying, setVerifying] = useState<Record<string, boolean>>({});
  const [verifyResults, setVerifyResults] = useState<Record<string, BackupVerifyResult | { ok: false; error: string }>>({});
  const [selectedBackup, setSelectedBackup] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setBackups(await client.listBackups());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load backups");
    }
  }, [client]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function runBackup() {
    setRunning(true);
    try {
      await client.createBackup();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "backup failed");
    } finally {
      setRunning(false);
    }
  }

  async function download(filename: string) {
    setDownloading((prev) => ({ ...prev, [filename]: true }));
    try {
      await client.downloadBackup(filename);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "download failed");
    } finally {
      setDownloading((prev) => ({ ...prev, [filename]: false }));
    }
  }

  async function verify(filename: string) {
    setVerifying((prev) => ({ ...prev, [filename]: true }));
    try {
      const result = await client.verifyBackup(filename);
      setVerifyResults((prev) => ({ ...prev, [filename]: result }));
      setError(null);
    } catch (err) {
      setVerifyResults((prev) => ({
        ...prev,
        [filename]: { ok: false, error: err instanceof Error ? err.message : "verify failed" },
      }));
    } finally {
      setVerifying((prev) => ({ ...prev, [filename]: false }));
    }
  }

  return (
    <section>
      <div className="row">
        <h1>Backups</h1>
        <button onClick={() => void runBackup()} disabled={running}>
          {running ? "Running…" : "Run backup now"}
        </button>
      </div>
      {error && <p className="error">{error}</p>}

      <table>
        <thead>
          <tr>
            <th>Filename</th>
            <th>Size</th>
            <th>Created</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {backups.map((b) => (
            <tr key={b.filename} className={selectedBackup === b.filename ? "selected" : ""}>
              <td>{b.filename}</td>
              <td>{formatBytes(b.size_bytes)}</td>
              <td>{new Date(b.created_at).toLocaleString()}</td>
              <td>
                <button disabled={downloading[b.filename]} onClick={() => void download(b.filename)}>
                  {downloading[b.filename] ? "Downloading…" : "Download"}
                </button>
                <button disabled={verifying[b.filename]} onClick={() => void verify(b.filename)}>
                  {verifying[b.filename] ? "Verifying…" : "Verify"}
                </button>
                <button onClick={() => setSelectedBackup(selectedBackup === b.filename ? null : b.filename)}>
                  {selectedBackup === b.filename ? "Hide restore" : "Restore…"}
                </button>
                {verifyResults[b.filename] && (
                  <span
                    className={`badge ${verifyResults[b.filename].ok ? "badge-ok" : "badge-error"}`}
                    data-testid={`verify-badge-${b.filename}`}
                  >
                    {verifyResults[b.filename].ok
                      ? `OK (${(verifyResults[b.filename] as BackupVerifyResult).table_count} tables)`
                      : verifyResults[b.filename].error}
                  </span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {backups.length === 0 && !error && <p>No backups yet.</p>}

      {selectedBackup && (
        <div className="restore-panel" data-testid="restore-panel">
          <h2>Restore procedure</h2>
          <p>To restore this backup, follow these steps:</p>
          <ol>
            <li>Stop the Muesli server process.</li>
            <li>
              Run the following command:
              <pre>
                <code>pg_restore -U postgres -d muesli --clean --if-exists {selectedBackup}</code>
              </pre>
            </li>
            <li>Start the server again.</li>
          </ol>
          <button disabled={downloading[selectedBackup]} onClick={() => void download(selectedBackup)}>
            {downloading[selectedBackup] ? "Downloading…" : "Download"}
          </button>
        </div>
      )}
    </section>
  );
}
