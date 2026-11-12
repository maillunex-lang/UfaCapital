import { useState } from "react";
import { api, type ConfigItem } from "../api";
import { message, useLoad } from "../useLoad";
import { Section, when } from "./shared";

function Row({ item, onChanged }: { item: ConfigItem; onChanged: () => void }) {
  const [value, setValue] = useState(item.value);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function save() {
    setBusy(true);
    setError("");
    try {
      // expected_version makes the server refuse the write if someone changed the value first.
      await api.setConfig(item.key, value.trim(), item.version);
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
        <code>{item.key}</code>
      </td>
      <td className="num">{item.version}</td>
      <td>{when(item.effective_at)}</td>
      <td>
        <input aria-label={`Значение ${item.key}`} value={value} onChange={(e) => setValue(e.target.value)} />
        <button type="button" disabled={busy || value.trim() === item.value || value.trim() === ""} onClick={save}>
          Сохранить
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

export function Config() {
  const load = useLoad(api.config);
  return (
    <Section title="Настройки экономики" load={load}>
      {(rows) => (
        <>
          <p className="note">Каждое сохранение создаёт новую версию и строку в журнале. Суммы — целые рубли.</p>
          <table>
            <thead>
              <tr>
                <th>Ключ</th>
                <th className="num">Версия</th>
                <th>Действует с</th>
                <th>Значение</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                // The version in the key resets the input after a successful save.
                <Row key={c.key + ":" + c.version} item={c} onChanged={load.reload} />
              ))}
            </tbody>
          </table>
        </>
      )}
    </Section>
  );
}
