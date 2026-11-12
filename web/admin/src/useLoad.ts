import { useCallback, useEffect, useState } from "react";
import { ApiError } from "./api";

export interface Loaded<T> {
  data: T | null;
  error: string;
  reload: () => void;
}

/** Loads once on mount and again on reload(); a late answer after unmount is dropped. */
export function useLoad<T>(fn: () => Promise<T>): Loaded<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [n, setN] = useState(0);

  useEffect(() => {
    let alive = true;
    fn().then(
      (d) => {
        if (alive) {
          setData(d);
          setError("");
        }
      },
      (e: unknown) => {
        if (alive) setError(e instanceof ApiError ? e.message : "не удалось загрузить");
      },
    );
    return () => {
      alive = false;
    };
    // fn is a stable module-level function at every call site.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [n]);

  const reload = useCallback(() => setN((x) => x + 1), []);
  return { data, error, reload };
}

export function message(e: unknown): string {
  return e instanceof ApiError ? e.message : "не удалось выполнить";
}
