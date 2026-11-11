const app = document.querySelector("#app");
const titles = { grain: "Зерно", flour: "Мука", bread: "Хлеб", mill: "Мельница", bakery: "Пекарня", shop: "Магазин", warehouse: "Склад" };
let token = localStorage.getItem("uc_token") || "";
let refresh = localStorage.getItem("uc_refresh") || "";
let mode = "login";
let notice = "";
let bad = false;
let board = null;
let market = [];
let busy = false;

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text != null) n.textContent = text;
  return n;
}
function field(label, input) {
  const l = el("label");
  l.append(document.createTextNode(label), input);
  return l;
}
function money(n) {
  const s = String(n ?? "0");
  return s.replace(/\B(?=(\d{3})+(?!\d))/g, " ");
}
function setNotice(text, isBad) {
  notice = text || "";
  bad = !!isBad;
  render();
}
async function api(path, opts = {}) {
  const headers = { ...(opts.headers || {}) };
  if (token) headers.Authorization = "Bearer " + token;
  if (opts.body) {
    headers["Content-Type"] = "application/json";
    headers["Idempotency-Key"] = crypto.randomUUID();
  }
  const res = await fetch(path, { ...opts, headers, body: opts.body ? JSON.stringify(opts.body) : undefined });
  const json = await res.json().catch(() => ({}));
  if (res.status === 401 && refresh && path !== "/api/v1/auth/refresh") {
    const again = await fetch("/api/v1/auth/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refresh }),
    });
    const body = await again.json().catch(() => ({}));
    if (again.ok && body.data?.access_token) {
      token = body.data.access_token;
      refresh = body.data.refresh_token;
      localStorage.setItem("uc_token", token);
      localStorage.setItem("uc_refresh", refresh);
      return api(path, opts);
    }
  }
  if (!res.ok) {
    const msg = json.error?.message || "запрос не выполнен";
    const err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  return json.data;
}
async function refreshBoard() {
  if (!token) return;
  try {
    const [b, m] = await Promise.all([
      api("/api/v1/companies/board"),
      api("/api/v1/market"),
    ]);
    board = b;
    market = Array.isArray(m) ? m : [];
    render();
  } catch (err) {
    if (err.status === 401) logout();
    else setNotice(err.message, true);
  }
}
function logout() {
  token = "";
  refresh = "";
  board = null;
  localStorage.removeItem("uc_token");
  localStorage.removeItem("uc_refresh");
  render();
}
async function act(fn) {
  if (busy) return;
  busy = true;
  render();
  try {
    await fn();
    notice = "Готово. Сервер принял команду.";
    bad = false;
    await refreshBoard();
  } catch (err) {
    setNotice(err.message, true);
  } finally {
    busy = false;
    render();
  }
}

function authView() {
  const box = el("div", "auth");
  const card = el("section", "card");
  card.append(el("p", "brand", "ДЕЛОВАЯ РЕСПУБЛИКА"), el("h1", "", "UFACAPITAL"));
  card.append(el("p", "sub", "Общий рынок Уфы. Деньги и товары считает только сервер."));
  const tabs = el("div", "tabs");
  const a = el("button", mode === "login" ? "primary" : "ghost", "Вход");
  const b = el("button", mode === "register" ? "primary" : "ghost", "Регистрация");
  a.onclick = () => { mode = "login"; render(); };
  b.onclick = () => { mode = "register"; render(); };
  tabs.append(a, b);
  const form = el("form", "form");
  const email = el("input"); email.type = "email"; email.required = true; email.autocomplete = "username"; email.placeholder = "you@example.com";
  const pass = el("input"); pass.type = "password"; pass.required = true; pass.minLength = 12; pass.autocomplete = mode === "login" ? "current-password" : "new-password";
  form.append(field("Почта", email), field("Пароль, от 12 символов", pass));
  let name;
  if (mode === "register") {
    name = el("input"); name.required = true; name.maxLength = 80; name.placeholder = "Как к вам обращаться";
    form.append(field("Имя", name));
  }
  const submit = el("button", "primary", mode === "login" ? "Войти" : "Создать счёт");
  submit.type = "submit";
  form.append(submit);
  form.onsubmit = (ev) => {
    ev.preventDefault();
    act(async () => {
      const path = mode === "login" ? "/api/v1/auth/login" : "/api/v1/auth/register";
      const body = { email: email.value, password: pass.value };
      if (name) body.display_name = name.value;
      const data = await api(path, { method: "POST", body });
      token = data.access_token;
      refresh = data.refresh_token;
      localStorage.setItem("uc_token", token);
      localStorage.setItem("uc_refresh", refresh);
    });
  };
  card.append(tabs, form);
  if (notice) card.append(el("p", bad ? "notice bad" : "notice", notice));
  box.append(card);
  return box;
}

