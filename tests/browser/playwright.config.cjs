const { defineConfig } = require("@playwright/test");
const path = require("node:path");
module.exports = defineConfig({
  testDir: ".",
  testMatch: "*.spec.cjs",
  workers: 1,
  retries: 0,
  use: {
    baseURL: "http://127.0.0.1:8187",
    viewport: { width: 1440, height: 1000 },
    trace: "retain-on-failure",
  },
  webServer: {
    command: "../../bin/ranjit ui --no-browser",
    url: "http://127.0.0.1:8187/readyz",
    env: {
      RANJIT_CONFIG: path.join(__dirname, ".tmp/config.json"),
      RANJIT_API_KEY: "",
      RANJIT_ADDR: "127.0.0.1:8187",
      RANJIT_BASE_URL: "http://127.0.0.1:8187",
      RANJIT_DATABASE: "data/ranjit.db",
    },
    reuseExistingServer: false,
  },
});
