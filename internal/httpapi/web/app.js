"use strict";
const $ = (id) => document.getElementById(id);
function money(value) {
  if (!value) return "Não informado";
  let cents = BigInt(value.amount_minor);
  const sign = cents < 0n ? "-" : "";
  if (cents < 0n) cents = -cents;
  return (
    sign +
    "R$ " +
    (cents / 100n).toLocaleString("pt-BR") +
    "," +
    (cents % 100n).toString().padStart(2, "0")
  );
}
const labels = {
  active: "Ativa",
  paused: "Coleta pausada",
  expired: "Acesso vencido",
  revoked: "Excluída",
  pending: "Aguardando autorização",
  denied: "Não autorizada",
  queued: "Na fila",
  running: "Sincronizando",
  failed: "Falha na sincronização",
  succeeded: "Sincronizada",
  cancelled: "Interrompida",
};
let connections = [],
  accounts = [],
  file = null,
  fileData = null,
  previewQuery = null,
  nextCursor = "",
  rows = [],
  busy = false;
function node(tag, text, className) {
  const n = document.createElement(tag);
  if (text !== undefined) n.textContent = text;
  if (className) n.className = className;
  return n;
}
function notice(message, error = false) {
  if (!$("login").hidden) {
    $("login-error").textContent = message;
    return;
  }
  $("notice").textContent = message;
  $("notice").classList.toggle("error", error);
  $("notice").hidden = false;
}
async function api(path, options = {}) {
  const r = await fetch(path, { credentials: "same-origin", ...options });
  if (r.status === 401) {
    $("workspace").hidden = true;
    $("login").hidden = false;
    throw Error("Abra o painel com ranjit ui ou entre com sua chave local.");
  }
  if (!r.ok) {
    let message = "Não foi possível concluir. Tente novamente.";
    try {
      message = (await r.json()).error.message;
    } catch {}
    throw Error(message);
  }
  if (r.status === 204) return;
  return r.json();
}
async function action(fn) {
  if (busy) return;
  busy = true;
  try {
    await fn();
  } catch (e) {
    notice(e.message, true);
  } finally {
    busy = false;
  }
}
function view(name) {
  document.querySelectorAll(".view").forEach((n) => (n.hidden = n.id !== name));
  document
    .querySelectorAll("nav button")
    .forEach((n) => n.classList.toggle("selected", n.dataset.view === name));
  $("breadcrumb").textContent =
    "Meu espaço / " +
    {
      overview: "Visão geral",
      transactions: "Lançamentos",
      import: "Importar extrato",
      settings: "Conexões e dados",
    }[name];
  if (name === "transactions") action(() => loadTransactions(false));
}
document
  .querySelectorAll("[data-view],[data-go]")
  .forEach((button) =>
    button.addEventListener("click", () =>
      view(button.dataset.view || button.dataset.go),
    ),
  );
