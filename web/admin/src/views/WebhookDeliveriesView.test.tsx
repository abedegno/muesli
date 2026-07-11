import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WebhookDeliveriesView } from "./WebhookDeliveriesView";
import type { WebhookDelivery } from "../api/types";

function makeClient(deliveries: WebhookDelivery[]) {
  return {
    listWebhookDeliveries: vi.fn().mockResolvedValue(deliveries),
    retryWebhookDelivery: vi.fn().mockResolvedValue({ status: "queued" }),
  };
}

const failed: WebhookDelivery = {
  id: "d1",
  webhook_id: "w1",
  status: "failed",
  attempts: 5,
  max_attempts: 5,
  last_error: "connection refused",
  next_attempt_at: null,
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:05:00Z",
};

const delivered: WebhookDelivery = {
  id: "d2",
  webhook_id: "w1",
  status: "delivered",
  attempts: 1,
  max_attempts: 5,
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:01:00Z",
};

const pending: WebhookDelivery = {
  id: "d3",
  webhook_id: "w1",
  status: "pending",
  attempts: 0,
  max_attempts: 5,
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
};

describe("WebhookDeliveriesView", () => {
  it("lists deliveries fetched from the client", async () => {
    const client = makeClient([failed]);
    render(<WebhookDeliveriesView client={client as never} />);
    expect(await screen.findByText("w1")).toBeInTheDocument();
    expect(screen.getByText("failed")).toBeInTheDocument();
    expect(screen.getByText("connection refused")).toBeInTheDocument();
    expect(client.listWebhookDeliveries).toHaveBeenCalled();
  });

  it("renders the delivery id so an operator can identify a specific row", async () => {
    const client = makeClient([failed]);
    render(<WebhookDeliveriesView client={client as never} />);
    await screen.findByText("w1");
    expect(screen.getByText("d1")).toBeInTheDocument();
  });

  it("shows a Retry button for a failed delivery", async () => {
    const client = makeClient([failed]);
    render(<WebhookDeliveriesView client={client as never} />);
    await screen.findByText("w1");
    expect(screen.getByRole("button", { name: /^retry$/i })).toBeInTheDocument();
  });

  it("hides the Retry button for a delivered delivery", async () => {
    const client = makeClient([delivered]);
    render(<WebhookDeliveriesView client={client as never} />);
    await screen.findByText("delivered");
    expect(screen.queryByRole("button", { name: /^retry$/i })).not.toBeInTheDocument();
  });

  it("hides the Retry button for a pending delivery", async () => {
    const client = makeClient([pending]);
    render(<WebhookDeliveriesView client={client as never} />);
    await screen.findByText("pending");
    expect(screen.queryByRole("button", { name: /^retry$/i })).not.toBeInTheDocument();
  });

  it("clicking Retry on a failed row calls the retry endpoint and refreshes to reflect the updated status", async () => {
    const client = makeClient([failed]);
    // After the retry call, a follow-up refresh shows the delivery back as pending.
    client.listWebhookDeliveries
      .mockResolvedValueOnce([failed])
      .mockResolvedValueOnce([{ ...failed, status: "pending", attempts: 0, last_error: undefined }]);

    render(<WebhookDeliveriesView client={client as never} />);
    await screen.findByText("failed");

    await userEvent.click(screen.getByRole("button", { name: /^retry$/i }));

    await waitFor(() => expect(client.retryWebhookDelivery).toHaveBeenCalledWith("d1"));
    await waitFor(() => expect(screen.getByText("pending")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /^retry$/i })).not.toBeInTheDocument();
  });

  it("surfaces a retry error next to the row without crashing", async () => {
    const client = makeClient([failed]);
    client.retryWebhookDelivery.mockRejectedValueOnce(new Error("already queued or in progress"));
    render(<WebhookDeliveriesView client={client as never} />);
    await screen.findByText("w1");

    await userEvent.click(screen.getByRole("button", { name: /^retry$/i }));

    expect(await screen.findByText(/already queued or in progress/i)).toBeInTheDocument();
  });
});
