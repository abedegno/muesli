import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { JobsView } from "./JobsView";
import type { Job } from "../api/types";

function makeClient(jobs: Job[], noteJobs: Job[] = []) {
  return {
    listJobs: vi.fn().mockResolvedValue(jobs),
    retryJob: vi.fn().mockResolvedValue(undefined),
    resummarizeNote: vi.fn().mockResolvedValue(undefined),
    cancelJob: vi.fn().mockResolvedValue(undefined),
    processNextJob: vi.fn().mockResolvedValue(undefined),
    listNoteJobs: vi.fn().mockResolvedValue(noteJobs),
  };
}

const failedJob: Job = {
  id: "j1",
  note_id: "note-failed",
  type: "transcribe",
  status: "failed",
  attempts: 3,
  last_error: "boom",
  priority: 0,
  started_at: "2026-07-05T10:00:00Z",
  finished_at: "2026-07-05T10:00:05Z",
};

const pendingJob: Job = {
  id: "j2",
  note_id: "note-pending",
  type: "summarize",
  status: "pending",
  attempts: 0,
  last_error: null,
  priority: 0,
  started_at: null,
  finished_at: null,
};

const runningJob: Job = {
  id: "j3",
  note_id: "note-running",
  type: "embed",
  status: "running",
  attempts: 1,
  last_error: null,
  priority: 0,
  started_at: "2026-07-05T10:05:00Z",
  finished_at: null,
};

const doneJob: Job = {
  id: "j4",
  note_id: "note-done",
  type: "transcribe",
  status: "done",
  attempts: 1,
  last_error: null,
  priority: 0,
  started_at: "2026-07-05T10:10:00Z",
  finished_at: "2026-07-05T10:13:04Z",
};

