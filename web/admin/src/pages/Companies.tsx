import { api } from "../api";
import { useLoad } from "../useLoad";
import { Section, money } from "./shared";

export function Companies() {
  const load = useLoad(api.companies);
  return (
    <Section title="Компании" load={load}>
      {(rows) => (
        <table>
          <thead>
            <tr>
              <th>Название</th>
              <th>Владелец</th>
              <th>Статус</th>
              <th className="num">Доступно, ₽</th>
              <th className="num">В резерве, ₽</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((c) => (
              <tr key={c.id}>
                <td>{c.name}</td>
                <td>{c.owner_kind === "ai" ? <span className="badge">ИИ</span> : c.owner_kind}</td>
                <td>{c.status}</td>
                <td className="num">{money(c.cash)}</td>
                <td className="num">{money(c.reserved)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Section>
  );
}
