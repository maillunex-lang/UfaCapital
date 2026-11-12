import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type Me } from "./api";
import { Login } from "./Login";
import { Players } from "./pages/Players";
import { Companies } from "./pages/Companies";
import { Agents } from "./pages/Agents";
import { Config } from "./pages/Config";
import { Audit } from "./pages/Audit";

const TABS = [
  { id: "players", title: "Игроки" },
  { id: "companies", title: "Компании" },
  { id: "agents", title: "ИИ" },
  { id: "config", title: "Настройки" },
  { id: "audit", title: "Журнал" },
] as const;
type TabId = (typeof TABS)[number]["id"];

export function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [checked, setChecked] = useState(false);
  const [down, setDown] = useState(false);
  const [tab, setTab] = useState<TabId>("players");

  const check = useCallback(() => {
    api.me().then(
      (m) => {
        setMe(m);
        setChecked(true);
        setDown(false);
      },
      (e: unknown) => {
        setMe(null);
        setChecked(true);
        setDown(e instanceof ApiError && e.status === 0);
      },
    );
  }, []);

  useEffect(check, [check]);

  const logout = useCallback(() => {
    api.logout().finally(() => setMe(null));
  }, []);

  if (!checked) return <main className="center">Загрузка…</main>;
  if (!me) {
    return (
      <>
        {down && (
          <p className="error center" role="alert">
            Нет связи с сервером.
          </p>
        )}
        <Login onDone={check} />
      </>
    );
  }

  return (
    <>
      <header className="top">
        <strong>UFACAPITAL</strong>
        <nav aria-label="Разделы">
          {TABS.map((t) => (
            <button key={t.id} type="button" className={t.id === tab ? "tab on" : "tab"} aria-current={t.id === tab ? "page" : undefined} onClick={() => setTab(t.id)}>
              {t.title}
            </button>
          ))}
        </nav>
        <span className="who">
          {me.display_name} · {me.roles.filter((r) => r !== "player").join(", ")}
        </span>
        <button type="button" onClick={logout}>
          Выйти
        </button>
      </header>
      <main>
        {tab === "players" && <Players me={me} />}
        {tab === "companies" && <Companies />}
        {tab === "agents" && <Agents />}
        {tab === "config" && <Config />}
        {tab === "audit" && <Audit />}
      </main>
    </>
  );
}