describe("JobsView", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows Retry only for a failed job", async () => {
    const client = makeClient([failedJob, pendingJob, runningJob, doneJob]);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-failed");

    expect(screen.getByRole("button", { name: /^retry$/i })).toBeInTheDocument();
  });

  it("shows Cancel and Process next only for a pending job", async () => {
    const client = makeClient([failedJob, pendingJob, runningJob, doneJob]);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-pending");

    expect(screen.getByRole("button", { name: /^cancel$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^process next$/i })).toBeInTheDocument();
  });

  it("hides Cancel and Process next for running/done/failed jobs", async () => {
    const client = makeClient([failedJob, runningJob, doneJob]);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-failed");

    expect(screen.queryByRole("button", { name: /^cancel$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^process next$/i })).not.toBeInTheDocument();
  });

  it("rejecting the confirm dialog on Retry does not call the API or refresh", async () => {
    const client = makeClient([failedJob]);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-failed");

    await userEvent.click(screen.getByRole("button", { name: /^retry$/i }));

    expect(client.retryJob).not.toHaveBeenCalled();
    expect(client.resummarizeNote).not.toHaveBeenCalled();
    expect(client.listJobs).toHaveBeenCalledTimes(1);
  });

  it("accepting the confirm dialog on Retry calls retryJob for non-summarize jobs and refetches", async () => {
    const client = makeClient([failedJob]);
    client.listJobs.mockResolvedValueOnce([failedJob]).mockResolvedValueOnce([]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-failed");

    await userEvent.click(screen.getByRole("button", { name: /^retry$/i }));

    await waitFor(() => expect(client.retryJob).toHaveBeenCalledWith("j1"));
    expect(client.resummarizeNote).not.toHaveBeenCalled();
    await waitFor(() => expect(client.listJobs).toHaveBeenCalledTimes(2));
  });

  it("accepting the confirm dialog on Retry calls resummarizeNote for summarize jobs", async () => {
    const summarizeFailed: Job = { ...failedJob, id: "j5", type: "summarize" };
    const client = makeClient([summarizeFailed]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-failed");

    await userEvent.click(screen.getByRole("button", { name: /^retry$/i }));

    await waitFor(() => expect(client.resummarizeNote).toHaveBeenCalledWith("note-failed"));
    expect(client.retryJob).not.toHaveBeenCalled();
  });

  it("rejecting the confirm dialog on Cancel does not call the API or refresh", async () => {
    const client = makeClient([pendingJob]);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-pending");

    await userEvent.click(screen.getByRole("button", { name: /^cancel$/i }));

    expect(client.cancelJob).not.toHaveBeenCalled();
    expect(client.listJobs).toHaveBeenCalledTimes(1);
  });

  it("accepting the confirm dialog on Cancel calls cancelJob with the job id and refetches", async () => {
    const client = makeClient([pendingJob]);
    client.listJobs.mockResolvedValueOnce([pendingJob]).mockResolvedValueOnce([]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-pending");

    await userEvent.click(screen.getByRole("button", { name: /^cancel$/i }));

    await waitFor(() => expect(client.cancelJob).toHaveBeenCalledWith("j2"));
    await waitFor(() => expect(client.listJobs).toHaveBeenCalledTimes(2));
  });

  it("rejecting the confirm dialog on Process next does not call the API or refresh", async () => {
    const client = makeClient([pendingJob]);
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-pending");

    await userEvent.click(screen.getByRole("button", { name: /^process next$/i }));

    expect(client.processNextJob).not.toHaveBeenCalled();
    expect(client.listJobs).toHaveBeenCalledTimes(1);
  });

  it("accepting the confirm dialog on Process next calls processNextJob with the job id and refetches", async () => {
    const client = makeClient([pendingJob]);
    client.listJobs.mockResolvedValueOnce([pendingJob]).mockResolvedValueOnce([pendingJob]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-pending");

    await userEvent.click(screen.getByRole("button", { name: /^process next$/i }));

    await waitFor(() => expect(client.processNextJob).toHaveBeenCalledWith("j2"));
    await waitFor(() => expect(client.listJobs).toHaveBeenCalledTimes(2));
  });

  it("the type filter narrows the visible rows client-side", async () => {
    const client = makeClient([failedJob, pendingJob, runningJob]);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-failed");
    expect(screen.getByText("note-pending")).toBeInTheDocument();
    expect(screen.getByText("note-running")).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText(/type filter/i), "summarize");

    expect(screen.queryByText("note-failed")).not.toBeInTheDocument();
    expect(screen.getByText("note-pending")).toBeInTheDocument();
    expect(screen.queryByText("note-running")).not.toBeInTheDocument();
    // Client-side filtering must not trigger another server round-trip.
    expect(client.listJobs).toHaveBeenCalledTimes(1);
  });

  it("the note-id search narrows the visible rows and combines with the type filter", async () => {
    const client = makeClient([failedJob, pendingJob, runningJob]);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-failed");

    await userEvent.type(screen.getByLabelText(/note id search/i), "PENDING");

    expect(screen.queryByText("note-failed")).not.toBeInTheDocument();
    expect(screen.getByText("note-pending")).toBeInTheDocument();
    expect(screen.queryByText("note-running")).not.toBeInTheDocument();

    // Combine with the type filter: narrowing to "transcribe" excludes the
    // (summarize) pending note even though it still matches the search text.
    await userEvent.selectOptions(screen.getByLabelText(/type filter/i), "transcribe");
    expect(screen.queryByText("note-pending")).not.toBeInTheDocument();
  });

  it("clicking View timeline fetches and renders the note's pipeline, including an errored stage", async () => {
    const transcribeDone: Job = {
      id: "t1",
      note_id: "note-pipeline",
      type: "transcribe",
      status: "done",
      attempts: 1,
      last_error: null,
      priority: 0,
      started_at: "2026-07-05T10:00:00Z",
      finished_at: "2026-07-05T10:03:04Z",
    };
    const summarizeFailed: Job = {
      id: "s1",
      note_id: "note-pipeline",
      type: "summarize",
      status: "failed",
      attempts: 2,
      last_error: "agent timeout",
      priority: 0,
      started_at: "2026-07-05T10:03:10Z",
      finished_at: "2026-07-05T10:03:40Z",
    };
    const embedPending: Job = {
      id: "e1",
      note_id: "note-pipeline",
      type: "embed",
      status: "pending",
      attempts: 0,
      last_error: null,
      priority: 0,
      started_at: null,
      finished_at: null,
    };
    const rowJob: Job = { ...doneJob, note_id: "note-pipeline" };
    const client = makeClient([rowJob], [transcribeDone, summarizeFailed, embedPending]);
    render(<JobsView client={client as never} />);
    await screen.findByText("note-pipeline");

    await userEvent.click(screen.getByRole("button", { name: /view timeline/i }));

    expect(client.listNoteJobs).toHaveBeenCalledWith("note-pipeline");
    await screen.findByText("agent timeout");
    expect(screen.getByText(/3m 4s/)).toBeInTheDocument();
    expect(screen.queryByText(/in progress/)).not.toBeInTheDocument();
    expect(screen.getByText(/duration: —/)).toBeInTheDocument();

    const timeline = screen.getByRole("region", { name: /pipeline timeline/i });
    const badges = within(timeline).getAllByText("failed");
    expect(badges.length).toBeGreaterThan(0);
  });
});
