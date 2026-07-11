import { FormEvent, useState } from "react";

interface Props {
  onSubmit: (email: string, password: string) => Promise<void>;
}

export function LoginView({ onSubmit }: Props) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const canSubmit = email.length > 0 && password.length > 0 && !busy;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await onSubmit(email, password);
    } catch (err) {
      setError(err instanceof Error ? err.message : "login failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="shell" onSubmit={handleSubmit}>
      <h1>Muesli Admin — sign in</h1>
      <label htmlFor="login-email">Email</label>
      <input id="login-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
      <label htmlFor="login-password">Password</label>
      <input
        id="login-password"
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
      />
      {error && <p className="error">{error}</p>}
      <p>
        <button type="submit" disabled={!canSubmit}>
          Sign in
        </button>
      </p>
    </form>
  );
}
