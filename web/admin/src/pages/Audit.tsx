import { api } from "../api";
import { useLoad } from "../useLoad";
import { Section, when } from "./shared";

function show(v: unknown): string {
  const s = JSON.stringify(v);
  return s === undefined || s === "{}" || s === "null" ? "—" : s;
}

export function Audit() {
  const load = useLoad(api.audit);
  return (
    <Section title="Журнал действий (последние 200)" load={load}>
      {(rows) => (
        <table>
          <thead>
            <tr>
              <th>Когда</th>
              <th>Кто</th>
              <th>Действие</th>
              <th>Причина</th>
              <th>Было</th>
              <th>Стало</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id}>
                <td>{when(r.at)}</td>
                <td>{r.actor}</td>
                <td>{r.action}</td>
                <td>{r.reason || "—"}</td>
                <td className="json">{show(r.before)}</td>
                <td className="json">{show(r.after)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Section>
  );
}