function table(items) {
  if (!items.length) {
    const e = node("div", undefined, "empty");
    e.append(
      node("strong", "Nenhum lançamento por aqui."),
      node("span", "Importe um extrato ou sincronize uma conta."),
    );
    return e;
  }
  const wrap = node("div", undefined, "table-wrap"),
    t = node("table"),
    head = node("thead"),
    h = node("tr");
  ["DATA", "DESCRIÇÃO", "CONTA", "VALOR"].forEach((x) =>
    h.append(node("th", x)),
  );
  head.append(h);
  t.append(head);
  const body = node("tbody");
  items.forEach((item) => {
    const tr = node("tr"),
      account = accounts.find((a) => a.id === item.account_id);
    tr.append(
      node("td", item.posted_on.split("-").reverse().join("/"), "muted"),
      node("td", item.description),
      node("td", account?.name || "Extrato", "muted"),
      node(
        "td",
        money(item.amount),
        item.amount.amount_minor > 0 ? "positive" : "",
      ),
    );
    body.append(tr);
  });
  t.append(body);
  wrap.append(t);
  return wrap;
}
async function allPages(path) {
  let output = [],
    cursor = "";
  do {
    const q =
      path +
      (path.includes("?") ? "&" : "?") +
      "limit=100" +
      (cursor ? "&cursor=" + encodeURIComponent(cursor) : "");
    const p = await api(q);
    output.push(...p.data);
    cursor = p.next_cursor || "";
  } while (cursor);
  return output;
}
async function load() {
  const diagnostics = await api("/v1/diagnostics");
  $("certificate-alerts").replaceChildren();
  for (const cert of diagnostics.certificates || []) {
    if (cert.level !== "ok")
      $("certificate-alerts").append(
        node(
          "p",
          cert.profile +
            ": certificado " +
            ({
              expired: "vencido",
              unreadable: "não pôde ser lido",
              not_yet_valid: "ainda não válido",
              warning: "vence em " + cert.days_remaining + " dias",
              urgent: "vence em " + cert.days_remaining + " dias",
            }[cert.level] || cert.level) +
            ". Atualize os arquivos e reinicie o serviço.",
          "error",
        ),
      );
  }
  for (const name of diagnostics.unavailable_profiles || [])
    $("certificate-alerts").append(
      node("p", name + ": perfil indisponível. Rode ranjit doctor.", "error"),
    );
  connections = await allPages("/v1/connections?customer_id=personal");
  accounts = [];
  const statuses = [];
  for (const c of connections) {
    statuses.push(await api("/v1/connections/" + c.id + "/status"));
    if (
      c.status === "active" ||
      (c.authorization_mode === "api_credentials" &&
        ["paused", "expired"].includes(c.status))
    )
      accounts.push(
        ...(await allPages("/v1/connections/" + c.id + "/accounts")),
      );
  }
  $("account-count").textContent = accounts.length;
  $("connection-count").textContent =
    connections.filter((c) => c.status !== "revoked").length + " conexões";
  $("transaction-count").textContent = statuses
    .reduce((sum, s) => sum + s.transactions, 0)
    .toLocaleString("pt-BR");
  const known = accounts.filter((a) => a.balance && a.type !== "credit_card");
  const total = known.reduce(
    (sum, a) => sum + BigInt(a.balance.amount_minor),
    0n,
  );
  $("balance").textContent = known.length
    ? total <= BigInt(Number.MAX_SAFE_INTEGER) &&
      total >= BigInt(Number.MIN_SAFE_INTEGER)
      ? money({ amount_minor: Number(total) })
      : "Consulte cada conta"
    : "—";
  $("balance-note").textContent =
    "Soma de " + known.length + " contas; cartões separados. Confira as datas.";
  $("accounts").replaceChildren();
  if (!accounts.length) {
    const empty = node("div", undefined, "empty");
    empty.append(
      node("strong", "Seu espaço está pronto."),
      node("span", "Comece importando um OFX ou CSV."),
    );
    $("accounts").append(empty);
  }
  for (const a of accounts) {
    const card = node("article", undefined, "account-card");
    card.append(
      node("div", a.type === "credit_card" ? "▤" : "↗", "icon"),
      node("h3", a.name),
      node(
        "p",
        connections.find((c) => c.id === a.connection_id)?.institution_id || "",
      ),
      node("strong", money(a.balance)),
      node(
        "p",
        a.balance_at
          ? "Saldo de " +
              new Date(a.balance_at).toLocaleDateString("pt-BR", {
                timeZone: "UTC",
              })
          : "O arquivo não informou saldo.",
      ),
    );
    $("accounts").append(card);
  }
  const previous = $("filter-account").value;
  $("filter-account").replaceChildren(new Option("Todas as contas", ""));
  for (const a of accounts)
    $("filter-account").add(
      new Option(
        a.name +
          " · " +
          connections.find((c) => c.id === a.connection_id)?.institution_id,
        a.id,
      ),
    );
  $("filter-account").value = previous;
  const recent = await api("/v1/transactions?customer_id=personal&limit=7");
  $("recent").replaceChildren(table(recent.data));
  renderConnections(statuses);
  const institutions = (await api("/v1/institutions")).data;
  $("institution").replaceChildren();
  for (const i of institutions)
    $("institution").add(
      new Option(
        i.name + (i.validation === "experimental" ? " · experimental" : ""),
        i.id,
      ),
    );
  $("connect-form").querySelector("button").disabled = !institutions.length;
  if (!institutions.length)
    $("institution").add(new Option("Nenhum perfil configurado", ""));
}
function renderConnections(statuses) {
  $("connections").replaceChildren();
  for (const s of statuses) {
    const c = s.connection;
    if (c.status === "revoked") continue;
    const row = node("article", undefined, "connection-row"),
      detail = node("div");
    detail.append(
      node("h3", c.institution_id),
      node(
        "p",
        (labels[c.status] || c.status) +
          " · " +
          s.transactions +
          " lançamentos",
      ),
      node(
        "p",
        c.last_synced_at
          ? "Última sincronização: " +
              new Date(c.last_synced_at).toLocaleString("pt-BR")
          : "Ainda sem sincronização.",
      ),
    );
    if (s.latest_job)
      detail.append(
        node(
          "p",
          (labels[s.latest_job.status] || s.latest_job.status) +
            (s.latest_job.error_code ? " · " + s.latest_job.error_code : ""),
        ),
      );
    const buttons = node("div", undefined, "actions");
    const add = (label, route) => {
      const b = node("button", label, "secondary");
      b.onclick = () =>
        action(async () => {
          await api("/v1/connections/" + c.id + "/" + route, {
            method: "POST",
          });
          notice(
            route === "sync"
              ? "Sincronização enfileirada."
              : route === "pause"
                ? "Coleta pausada. Seu histórico foi preservado."
                : "Acesso renovado.",
          );
          await load();
        });
      buttons.append(b);
    };
    if (c.provider !== "file" && c.status === "active")
      add("Sincronizar", "sync");
    if (c.authorization_mode === "api_credentials") {
      if (c.status === "active") add("Pausar", "pause");
      else add("Renovar acesso", "renew");
    }
    const remove = node("button", "Excluir dados", "text-button");
    remove.onclick = async () => {
      $("delete-dialog").returnValue = "";
      $("delete-dialog").showModal();
      $("delete-dialog").onclose = () => {
        if ($("delete-dialog").returnValue === "delete")
          action(async () => {
            await api("/v1/connections/" + c.id, { method: "DELETE" });
            notice("Conexão e dados excluídos.");
            await load();
          });
      };
    };
    buttons.append(remove);
    row.append(detail, buttons);
    $("connections").append(row);
  }
}
async function loadTransactions(more) {
  const q = new URLSearchParams(new FormData($("filter-form")));
  q.set("customer_id", "personal");
  q.set("limit", "50");
  if (more && nextCursor) q.set("cursor", nextCursor);
  const result = await api("/v1/transactions?" + q);
  rows = more ? [...rows, ...result.data] : result.data;
  nextCursor = result.next_cursor || "";
  $("transaction-list").replaceChildren(table(rows));
  $("more").hidden = !nextCursor;
}
$("filter-form").onsubmit = (e) => {
  e.preventDefault();
  action(() => loadTransactions(false));
};
$("more").onclick = () => action(() => loadTransactions(true));
function invalidate() {
  previewQuery = null;
  $("preview").hidden = true;
}
function importQuery() {
  const q = new URLSearchParams({
    customer_id: "personal",
    format: $("import-format").value,
    account: $("import-account").value.trim(),
    decimal: $("decimal").value,
    date_format: $("date-format").value,
    delimiter: $("delimiter").value,
  });
  document
    .querySelectorAll("[data-column]")
    .forEach((s) => q.set(s.dataset.column + "_column", s.value));
  return q;
}
async function headers() {
  if (!fileData || $("import-format").value !== "csv") return;
  const result = await api(
    "/v1/imports/columns?delimiter=" + encodeURIComponent($("delimiter").value),
    { method: "POST", body: fileData },
  );
  $("column-fields").replaceChildren();
  const aliases = {
    date: ["date", "data", "posted_on"],
    description: [
      "description",
      "descrição",
      "descricao",
      "histórico",
      "historico",
      "title",
    ],
    amount: ["amount", "valor"],
    id: ["id", "fitid", "transaction_id"],
  };
  for (const field of ["date", "description", "amount", "id"]) {
    const label = node(
        "label",
        {
          date: "Data",
          description: "Descrição",
          amount: "Valor",
          id: "ID (opcional)",
        }[field],
      ),
      select = node("select");
    select.dataset.column = field;
    select.add(
      new Option(field === "id" ? "Detectar (opcional)" : "Detectar", ""),
    );
    for (const h of result.columns) select.add(new Option(h, h));
    select.value =
      result.columns.find((h) =>
        aliases[field].includes(h.trim().toLowerCase()),
      ) || "";
    select.onchange = invalidate;
    label.append(select);
    $("column-fields").append(label);
  }
}
async function chooseFile(chosen) {
  invalidate();
  fileData = null;
  file = chosen;
  if (!file) return;
  if (file.size > 8 * 1024 * 1024) throw Error("O arquivo deve ter até 8 MB.");
  fileData = await file.arrayBuffer();
  $("import-format").value = /\.csv$/i.test(file.name) ? "csv" : "ofx";
  $("csv-options").hidden = $("import-format").value !== "csv";
  await headers();
}
$("statement-file").onchange = () =>
  action(() => chooseFile($("statement-file").files[0]));
