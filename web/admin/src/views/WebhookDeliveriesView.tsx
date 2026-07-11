import { useCallback, useEffect, useState } from "react";
import type { ApiClient } from "../api/client";
import type { WebhookDelivery } from "../api/types";

interface Props {
  client: Pick<ApiClient, "listWebhookDeliveries" | "retryWebhookDelivery">;
}

// Deliveries in these states can't be usefully retried right now: `delivered`
// is already done (retrying is a no-op the UI shouldn't invite), and
// `pending`/`in_flight` are already queued or being processed by the
// background worker (retrying would 409). Only `failed` is retryable.
function isRetryable(status: WebhookDelivery["status"]): boolean {
  return status === "failed";
}

export function WebhookDeliveriesView({ client }: Props) {
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [retrying, setRetrying] = useState<Record<string, boolean>>({});
  const [retryErrors, setRetryErrors] = useState<Record<string, string>>({});

  const refresh = useCallback(async () => {
    try {
      setDeliveries(await client.listWebhookDeliveries());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load webhook deliveries");
    }
  }, [client]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const handleRetry = useCallback(
    async (delivery: WebhookDelivery) => {
      setRetrying((prev) => ({ ...prev, [delivery.id]: true }));
      setRetryErrors((prev) => {
        const next = { ...prev };
        delete next[delivery.id];
        return next;
      });
      try {
        await client.retryWebhookDelivery(delivery.id);
        await refresh();
      } catch (err) {
        setRetryErrors((prev) => ({
          ...prev,
          [delivery.id]: err instanceof Error ? err.message : "retry failed",
        }));
      } finally {
        setRetrying((prev) => ({ ...prev, [delivery.id]: false }));
      }
    },
    [client, refresh],
  );

  return (
    <section>
      <div className="row">
        <h1>Webhook deliveries</h1>
        <button onClick={() => void refresh()}>Refresh</button>
      </div>
      {error && <p className="error">{error}</p>}
      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>Webhook</th>
            <th>Status</th>
            <th>Attempts</th>
            <th>Last error</th>
            <th>Next attempt</th>
            <th>Created</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {deliveries.map((d) => (
            <tr key={d.id}>
              <td className="mono-id" title={d.id}>
                {d.id}
              </td>
              <td>{d.webhook_id}</td>
              <td>{d.status}</td>
              <td>
                {d.attempts}/{d.max_attempts}
              </td>
              <td>{d.last_error || "—"}</td>
              <td>{d.next_attempt_at ?? "—"}</td>
              <td>{d.created_at}</td>
              <td>
                {isRetryable(d.status) && (
                  <>
                    <button disabled={retrying[d.id]} onClick={() => void handleRetry(d)}>
                      {retrying[d.id] ? "Retrying…" : "Retry"}
                    </button>
                    {retryErrors[d.id] && <span style={{ color: "red" }}>{retryErrors[d.id]}</span>}
                  </>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
