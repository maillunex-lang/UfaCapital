import { useState, type FormEvent } from "react";
import { api } from "./api";
import { message } from "./useLoad";

export function Login({ onDone }: { onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [mfa, setMfa] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (!mfa) {
        const r = await api.login(email, password);
        setMfa(r.mfa_token);
        setPassword("");
      } else {
        await api.totp(mfa, code);
        onDone();
      }
    } catch (err) {
      setError(message(err));
      setCode("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="center">
      <form className="card" onSubmit={submit}>
        <h1>Вход в админку</h1>
        {!mfa ? (
          <>
            <label>
              Почта
              <input type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
            </label>
            <label>
              Пароль
              <input type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            </label>
          </>
        ) : (
          <label>
            Код из приложения (6 цифр)
            <input inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required autoFocus value={code} onChange={(e) => setCode(e.target.value)} />
          </label>
        )}
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy}>
          {mfa ? "Подтвердить" : "Дальше"}
        </button>
      </form>
    </main>
  );
}
