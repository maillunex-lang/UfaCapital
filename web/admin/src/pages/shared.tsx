import type { ReactNode } from "react";
import type { Loaded } from "../useLoad";

export function Section<T>({ title, load, children }: { title: string; load: Loaded<T>; children: (data: T) => ReactNode }) {
  return (
    <section>
      <div className="head">
        <h1>{title}</h1>
        <button type="button" onClick={load.reload}>
          Обновить
        </button>
      </div>
      {load.error && (
        <p className="error" role="alert">
          {load.error}
        </p>
      )}
      {load.data === null ? !load.error && <p>Загрузка…</p> : children(load.data)}
    </section>
  );
}

/** Amounts arrive as decimal strings; group digits without converting to a float. */
export function money(s: string): string {
  return s.replace(/\B(?=(\d{3})+(?!\d))/g, " ");
}

export function when(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString("ru-RU");
}
