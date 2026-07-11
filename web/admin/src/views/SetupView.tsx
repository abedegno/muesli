import { FormEvent, useState } from "react";

interface Props {
  onSubmit: (email: string, password: string) => Promise<void>;
}

export function SetupView({ onSubmit }: Props) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const canSubmit = email.length > 0 && password.length >= 8 && !busy;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await onSubmit(email, password);
    } catch (err) {
      setError(err instanceof Error ? err.message : "setup failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="shell" onSubmit={handleSubmit}>
      <h1>Welcome to Muesli</h1>
      <p className="setup-desc">Create your operator account to get started.</p>
      <label htmlFor="setup-email">Email</label>
      <input id="setup-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
      <label htmlFor="setup-password">Password (min 8 characters)</label>
      <input
        id="setup-password"
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
      />
      {error && <p className="error">{error}</p>}
      <p>
        <button type="submit" disabled={!canSubmit}>
          Create account
        </button>
      </p>
    </form>
  );
}
