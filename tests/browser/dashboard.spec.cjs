const { test, expect } = require("@playwright/test");
const fs = require("node:fs");
const path = require("node:path");

test("first use, mapped preview, import, search, duplicate and deletion", async ({
  page,
  request,
}, info) => {
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const key = JSON.parse(
    fs.readFileSync(path.join(__dirname, ".tmp/config.json")),
  ).api_key;
  const issued = await request.post("/ui/link", {
    headers: { Authorization: "Bearer " + key },
  });
  expect(issued.ok()).toBeTruthy();
  const link = (await issued.json()).path;
  await page.goto(link);
  await expect(page.locator("#workspace")).toBeVisible();
  await expect(page).toHaveURL(/\/app\/$/);
  await expect(page.locator("#account-count")).toHaveText("0");
  await page
    .getByRole("button", { name: "＋ Importar extrato", exact: true })
    .click();
  await page.locator("#statement-file").setInputFiles({
    name: "extrato.csv",
    mimeType: "text/csv",
    buffer: Buffer.from(
      "Quando;Texto;Quantia;Código\n07/11/2026;Mercado do bairro;-123,45;tx1\n06/11/2026;Salário;4500,00;tx2\n",
    ),
  });
  await expect(page.locator('[data-column="date"] option')).toHaveCount(5);
  await page.locator("#import-account").fill("Conta pessoal");
  await page.locator("#decimal").selectOption(",");
  for (const [field, column] of Object.entries({
    date: "Quando",
    description: "Texto",
    amount: "Quantia",
    id: "Código",
  }))
    await page.locator(`[data-column="${field}"]`).selectOption(column);
  await page
    .getByRole("button", { name: "Conferir extrato", exact: true })
    .click();
  await expect(page.locator("#preview-summary")).toContainText("2 novos");
  // The API still sees an empty personal space before confirmation.
  const before = await request.get("/v1/connections?customer_id=personal", {
    headers: { Authorization: "Bearer " + key },
  });
  expect((await before.json()).data).toHaveLength(0);
  await page.screenshot({
    path: info.outputPath("import-preview.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Confirmar importação", exact: true })
    .click();
  await expect(page.locator("#transaction-count")).toHaveText("2");
  await expect(page.locator("#balance")).toHaveText("—");
  await page.screenshot({
    path: info.outputPath("overview-desktop.png"),
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Lançamentos 02", exact: true })
    .click();
  await page.locator('[name="q"]').fill("mercado");
  await page.getByRole("button", { name: "Filtrar", exact: true }).click();
  await expect(page.locator("#transaction-list tbody tr")).toHaveCount(1);
  await expect(page.locator("#transaction-list")).toContainText(
    "Mercado do bairro",
  );
  await page
    .getByRole("button", { name: "Importar extrato 03", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Conferir extrato", exact: true })
    .click();
  await expect(page.locator("#preview-summary")).toContainText(
    "2 já existentes",
  );
  await expect(page.locator("#confirm-import")).toBeDisabled();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator('nav [data-view="overview"]').click();
  await page.screenshot({
    path: info.outputPath("overview-mobile.png"),
    fullPage: true,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page.locator('nav [data-view="settings"]').click();
  await page
    .getByRole("button", { name: "Excluir dados", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Manter meus dados", exact: true })
    .click();
  await expect(page.locator(".connection-row")).toHaveCount(1);
  await page
    .getByRole("button", { name: "Excluir dados", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Excluir conexão e dados", exact: true })
    .click();
  await expect(page.locator(".connection-row")).toHaveCount(0);
  await page.getByRole("button", { name: "Sair", exact: true }).click();
  await expect(page.locator("#login")).toBeVisible();
  await page.locator("#api-key").fill("wrong-key");
  await page.getByRole("button", { name: "Entrar", exact: true }).click();
  await expect(page.locator("#login-error")).not.toHaveText("");
  expect(errors).toEqual([]);
});
