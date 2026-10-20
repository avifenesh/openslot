import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests',
  workers: 2,
  fullyParallel: true,
  use: { baseURL: 'http://127.0.0.1:4188', headless: true },
  webServer: { command: 'yarn exec vite preview --host 127.0.0.1 --port 4188', url: 'http://127.0.0.1:4188', reuseExistingServer: false },
});
