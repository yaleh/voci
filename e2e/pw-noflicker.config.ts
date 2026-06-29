import { defineConfig, devices } from '@playwright/test';
export default defineConfig({
  testDir: './tests',
  timeout: 40_000,
  expect: { timeout: 5_000 },
  use: { baseURL: 'http://localhost:9474' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