$("dropzone").ondragover = (e) => {
  e.preventDefault();
  $("dropzone").classList.add("dragging");
};
$("dropzone").ondragleave = () => $("dropzone").classList.remove("dragging");
$("dropzone").ondrop = (e) => {
  e.preventDefault();
  $("dropzone").classList.remove("dragging");
  if (e.dataTransfer.files.length) {
    $("statement-file").files = e.dataTransfer.files;
    action(() => chooseFile(e.dataTransfer.files[0]));
  }
};
$("import-form").addEventListener("input", invalidate);
$("import-format").onchange = () =>
  action(async () => {
    $("csv-options").hidden = $("import-format").value !== "csv";
    await headers();
  });
$("delimiter").onchange = () => action(headers);
$("import-form").onsubmit = (e) => {
  e.preventDefault();
  action(async () => {
    if (!fileData || file !== $("statement-file").files[0])
      throw Error("Escolha novamente o arquivo.");
    const q = importQuery();
    const result = await api("/v1/imports/preview?" + q, {
      method: "POST",
      body: fileData,
    });
    previewQuery = q.toString();
    $("preview-summary").textContent =
      result.result.new +
      " novos · " +
      result.result.updated +
      " atualizados · " +
      result.result.unchanged +
      " já existentes" +
      (result.result.deduplication === "exact_file"
        ? " — CSV sem IDs: novos arquivos precisam ter períodos separados."
        : "");
    $("preview-table").replaceChildren(
      table(
        result.sample.map((t) => ({
          posted_on: t.posted_on,
          description: t.description,
          amount: t.amount,
          account_id: "",
        })),
      ),
    );
    $("preview").hidden = false;
    $("confirm-import").disabled = result.result.duplicate;
    $("preview").scrollIntoView({ behavior: "smooth", block: "start" });
  });
};
$("confirm-import").onclick = () =>
  action(async () => {
    if (
      !previewQuery ||
      file !== $("statement-file").files[0] ||
      previewQuery !== importQuery().toString()
    )
      throw Error("Confira novamente a prévia.");
    const result = await api("/v1/imports?" + previewQuery, {
      method: "POST",
      body: fileData,
    });
    invalidate();
    await load();
    view("overview");
    notice(
      result.duplicate
        ? "Este arquivo já estava importado."
        : "Extrato importado: " + result.new + " novos lançamentos.",
    );
  });
