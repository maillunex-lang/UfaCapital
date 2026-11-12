import { Component, type ReactNode } from "react";

export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (this.state.failed) {
      return (
        <main className="center">
          <p role="alert">Страница сломалась. Обновите её; несохранённых изменений на сервере нет.</p>
        </main>
      );
    }
    return this.props.children;
  }
}
