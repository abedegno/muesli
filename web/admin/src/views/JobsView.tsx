import { useCallback, useEffect, useMemo, useState } from "react";
import type { ApiClient } from "../api/client";
import type { Job } from "../api/types";

interface Props {
  client: Pick<
    ApiClient,
    "listJobs" | "retryJob" | "resummarizeNote" | "cancelJob" | "processNextJob" | "listNoteJobs"
  >;
}

type JobAction = "retry" | "cancel" | "process-next";

// formatDuration renders the started_at/finished_at diff for a job's most
// recent attempt as e.g. "3m 4s". Returns "in progress" when started but not
// yet finished, and "—" when the attempt hasn't started at all.
function formatDuration(startedAt: string | null, finishedAt: string | null): string {
  if (!startedAt) return "—";
  if (!finishedAt) return "in progress";
  const ms = new Date(finishedAt).getTime() - new Date(startedAt).getTime();
  const totalSeconds = Math.max(0, Math.round(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return minutes > 0 ? `${minutes}m ${seconds}s` : `${seconds}s`;
}

// StatusBadge reuses the app's existing badge styling (see .badge-* in app.css).
function StatusBadge({ status }: { status: string }) {
  return <span className={`badge badge-${status}`}>{status}</span>;
}

// jobTargetLabel renders a job's target generically: a note job shows its
// note id, an event (pre_generate) job shows its event id. Exactly one of
// note_id/calendar_event_id is ever present on a given job (see the Job
// type), so this never has to guess.
function jobTargetLabel(job: Job): string {
  if (job.note_id) return `note ${job.note_id}`;
  if (job.calendar_event_id) return `event ${job.calendar_event_id}`;
  return "—";
}

export function JobsView({ client }: Props) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [statusFilter, setStatusFilter] = useState("");
  const [typeFilter, setTypeFilter] = useState("");
  const [noteIdSearch, setNoteIdSearch] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const [actionErrors, setActionErrors] = useState<Record<string, string>>({});

  // ADM04: per-note pipeline timeline, triggered from a job row's "View
  // timeline" button.
  const [timelineNoteId, setTimelineNoteId] = useState<string | null>(null);
  const [timelineJobs, setTimelineJobs] = useState<Job[]>([]);
  const [timelineError, setTimelineError] = useState<string | null>(null);
  const [timelineLoading, setTimelineLoading] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setJobs(await client.listJobs(statusFilter));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load jobs");
    }
  }, [client, statusFilter]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const visibleJobs = useMemo(() => {
    const needle = noteIdSearch.trim().toLowerCase();
    return jobs.filter((j) => {
      if (typeFilter && j.type !== typeFilter) return false;
      if (needle && !jobTargetLabel(j).toLowerCase().includes(needle)) return false;
      return true;
    });
  }, [jobs, typeFilter, noteIdSearch]);

  const confirmMessages: Record<JobAction, (job: Job) => string> = {
    retry: (job) => `Retry job ${job.id} (${jobTargetLabel(job)})?`,
    cancel: (job) => `Cancel pending job ${job.id} (${jobTargetLabel(job)})?`,
    "process-next": (job) => `Process job ${job.id} (${jobTargetLabel(job)}) next?`,
  };

  const handleAction = useCallback(
    async (job: Job, action: JobAction) => {
      if (!window.confirm(confirmMessages[action](job))) {
        return;
      }
      setBusy((prev) => ({ ...prev, [job.id]: true }));
      setActionErrors((prev) => {
        const next = { ...prev };
        delete next[job.id];
        return next;
      });
      try {
        if (action === "retry") {
          if (job.type === "summarize" && job.note_id) {
            await client.resummarizeNote(job.note_id);
          } else {
            await client.retryJob(job.id);
          }
        } else if (action === "cancel") {
          await client.cancelJob(job.id);
        } else {
          await client.processNextJob(job.id);
        }
        await refresh();
      } catch (err) {
        setActionErrors((prev) => ({
          ...prev,
          [job.id]: err instanceof Error ? err.message : `${action} failed`,
        }));
      } finally {
        setBusy((prev) => ({ ...prev, [job.id]: false }));
      }
    },
    [client, refresh],
  );

  const showTimeline = useCallback(
    async (noteId: string) => {
      setTimelineNoteId(noteId);
      setTimelineLoading(true);
      setTimelineError(null);
      try {
        setTimelineJobs(await client.listNoteJobs(noteId));
      } catch (err) {
        setTimelineJobs([]);
        setTimelineError(err instanceof Error ? err.message : "failed to load timeline");
      } finally {
        setTimelineLoading(false);
      }
    },
    [client],
  );

  return (
    <section>
      <div className="row">
        <h1>Processing jobs</h1>
        <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} aria-label="status filter">
          <option value="">all</option>
          <option value="pending">pending</option>
          <option value="running">running</option>
          <option value="failed">failed</option>
          <option value="done">done</option>
        </select>
        <select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)} aria-label="type filter">
          <option value="">all types</option>
          <option value="transcribe">transcribe</option>
          <option value="summarize">summarize</option>
          <option value="embed">embed</option>
          <option value="pre_generate">pre_generate</option>
        </select>
        <input
          type="text"
          value={noteIdSearch}
          onChange={(e) => setNoteIdSearch(e.target.value)}
          placeholder="Search note or event id…"
          aria-label="note or event id search"
        />
        <button onClick={() => void refresh()}>Refresh</button>
      </div>
      {error && <p className="error">{error}</p>}
      <table>
        <thead>
          <tr>
            <th>Target</th>
            <th>Type</th>
            <th>Status</th>
            <th>Attempts</th>
            <th>Last error</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {visibleJobs.map((j) => (
            <tr key={j.id}>
              <td>{jobTargetLabel(j)}</td>
              <td>{j.type}</td>
              <td>
                <StatusBadge status={j.status} />
              </td>
              <td>{j.attempts}</td>
              <td>{j.last_error ?? "—"}</td>
              <td>
                {j.status === "failed" && (
                  <button disabled={busy[j.id]} onClick={() => void handleAction(j, "retry")}>
                    {busy[j.id] ? "Retrying…" : "Retry"}
                  </button>
                )}
                {j.status === "pending" && (
                  <>
                    <button disabled={busy[j.id]} onClick={() => void handleAction(j, "cancel")}>
                      {busy[j.id] ? "Cancelling…" : "Cancel"}
                    </button>
                    <button disabled={busy[j.id]} onClick={() => void handleAction(j, "process-next")}>
                      {busy[j.id] ? "Processing…" : "Process next"}
                    </button>
                  </>
                )}
                {j.note_id && <button onClick={() => void showTimeline(j.note_id!)}>View timeline</button>}
                {actionErrors[j.id] && <span style={{ color: "red" }}>{actionErrors[j.id]}</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {timelineNoteId && (
        <section aria-label="pipeline timeline">
          <h2>Pipeline timeline — note {timelineNoteId}</h2>
          {timelineLoading && <p>Loading…</p>}
          {timelineError && <p className="error">{timelineError}</p>}
          {!timelineLoading && !timelineError && (
            <ol className="timeline">
              {timelineJobs.map((tj) => (
                <li key={tj.id}>
                  <strong>{tj.type}</strong>
                  <StatusBadge status={tj.status} />
                  <span className="timeline-meta">
                    attempts: {tj.attempts} · duration: {formatDuration(tj.started_at, tj.finished_at)}
                  </span>
                  {tj.status === "failed" && tj.last_error && <p className="error">{tj.last_error}</p>}
                </li>
              ))}
            </ol>
          )}
        </section>
      )}
    </section>
  );
}
