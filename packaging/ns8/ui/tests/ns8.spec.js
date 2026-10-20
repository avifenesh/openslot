import { test, expect } from '@playwright/test';

test.afterEach(async ({ page }, info) => {
  if (info.status !== info.expectedStatus) console.log('FRAME DOM:', await page.frames()[1]?.locator('body').innerText());
});

// Same-origin NS8 parent fixture. The UI and its task/network code are real;
// cluster responses and the parent's Vue 2 event bus are deliberately mocked.
async function mount(page, requestedPage = 'status', mode = 'ok', locale = 'en-US') {
  const errors = [];
  page.on('console', message => {
    if (message.type() === 'error' && /^(TypeError|ReferenceError|SyntaxError)/.test(message.text())) errors.push(message.text());
  });
  page.on('pageerror', error => { errors.push(error.message); console.error('BROWSER:', error.message); });
  await page.addInitScript(({ requestedPage, mode, locale }) => {
    Object.defineProperty(navigator, 'language', { value: locale });
    if (window !== window.parent) return;
    localStorage.setItem('loginInfo', JSON.stringify({ token: 'test-token' }));
    const handlers = new Map();
    const root = {
      apiUrl: '/api', $i18n: { locale: 'en' },
      $once(name, fn) { const once = (...args) => { root.$off(name, once); fn(...args); }; root.$on(name, once); },
      $on(name, fn) { handlers.set(name, [...(handlers.get(name) || []), fn]); },
      $off(name, fn) { handlers.set(name, (handlers.get(name) || []).filter(f => f !== fn)); },
      $emit(name, ...args) { for (const fn of [...(handlers.get(name) || [])]) fn(...args); },
    };
    const route = { path: '/apps/calnode1', fullPath: `/apps/calnode1?page=${requestedPage}` };
    const navigate = path => {
      if (typeof path === 'object') path = path.path;
      route.fullPath = path;
      route.path = path.split('?')[0];
      location.hash = path;
    };
    location.hash = route.fullPath;
    window.core = { $root: root, $route: route, $router: { push: navigate, replace: navigate }, $t: k => k, $tc: k => k, $te: () => true, $i18n: { locale: 'en' } };
    window.mode = mode;
    window.requests = [];
    window.config = { host: 'book.example.com', lets_encrypt: true, http2https: true };
  }, { requestedPage, mode, locale });
  await page.route('**/harness', route => route.fulfill({ contentType: 'text/html', body: '<iframe title="NS8 module" src="/index.html" style="width:100%;height:95vh;border:0"></iframe>' }));
  await page.route('**/api/**/tasks', async route => {
    expect(route.request().headers().authorization).toBe('Bearer test-token');
    const task = route.request().postDataJSON();
    const output = await page.evaluate(({ task, url }) => {
      window.requests.push({ task, url });
      if (task.action === 'configure-module' && window.mode === 'ok') window.config = task.data;
      return {
        'get-name': { name: 'Booking module' },
        'get-status': { instance: 'calnode1', node: 1, node_ui_name: 'Test node', services: [{ name: 'calnode.service', active: true, enabled: true, failed: false }], images: [{ name: 'ghcr.io/calnode/calnode:latest', size: '100 MB', created: 'today' }], volumes: [{ name: 'calnode-data', mount: '/data', created: 'today' }] },
        'list-backup-repositories': { repositories: [] },
        'list-backups': { backups: [] },
        'get-configuration': window.config,
        'configure-module': {},
        'list-installed-modules': { 1: [{ id: 'calnode1', version: '1.2.3' }] },
      }[task.action];
    }, { task, url: route.request().url() });
    if (mode === 'http-error' && task.action === 'get-configuration') {
      await route.fulfill({ status: 500, json: { message: 'fixture unavailable' } });
      return;
    }
    await route.fulfill({ json: { id: 'fixture-task' } });
    await page.evaluate(({ task, output, mode }) => {
      setTimeout(() => {
        const event = `${task.action}-${task.action === 'configure-module' && mode !== 'ok' ? mode : 'completed'}-${task.extra.eventId}`;
        if (mode === 'validation-failed' && task.action === 'configure-module') window.core.$root.$emit(event, [{ field: 'host', error: 'invalid_host' }]);
        else if (mode === 'aborted' && task.action === 'configure-module') window.core.$root.$emit(event, { error: 'fixture failure' }, task);
        else window.core.$root.$emit(event, task, { output });
      }, 50);
    }, { task, output, mode });
  });
  await page.goto('/harness');
  const app = page.frameLocator('iframe');
  await expect(app.getByText('Booking module', { exact: true }).first()).toBeVisible();
  return { app, errors };
}

