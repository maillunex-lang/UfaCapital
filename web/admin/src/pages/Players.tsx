import { useState } from "react";
import { api, ROLES, type Me, type Player } from "../api";
import { message, useLoad } from "../useLoad";
import { Section } from "./shared";

function Editor({ p, canRoles, onDone }: { p: Player; canRoles: boolean; onDone: () => void }) {
  const [roles, setRoles] = useState<string[]>(p.roles);
  const [hours, setHours] = useState("24");
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const okReason = reason.trim().length >= 4;

  async function run(fn: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await fn();
      onDone();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }

  function flip(role: string) {
    setRoles((cur) => (cur.includes(role) ? cur.filter((r) => r !== role) : [...cur, role]));
  }

  return (
    <div className="editor">
      <label>
        Причина (попадёт в журнал)
        <input value={reason} onChange={(e) => setReason(e.target.value)} />
      </label>
      <fieldset>
        <legend>VIP</legend>
        <label>
          Часов
          <input inputMode="numeric" pattern="[0-9]+" value={hours} onChange={(e) => setHours(e.target.value)} />
        </label>
        <button type="button" disabled={busy || !okReason || !/^[1-9]\d*$/.test(hours)} onClick={() => run(() => api.vip(p.id, hours, reason.trim()))}>
          Выдать VIP
        </button>
      </fieldset>
      {canRoles && (
        <fieldset>
          <legend>Роли</legend>
          {ROLES.map((r) => (
            <label key={r} className="check">
              <input type="checkbox" checked={roles.includes(r)} onChange={() => flip(r)} /> {r}
            </label>
          ))}
          <button type="button" disabled={busy || !okReason} onClick={() => run(() => api.setRoles(p.id, roles, reason.trim()))}>
            Сохранить роли
          </button>
        </fieldset>
      )}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

export function Players({ me }: { me: Me }) {
  const load = useLoad(api.players);
  const [open, setOpen] = useState("");
  const superadmin = me.roles.includes("superadmin");

  return (
    <Section title="Игроки (последние 100)" load={load}>
      {(rows) => (
        <table>
          <thead>
            <tr>
              <th>Имя</th>
              <th>Почта</th>
              <th>Роли</th>
              <th>Состояние</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {rows.map((p) => (
              <tr key={p.id}>
                <td>
                  {p.display_name} {p.is_ai && <span className="badge">ИИ</span>}
                </td>
                <td>{p.email}</td>
                <td>{p.roles.join(", ")}</td>
                <td>{p.blocked ? "заблокирован" : "активен"}</td>
                <td>
                  <button type="button" aria-expanded={open === p.id} onClick={() => setOpen(open === p.id ? "" : p.id)}>
                    {open === p.id ? "Закрыть" : "Изменить"}
                  </button>
                  {open === p.id && (
                    <Editor
                      p={p}
                      canRoles={superadmin && p.id !== me.id}
                      onDone={() => {
                        setOpen("");
                        load.reload();
                      }}
                    />
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Section>
  );
}