function gameView() {
  const c = board?.company;
  const shell = el("main", "shell");
  const top = el("header", "top");
  const left = el("div");
  left.append(el("p", "brand", "УФА · ОБЩИЙ РЫНОК"), el("h1", "", c ? c.name : "Новая компания"));
  left.append(el("p", "sub", c ? "Счёт компании ведётся на сервере. ИИ играют теми же командами." : "Стартовый грант 100 000 — один раз, через серверную проводку."));
  const metrics = el("div", "metrics");
  metrics.append(metric("Тик", board?.tick || "0"), metric("Касса", c ? money(c.cash) : "—"), metric("Резерв", c ? money(c.reserved) : "—"));
  const out = el("button", "ghost", "Выйти");
  out.onclick = () => logout();
  metrics.append(out);
  top.append(left, metrics);
  shell.append(top);

  const grid = el("div", "grid");
  grid.append(plants(c), marketCard(c));
  shell.append(grid);
  if (notice) shell.append(el("p", bad ? "notice bad" : "notice", notice));
  return shell;
}

function metric(k, v) {
  const n = el("div", "metric");
  n.append(el("span", "", k), el("strong", "", v));
  return n;
}
function plants(c) {
  const card = el("section", "card");
  card.append(el("h2", "", "Предприятия"));
  if (!c) {
    const form = el("form", "form");
    const name = el("input"); name.required = true; name.maxLength = 80; name.placeholder = "Мельница Юлдаш";
    const go = el("button", "primary", "Открыть компанию"); go.type = "submit";
    form.append(field("Название", name), go);
    form.onsubmit = (ev) => {
      ev.preventDefault();
      act(() => api("/api/v1/companies", { method: "POST", body: { name: name.value } }));
    };
    card.append(form);
    return card;
  }
  const owned = new Set((board.enterprises || []).map((e) => e.template));
  for (const e of board.enterprises || []) {
    const row = el("article", "plant");
    const info = el("div");
    const title = el("b", "", e.title || titles[e.template] || e.template);
    const p = el("p");
    const st = el("span", e.status === "running" ? "tag on" : "tag", e.status === "running" ? "работает" : "стоит");
    p.append(st, document.createTextNode(" штат " + e.headcount + " · зарплата " + money(e.wage) + (e.pledged ? " · в залоге" : "")));
    info.append(title, p);
    const actions = el("div", "row");
    if (e.template !== "warehouse" && e.status !== "running" && !e.pledged) {
      const run = el("button", "primary", "Пуск");
      run.onclick = () => act(async () => {
        await api("/api/v1/enterprises/" + e.id + "/workforce", { method: "POST", body: { headcount: "2", wage: "15" } });
        await api("/api/v1/enterprises/" + e.id + "/policy", { method: "POST", body: { running: true } });
      });
      actions.append(run);
    } else if (e.status === "running") {
      const stop = el("button", "ghost", "Стоп");
      stop.onclick = () => act(() => api("/api/v1/enterprises/" + e.id + "/policy", { method: "POST", body: { running: false } }));
      actions.append(stop);
    }
    if (!e.pledged && e.template !== "warehouse") {
      const loan = el("button", "copper", "Залог");
      loan.onclick = () => act(() => api("/api/v1/loans", { method: "POST", body: { company_id: c.id, enterprise_id: e.id, amount: "10000" } }));
      actions.append(loan);
    }
    row.append(info, actions);
    card.append(row);
  }
  const open = el("div", "row");
  for (const code of ["mill", "bakery", "shop"]) {
    if (owned.has(code)) continue;
    const b = el("button", "ghost", "Открыть: " + titles[code]);
    b.onclick = () => act(() => api("/api/v1/enterprises", { method: "POST", body: { company_id: c.id, template: code } }));
    open.append(b);
  }
  if (open.childNodes.length) card.append(open);
  card.append(stockBlock(c));
  return card;
}
function stockBlock(c) {
  const wrap = el("div");
  wrap.append(el("h2", "", "Склад и сделки"));
  const stock = board.stock || [];
  if (!stock.length) wrap.append(el("p", "sub", "Склад пуст. Купите зерно у внешнего контура или дождитесь выпуска."));
  for (const s of stock) {
    wrap.append(el("p", "", (titles[s.good] || s.good) + ": " + s.qty + " (резерв " + s.reserved + ")"));
  }
  const buy = el("button", "primary", "Купить 10 зерна по лучшей цене");
  buy.onclick = () => act(async () => {
    const offer = market.find((o) => o.good === "grain" && o.side === "sell");
    if (!offer) throw new Error("на рынке нет зерна");
    const total = String(Number(offer.price) * 10);
    await api("/api/v1/market/orders/" + offer.order_id + "/accept", { method: "POST", body: { qty: "10", max_total: total } });
  });
  const sell = el("button", "copper", "Продать 8 муки по 70");
  sell.onclick = () => act(() => api("/api/v1/market/orders", { method: "POST", body: { company_id: c.id, good: "flour", side: "sell", price: "70", qty: "8" } }));
  const contract = el("button", "ghost", "Контракт: 10 муки");
  contract.onclick = () => act(() => api("/api/v1/contracts", {
    method: "POST",
    body: { company_id: c.id, good: "flour", qty: "10", price: "70", deadline_tick: String(Number(board.tick) + 12) },
  }));
  wrap.append(el("div", "row", null));
  const row = el("div", "row");
  row.append(buy, sell, contract);
  wrap.append(row);
  for (const offer of board.offers || []) {
    const line = el("div", "row");
    line.append(el("span", "", (offer.seller_name || "продавец") + " · " + (titles[offer.good] || offer.good) + " × " + offer.qty));
    const ok = el("button", "primary", "Принять");
    ok.onclick = () => act(() => api("/api/v1/contracts/" + offer.id + "/accept", { method: "POST", body: {} }));
    line.append(ok);
    wrap.append(line);
  }
  for (const loan of board.loans || []) {
    const line = el("div", "row");
    line.append(el("span", "", "Кредит " + money(loan.outstanding) + " · " + loan.status));
    const pay = el("button", "ghost", "Погасить");
    pay.onclick = () => act(() => api("/api/v1/loans/" + loan.id + "/repay", { method: "POST", body: { amount: loan.outstanding } }));
    line.append(pay);
    wrap.append(line);
  }
  return wrap;
}
function marketCard() {
  const card = el("section", "card");
  card.append(el("h2", "", "Книга заявок"));
  const table = el("table", "book");
  const head = el("tr");
  for (const h of ["Товар", "Сторона", "Цена", "Объём"]) head.append(el("th", "", h));
  table.append(head);
  const rows = market.slice(0, 12);
  if (!rows.length) card.append(el("p", "sub", "Открытых заявок нет."));
  for (const o of rows) {
    const tr = el("tr");
    tr.append(el("td", "", titles[o.good] || o.good), el("td", "", o.side === "sell" ? "продажа" : "покупка"), el("td", "", money(o.price)), el("td", "", o.qty_open));
    table.append(tr);
  }
  card.append(table);
  card.append(el("h2", "", "Кто на рынке"));
  const peers = el("div", "peers");
  for (const p of board?.peers || []) {
    const row = el("div", "peer");
    const name = el("div");
    name.append(el("b", "", p.name));
    const tag = el("span", p.is_ai || p.owner_kind === "AI" ? "tag ai" : "tag", p.owner_kind === "AI" ? "ИИ" : p.owner_kind === "SYSTEM" ? "контур" : "человек");
    name.append(document.createTextNode(" "), tag);
    row.append(name, el("span", "", p.status));
    peers.append(row);
  }
  card.append(peers);
  return card;
}

function render() {
  app.replaceChildren(token ? gameView() : authView());
}
render();
refreshBoard();
setInterval(refreshBoard, 3000);
