import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BackupsView } from "./BackupsView";
import type { BackupInfo } from "../api/types";

function makeClient(backups: BackupInfo[]) {
  return {
    listBackups: vi.fn().mockResolvedValue(backups),
    createBackup: vi.fn().mockResolvedValue({}),
    downloadBackup: vi.fn().mockResolvedValue(undefined),
    verifyBackup: vi.fn().mockResolvedValue({ ok: true, table_count: 5, size_bytes: 2048 }),
  };
}

const backup: BackupInfo = {
  filename: "muesli-20240101120000.dump",
  size_bytes: 2048,
  created_at: "2024-01-01T12:00:00Z",
};

describe("BackupsView", () => {
  it("lists backups fetched from the client", async () => {
    const client = makeClient([backup]);
    render(<BackupsView client={client as never} />);
    expect(await screen.findByText("muesli-20240101120000.dump")).toBeInTheDocument();
    expect(client.listBackups).toHaveBeenCalled();
  });

  it("shows a human-readable size", async () => {
    const client = makeClient([backup]);
    render(<BackupsView client={client as never} />);
    await screen.findByText("muesli-20240101120000.dump");
    expect(screen.getByText("2.0 KB")).toBeInTheDocument();
  });

  it("shows 'No backups yet.' when the list is empty", async () => {
    const client = makeClient([]);
    render(<BackupsView client={client as never} />);
    expect(await screen.findByText(/no backups yet/i)).toBeInTheDocument();
  });

  it("runs a backup via createBackup and refreshes the list", async () => {
    const client = makeClient([]);
    client.createBackup.mockImplementation(async () => {
      client.listBackups.mockResolvedValue([backup]);
      return {};
    });
    render(<BackupsView client={client as never} />);
    await screen.findByText(/no backups yet/i);

    await userEvent.click(screen.getByRole("button", { name: /run backup now/i }));

    await waitFor(() => expect(client.createBackup).toHaveBeenCalled());
    expect(await screen.findByText("muesli-20240101120000.dump")).toBeInTheDocument();
  });

  it("downloads a backup via downloadBackup", async () => {
    const client = makeClient([backup]);
    render(<BackupsView client={client as never} />);
    await screen.findByText("muesli-20240101120000.dump");

    // Click the first "Download" button (in the row actions)
    const downloadButtons = screen.getAllByRole("button", { name: /download/i });
    await userEvent.click(downloadButtons[0]);

    await waitFor(() => expect(client.downloadBackup).toHaveBeenCalledWith("muesli-20240101120000.dump"));
  });

  it("shows an error message when listBackups fails", async () => {
    const client = {
      listBackups: vi.fn().mockRejectedValue(new Error("backups are not configured")),
      createBackup: vi.fn(),
      downloadBackup: vi.fn(),
      verifyBackup: vi.fn(),
    };
    render(<BackupsView client={client as never} />);
    expect(await screen.findByText(/backups are not configured/i)).toBeInTheDocument();
  });

  it("shows an error message when createBackup fails", async () => {
    const client = makeClient([]);
    client.createBackup.mockRejectedValue(new Error("backup failed"));
    render(<BackupsView client={client as never} />);
    await screen.findByText(/no backups yet/i);

    await userEvent.click(screen.getByRole("button", { name: /run backup now/i }));

    expect(await screen.findByText(/backup failed/i)).toBeInTheDocument();
  });

  it("clicking Verify calls verifyBackup and renders the ok badge with table count", async () => {
    const client = makeClient([backup]);
    client.verifyBackup.mockResolvedValue({ ok: true, table_count: 3, size_bytes: 2048 });
    render(<BackupsView client={client as never} />);
    await screen.findByText("muesli-20240101120000.dump");

    await userEvent.click(screen.getByRole("button", { name: /^verify$/i }));

    await waitFor(() => expect(client.verifyBackup).toHaveBeenCalledWith("muesli-20240101120000.dump"));
    expect(await screen.findByText("OK (3 tables)")).toBeInTheDocument();
  });

  it("a failed verify renders the error badge", async () => {
    const client = makeClient([backup]);
    client.verifyBackup.mockResolvedValue({ ok: false, error: "corrupt archive", size_bytes: 2048, table_count: 0 });
    render(<BackupsView client={client as never} />);
    await screen.findByText("muesli-20240101120000.dump");

    await userEvent.click(screen.getByRole("button", { name: /^verify$/i }));

    await waitFor(() => expect(client.verifyBackup).toHaveBeenCalled());
    expect(await screen.findByText("corrupt archive")).toBeInTheDocument();
  });

  it("selecting a backup renders the Restore procedure panel with the real filename", async () => {
    const client = makeClient([backup]);
    render(<BackupsView client={client as never} />);
    await screen.findByText("muesli-20240101120000.dump");

    await userEvent.click(screen.getByRole("button", { name: /restore…/i }));

    const panel = screen.getByTestId("restore-panel");
    expect(panel).toBeInTheDocument();
    expect(panel).toHaveTextContent("pg_restore -U postgres -d muesli --clean --if-exists muesli-20240101120000.dump");
    expect(panel).toHaveTextContent("Stop the Muesli server process");
    expect(panel).toHaveTextContent("Start the server again");
  });

  it("restore panel Download button calls downloadBackup for the selected backup", async () => {
    const client = makeClient([backup]);
    render(<BackupsView client={client as never} />);
    await screen.findByText("muesli-20240101120000.dump");

    await userEvent.click(screen.getByRole("button", { name: /restore…/i }));

    // The panel has its own Download button
    const downloadButtons = screen.getAllByRole("button", { name: /download/i });
    // The last one is in the restore panel
    await userEvent.click(downloadButtons[downloadButtons.length - 1]);

    await waitFor(() => expect(client.downloadBackup).toHaveBeenCalledWith("muesli-20240101120000.dump"));
  });
});