test('status, services, images, volumes, backup/log navigation and about survive Vue 3', async ({ page }) => {
  const { app, errors } = await mount(page);
  await expect(app.getByText('Test node', { exact: true })).toBeVisible();
  await expect(app.getByText('calnode.service', { exact: true })).toBeVisible();
  await expect(app.getByText('ghcr.io/calnode/calnode:latest')).toBeVisible();
  await expect(app.getByText('calnode-data', { exact: true })).toBeVisible();
  await expect(app.getByText('backup.no_backup_configured')).toBeVisible();
  await app.getByText('About', { exact: true }).click();
  await expect(app.getByText('1.2.3', { exact: true })).toBeVisible();
  await expect(app.locator('img').first()).toBeVisible();
  expect(errors).toEqual([]);
});

test('settings load, edit, save exact NS8 payload, reload and parent navigation', async ({ page }) => {
  const { app, errors } = await mount(page, 'settings');
  const host = app.getByRole('textbox');
  await expect(host).toHaveValue('book.example.com');
  await host.fill('new.example.com');
  await app.getByRole('checkbox').first().uncheck();
  await app.getByRole('button', { name: 'Save' }).click();
  await expect.poll(() => page.evaluate(() => window.requests.filter(r => r.task.action === 'get-configuration').length)).toBe(2);
  const request = await page.evaluate(() => window.requests.find(r => r.task.action === 'configure-module'));
  expect(request.url).toContain('/api/module/calnode1/tasks');
  expect(request.task.data).toEqual({ host: 'new.example.com', lets_encrypt: false, http2https: true });
  expect(request.task.extra.eventId).toMatch(/^[\da-f-]{36}$/);
  await expect(host).toHaveValue('new.example.com');
  await page.evaluate(() => window.core.$router.push('/apps/calnode1?page=about'));
  await expect(app.getByText('1.2.3', { exact: true })).toBeVisible();
  expect(errors).toEqual([]);
});

test('HTTP task-creation failure is visible', async ({ page }) => {
  const { app, errors } = await mount(page, 'settings', 'http-error');
  await expect(app.getByText('Get configuration', { exact: true })).toBeVisible();
  await expect(app.getByRole('textbox')).toBeEnabled();
  expect(errors).toEqual([]);
});

test('backup and system log links retain the parent router contract', async ({ page }) => {
  const { app, errors } = await mount(page);
  await app.getByRole('button', { name: 'backup.go_to_backup' }).click();
  expect(await page.evaluate(() => window.core.$route.fullPath)).toBe('/backup');
  await page.evaluate(() => window.core.$router.push('/apps/calnode1?page=status'));
  await app.getByRole('button', { name: 'system_logs.card_button_label' }).click();
  expect(await page.evaluate(() => window.core.$route.fullPath)).toBe('/system-logs?context=module&selectedAppId=calnode1');
  expect(errors).toEqual([]);
});

test('mobile viewport can configure the module through parent navigation', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const { app, errors } = await mount(page, 'settings');
  await expect(app.getByRole('textbox')).toHaveValue('book.example.com');
  await app.getByRole('textbox').fill('mobile.example.com');
  await app.getByRole('button', { name: 'Save' }).click();
  await expect.poll(() => page.evaluate(() => window.config.host)).toBe('mobile.example.com');
  expect(errors).toEqual([]);
});

for (const mode of ['validation-failed', 'aborted']) {
  test(`settings surfaces ${mode} and allows retry`, async ({ page }) => {
    const { app, errors } = await mount(page, 'settings', mode);
    await expect(app.getByRole('textbox')).toHaveValue('book.example.com');
    await app.getByRole('button', { name: 'Save' }).click();
    await expect(app.getByText(mode === 'aborted' ? 'Something went wrong' : 'settings.invalid_host', { exact: false })).toBeVisible();
    await expect(app.getByRole('button', { name: 'Save' })).toBeEnabled();
    expect(errors).toEqual([]);
  });
}

test('unsupported language falls back and empty host is not submitted', async ({ page }) => {
  const { app, errors } = await mount(page, 'settings', 'ok', 'zz-ZZ');
  await expect(app.getByRole('textbox')).toHaveValue('book.example.com');
  await app.getByRole('textbox').fill('');
  await app.getByRole('button', { name: 'Save' }).click();
  expect(await page.evaluate(() => window.requests.some(r => r.task.action === 'configure-module'))).toBe(false);
  expect(errors).toEqual([]);
});
