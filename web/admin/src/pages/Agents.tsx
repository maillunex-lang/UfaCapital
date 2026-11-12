import { useState } from "react";
import { api, type Agent } from "../api";
import { message, useLoad } from "../useLoad";
import { Section } from "./shared";

function Row({ a, onChanged }: { a: Agent; onChanged: () => void }) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function toggle() {
    setBusy(true);
    setError("");
    try {
      await api.pause(a.id, !a.paused, reason.trim());
      setReason("");
      onChanged();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <tr>
      <td>
        {a.name} <span className="badge">ИИ</span>
      </td>
      <td>{a.profile}</td>
      <td>{a.specialization}</td>
      <td className="num">{a.loss_streak}</td>
      <td>{a.paused ? "на паузе" : "работает"}</td>
      <td>
        <input aria-label={`Причина для ${a.name}`} placeholder="причина" value={reason} onChange={(e) => setReason(e.target.value)} />
        <button type="button" disabled={busy || reason.trim().length < 4} onClick={toggle}>
          {a.paused ? "Снять паузу" : "Поставить на паузу"}
        </button>
        {error && (
          <span className="error" role="alert">
            {error}
          </span>
        )}
      </td>
    </tr>
  );
}

export function Agents() {
  const load = useLoad(api.agents);
  return (
    <Section title="ИИ-участники" load={load}>
      {(rows) => (
        <table>
          <thead>
            <tr>
              <th>Имя</th>
              <th>Профиль</th>
              <th>Специализация</th>
              <th className="num">Убытков подряд</th>
              <th>Состояние</th>
              <th>Пауза (причина попадёт в журнал)</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((a) => (
              <Row key={a.id} a={a} onChanged={load.reload} />
            ))}
          </tbody>
        </table>
      )}
    </Section>
  );
}