$("connect-form").onsubmit = (e) => {
  e.preventDefault();
  action(async () => {
    const r = await api("/v1/connections", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        customer_id: "personal",
        institution_id: $("institution").value,
      }),
    });
    if (r.authorization_url) {
      const link = node("a", "Abrir autorização simulada ↗");
      link.href = r.authorization_url;
      link.target = "_blank";
      link.rel = "noopener";
      notice("Autorize os dados fictícios: ");
      $("notice").append(link);
    } else notice("Conexão criada. A primeira sincronização está na fila.");
    await load();
  });
};
$("logout").onclick = () =>
  action(async () => {
    await api("/ui/logout", { method: "POST" });
    location.reload();
  });
$("login-form").onsubmit = (e) => {
  e.preventDefault();
  action(async () => {
    await api("/ui/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ key: $("api-key").value }),
    });
    $("api-key").value = "";
    await start();
  });
};
async function start() {
  const token = location.hash.slice(1);
  history.replaceState(null, "", "/app/");
  if (token)
    await api("/ui/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    });
  await load();
  $("login").hidden = true;
  $("workspace").hidden = false;
}
start().catch((e) => {
  $("login").hidden = false;
  $("workspace").hidden = true;
  $("login-error").textContent = e.message;
});
setInterval(() => {
  if (!$("workspace").hidden && !busy && document.visibilityState === "visible")
    action(load);
}, 15000);
