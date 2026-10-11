import { defineConfig } from 'playwright/test';

export default defineConfig({
	testDir: './e2e',
	workers: 1,
	fullyParallel: false,
	retries: 0,
	timeout: 120_000,
	expect: { timeout: 15_000 },
	outputDir: '../artifacts/e2e',
	reporter: 'list',
	use: {
		headless: true,
		viewport: { width: 1023, height: 991 },
		locale: 'en-GB',
		timezoneId: 'UTC',
		colorScheme: 'light',
		screenshot: 'only-on-failure',
		trace: 'retain-on-failure',
		launchOptions: { args: ['--disable-gpu'] },
	},
});
