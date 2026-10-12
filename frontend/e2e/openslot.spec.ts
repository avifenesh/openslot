import { test, expect, type APIRequestContext, type BrowserContext, type Page } from 'playwright/test';
import { spawn, execFileSync, type ChildProcess } from 'node:child_process';
import { createServer, type Server } from 'node:http';
import { createServer as createSocketServer } from 'node:net';
import { mkdir, mkdtemp, rm, readFile, writeFile } from 'node:fs/promises';
import { openSync, closeSync } from 'node:fs';
import { homedir } from 'node:os';
import { resolve, join } from 'node:path';
import { randomBytes } from 'node:crypto';

let child: ChildProcess;
let guestServer: Server;
let dataDirectory: string;
let base: string;
let guestOrigin: string;
let agentKey = '';
let keyId = '';
let bookedId = '';
let cookies: Awaited<ReturnType<BrowserContext['cookies']>> = [];
let serverLog: number;
const screenshots = resolve('../artifacts/screenshots');
const errors: string[] = [];
const externalRequests: string[] = [];
const downloads: {connector: string; filename: string; format: string}[] = [];

async function freePort(): Promise<number> {
	return new Promise((resolvePort, reject) => {
		const server = createSocketServer();
		server.on('error', reject);
		server.listen(0, '127.0.0.1', () => {
			const address = server.address();
			if (!address || typeof address === 'string') return reject(new Error('No test port'));
			server.close(error => error ? reject(error) : resolvePort(address.port));
		});
	});
}

async function api(request: APIRequestContext, path: string, method = 'GET', body?: unknown) {
	const response = await request.fetch(base + path, { method, data: body });
	expect(response.ok(), `${method} ${path}: ${await response.text()}`).toBeTruthy();
	return response.status() === 204 ? null : response.json();
}

async function mcp(request: APIRequestContext, name: string, args: Record<string, unknown> = {}) {
	const response = await request.post(base + '/mcp', {
		headers: { Authorization: `Bearer ${agentKey}`, 'MCP-Protocol-Version': '2026-07-28', 'Mcp-Method': 'tools/call', 'Mcp-Name': name, Accept: 'application/json, text/event-stream' },
		data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args, _meta: { 'io.modelcontextprotocol/protocolVersion': '2026-07-28', 'io.modelcontextprotocol/clientCapabilities': {}, 'io.modelcontextprotocol/clientInfo': {name:'openslot-e2e',version:'1.0.0'} } } },
	});
	expect(response.status(), await response.text()).toBe(200);
	const wire = await response.text();
	const result = response.headers()['content-type']?.includes('text/event-stream')
		? wire.split('\n').filter(line => line.startsWith('data:')).map(line => JSON.parse(line.slice(5))).find(message => message.id === 1)
		: JSON.parse(wire);
	expect(result, 'MCP returned a response to the request').toBeDefined();
	expect(result.error).toBeUndefined();
	expect(result.result.isError, JSON.stringify(result.result.content)).not.toBe(true);
	return result.result.structuredContent ?? JSON.parse(result.result.content.find((part: {type:string}) => part.type === 'text').text);
}

async function shot(page: Page, name: string) {
	await page.evaluate(() => window.scrollTo(0, 0));
	await page.screenshot({ path: join(screenshots, name + '.png'), fullPage: true, animations: 'disabled', mask: [page.locator('[data-sensitive]')] });
}

test.describe.configure({ mode: 'serial' });
test.beforeAll(async () => {
	await mkdir(screenshots, { recursive: true });
	const cacheRoot = join(homedir(), '.cache', 'openslot-tests');
	await mkdir(cacheRoot, { recursive: true });
	dataDirectory = await mkdtemp(join(cacheRoot, 'e2e-'));
	const port = await freePort();
	base = `http://127.0.0.1:${port}`;
	serverLog = openSync(resolve('../artifacts/e2e-server.log'), 'w');
	child = spawn(resolve('../openslot'), [], {
		cwd: dataDirectory,
		stdio: ['ignore', serverLog, serverLog],
		// Deliberately exclude any real calendar, mail or model credentials from tests.
		env: { PATH: process.env.PATH, HOME: homedir(), PORT: String(port), BIND_HOST: '127.0.0.1', BASE_URL: base, DATABASE_URL: `sqlite://${join(dataDirectory, 'test.db')}`, DATA_DIR: join(dataDirectory, 'uploads'), CALNODE_ENCRYPTION_KEY: randomBytes(32).toString('hex'), CALNODE_RECOVERY_SECRET: randomBytes(32).toString('hex'), LOG_LEVEL: 'error' },
	});
	await expect.poll(async () => {
		if (child.exitCode !== null) throw new Error(`Test server exited: ${child.exitCode}`);
		try { return (await fetch(base + '/v1/auth/status')).status; } catch { return 0; }
	}, { timeout: 30_000 }).toBe(200);
	guestServer = createServer((_request, response) => {
		response.setHeader('Content-Type', 'text/html; charset=utf-8');
		response.end(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Independent customer website</title><style>body{margin:0;padding:24px;font-family:sans-serif;background:#f5f6f3}main{max-width:880px;margin:auto}button{font-size:48px;background:magenta}h1{font-size:24px}input{border:20px solid red}</style><script src="${base}/embed.js" defer></script></head><body><main><h1>Let’s find a time</h1><openslot-booking slug="intro-call"></openslot-booking><button id="add">Add a booking button</button><div id="dynamic"></div></main><script>window.bookingEvents=[];document.addEventListener('openslot:booked',event=>window.bookingEvents.push(event.detail));document.querySelector('#add').onclick=()=>{document.querySelector('#dynamic').innerHTML='<button id="late-button" data-openslot-popup="intro-call">Book now</button>';};</script></body></html>`);
	});
	await new Promise<void>(resolveServer => guestServer.listen(0, '127.0.0.1', resolveServer));
	const address = guestServer.address();
	if (!address || typeof address === 'string') throw new Error('No customer-site port');
	guestOrigin = `http://127.0.0.1:${address.port}`;
});

test.beforeEach(async ({ page }) => {
	page.on('pageerror', error => errors.push(error.message));
	await page.context().route('**/*', async route => {
		const origin = new URL(route.request().url()).origin;
		if (origin !== base && origin !== guestOrigin) {
			externalRequests.push(origin);
			await route.abort();
		} else await route.continue();
	});
	if (cookies.length) await page.context().addCookies(cookies);
});

test.afterAll(async () => {
	await writeFile(resolve('../artifacts/connector-downloads.json'), JSON.stringify({downloads, externalRequests}, null, 2));
	if (guestServer) await new Promise<void>(resolveServer => guestServer.close(() => resolveServer()));
	if (child && child.exitCode === null) {
		await new Promise<void>(resolveChild => {
			const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
			child.once('exit', () => { clearTimeout(timer); resolveChild(); });
			child.kill('SIGTERM');
		});
	}
	if (serverLog !== undefined) closeSync(serverLog);
	if (dataDirectory) await rm(dataDirectory, { recursive: true, force: true });
});

test('owner setup, meeting creation, agent key and embedding setup', async ({ page }) => {
	await page.goto(base + '/admin/claim');
	await page.getByLabel('Full name', { exact: true }).fill('Alex Morgan');
	await page.getByLabel('Email', { exact: true }).fill('alex@example.test');
	await page.getByLabel('Password', { exact: true }).fill('Local-test-passphrase-2026');
	await page.getByRole('button', { name: 'Create owner account', exact: true }).click();
	await expect(page.getByRole('heading', { name: 'Your time, well spent.' })).toBeVisible();
	cookies = await page.context().cookies();
	await expect(page.getByText('Your booking page starts here.')).toBeVisible();
	for (let day = 0; day < 7; day++) {
		await api(page.request, '/v1/availability-rules', 'POST', { day_of_week: day, start_time: '09:00', end_time: '17:00' });
	}
	await page.getByRole('link', { name: 'Meeting types', exact: true }).click();
	await page.getByRole('button', { name: 'New event type', exact: true }).click();
	await page.getByLabel('Name', { exact: true }).fill('Intro call');
	await page.getByLabel('Slug (URL)', { exact: true }).fill('intro-call');
	await page.getByLabel('Duration (minutes)', { exact: true }).fill('30');
	await page.getByRole('button', { name: 'Create event type', exact: true }).click();
	await expect(page.getByRole('cell', { name: 'Intro call intro-call', exact: true })).toBeVisible();
	await api(page.request, '/v1/event-types/intro-call', 'PATCH', { location_type: 'link', location_value: 'https://example.com/meeting', is_public: true, is_active: true });
	for (const [slug, name, duration] of [['quick-hello', 'A quick hello', 15], ['project-catchup', 'Project catch-up', 45]] as const) {
		await api(page.request, '/v1/event-types', 'POST', { slug, name, duration_minutes: duration, location_type: 'link', location_value: 'https://example.com/meeting' });
	}
	await page.getByRole('link', { name: 'Your agent', exact: true }).click();
	await expect(page.getByLabel('MCP server URL')).toHaveValue(base + '/mcp');
	await page.getByRole('button', { name: 'New key', exact: true }).click();
	await page.getByLabel('Key name').fill('My test agent');
	await page.getByRole('button', { name: 'Create key', exact: true }).click();
	await expect(page.locator('[data-key-value]')).toBeVisible();
	agentKey = (await page.locator('[data-key-value]').textContent())!.trim();
	const keys = await api(page.request, '/v1/api-keys');
	keyId = keys.items.find((key: {name:string}) => key.name === 'My test agent').id;
	expect(keys.items.find((key: {id:string}) => key.id === keyId).connector_id).toBe('codex');
	await page.getByRole('button', {name:'Check OpenSlot connection',exact:true}).click();
	await expect(page.getByText(/OpenSlot responded with 10 tools/)).toBeVisible();
	await shot(page, 'agent-desktop-light');
	const today = new Date().toISOString().slice(0, 10);
	const end = new Date(Date.now() + 7 * 86400000).toISOString().slice(0, 10);
	const slots = await mcp(page.request, 'get_available_slots', { event_type_id: 'intro-call', date_from: today, date_to: end, timezone: 'UTC' });
	expect(slots.slots.length).toBeGreaterThan(0);
	await page.getByRole('link', { name: 'Share & embed', exact: true }).click();
	await expect(page.locator('openslot-booking .card')).toBeVisible();
	await expect(page.locator('.embed-code')).toContainText('<openslot-booking');
	await page.getByRole('button', { name: /A booking button/ }).click();
	await page.getByRole('button', { name: 'Find a time' }).click();
	await expect(page.getByRole('dialog', { name: 'Book a meeting' })).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(page.getByRole('dialog', { name: 'Book a meeting' })).toHaveCount(0);
	await page.getByRole('button', { name: /An inline calendar/ }).click();
	await shot(page, 'embed-desktop-light');
});

test('a guest books cross-origin, an agent manages the same booking, and conflicts are rejected', async ({ page, browser }) => {
	const guest = await browser.newContext({ viewport: {width:1023,height:991}, timezoneId: 'UTC', locale: 'en-GB' });
	try {
		const guestPage = await guest.newPage();
		guestPage.on('pageerror', error => errors.push(error.message));
		await guestPage.goto(guestOrigin);
		const widget = guestPage.locator('openslot-booking');
		await expect(widget.locator('.card')).toBeVisible();
		await expect(widget.locator('.cd.available').first()).toBeVisible();
		await widget.locator('.cd.available').first().click();
		await widget.locator('.slot-btn:not(.taken)').first().click();
		await widget.locator('input[autocomplete="name"]').fill('Jordan Lee');
		await widget.locator('input[type="email"]').fill('jordan@example.test');
		await widget.getByRole('button', { name: 'Confirm booking', exact: true }).click();
		await expect(widget.getByText('Booking confirmed', {exact:true})).toBeVisible();
		await expect.poll(() => guestPage.evaluate(() => (window as any).bookingEvents.length)).toBe(1);
		bookedId = await guestPage.evaluate(() => (window as any).bookingEvents[0].id);
		await shot(guestPage, 'guest-booking-confirmed');
		const booking = await mcp(page.request, 'get_booking', { booking_id: bookedId });
		expect(booking.status).toBe('confirmed');
		await mcp(page.request, 'cancel_booking', { booking_id: bookedId, reason: 'Local end-to-end verification' });
		const cancelled = await mcp(page.request, 'get_booking', { booking_id: bookedId });
		expect(cancelled.status).toBe('cancelled');
		const day = new Date(Date.now() + 86400000).toISOString().slice(0, 10);
		const until = new Date(Date.now() + 7 * 86400000).toISOString().slice(0, 10);
		const slots = await mcp(page.request, 'get_available_slots', { event_type_id: 'intro-call', date_from: day, date_to: until, timezone: 'UTC' });
		const body = { event_type_slug: 'intro-call', start_at: slots.slots[0].start, name:'Sam Rivera', email:'sam@example.test', timezone:'UTC' };
		const results = await Promise.all([0,1].map(() => guestPage.request.post(base + '/v1/bookings', { data:body })));
		expect(results.map(result => result.status()).sort()).toEqual([201,409]);
		const catchupSlots = await mcp(page.request, 'get_available_slots', { event_type_id: 'project-catchup', date_from: day, date_to: until, timezone: 'UTC' });
		const another = await mcp(page.request, 'create_booking', { event_type_id:'project-catchup', slot_start:catchupSlots.slots[0].start, attendee_name:'Casey Kim', attendee_email:'casey@example.test', timezone:'UTC' });
		expect(another.status).toBe('confirmed');
		const moved = await mcp(page.request, 'reschedule_booking', { booking_id: another.id, new_slot_start: catchupSlots.slots[2].start });
		expect(Date.parse(moved.start_at)).toBe(Date.parse(catchupSlots.slots[2].start));
		await guestPage.getByRole('button', { name:'Add a booking button' }).click();
		await guestPage.getByRole('button', { name:'Book now', exact:true }).click();
		await expect(guestPage.getByRole('dialog', { name:'Book a meeting' })).toBeVisible();
		await guestPage.keyboard.press('Escape');
		await expect(guestPage.locator('#late-button')).toBeFocused();
		await guestPage.goto(base + '/book/intro-call');
		await expect(guestPage.getByRole('heading', {name:'Intro call',exact:true})).toBeVisible();
		await shot(guestPage, 'public-booking-desktop');
		await guestPage.setViewportSize({width:390,height:844});
		await shot(guestPage, 'public-booking-mobile');
		expect(await guestPage.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
	} finally { await guest.close(); }
});

test('responsive views, dark theme, revocation and no client exceptions', async ({ page }) => {
	// Put one synthetic booking in the past to exercise the dashboard count contract.
	execFileSync('python3', ['-c', "import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.execute(\"UPDATE bookings SET start_at='2020-01-01T09:00:00Z', end_at='2020-01-01T09:30:00Z' WHERE status='confirmed' AND id=(SELECT id FROM bookings WHERE status='confirmed' ORDER BY start_at LIMIT 1)\"); c.commit(); c.close()", join(dataDirectory, 'test.db')]);
	await page.goto(base + '/admin/');
	await expect(page.getByRole('heading', {name:'Your time, well spent.'})).toBeVisible();
	await expect.soft(page.locator('.count-pill')).toHaveText('1');
	await shot(page, 'overview-desktop-light');
	await page.getByRole('button', {name:'Use dark theme'}).click();
	await expect(page.locator('html')).toHaveClass(/dark/);
	await shot(page, 'overview-desktop-dark');
	await page.getByRole('link', {name:'Your agent',exact:true}).click();
	await shot(page, 'agent-desktop-dark');
	await page.setViewportSize({width:390,height:844});
	await page.goto(base + '/admin/');
	await expect(page.getByRole('heading', {name:'Your time, well spent.'})).toBeVisible();
	await expect.soft(page.locator('.app-sidebar')).toBeHidden();
	await shot(page, 'overview-mobile-dark');
	expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
	await page.getByRole('button', {name:'Open navigation'}).click();
	await expect(page.getByRole('link', {name:'Overview',exact:true})).toBeFocused();
	await expect(page.locator('main')).toHaveAttribute('inert', '');
	await page.keyboard.press('Escape');
	await expect(page.getByRole('button', {name:'Open navigation'})).toBeFocused();
	await page.getByRole('button', {name:'Open navigation'}).click();
	await page.getByRole('button', {name:'Use light theme'}).click();
	await page.getByRole('link', {name:'Share & embed',exact:true}).click();
	await expect(page.locator('openslot-booking .card')).toBeVisible();
	await shot(page, 'embed-mobile-light');
	expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
	await api(page.request, `/v1/api-keys/${keyId}`, 'DELETE');
	const denied = await page.request.post(base + '/mcp', { headers:{Authorization:`Bearer ${agentKey}`}, data:{jsonrpc:'2.0',id:1,method:'tools/list'} });
	expect(denied.status()).toBe(401);
	expect(errors).toEqual([]);
});


test('app connections and provider-specific setup are usable without provider keys', async ({page}) => {
 await page.goto(base + '/admin/apps');
 await expect(page.getByRole('heading',{name:'Make your tools work together.'})).toBeVisible();
 for (const name of ['Google Calendar','Outlook / Microsoft 365','Apple, Fastmail & CalDAV','Gmail','Notion']) await expect(page.getByRole('heading',{name,exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Connect Notion',exact:true}).click();
 await expect(page.getByLabel('Notion integration token',{exact:true})).toHaveAttribute('type','password');
 await expect(page.getByRole('button',{name:'Connect and create bookings database'})).toBeVisible();
 await shot(page,'apps-desktop-light');
 await page.goto(base+'/admin/agent');
 const catalog=await api(page.request,'/v1/agent-connectors');
 for (const connector of catalog.items) {
  await page.getByRole('button',{name:`Connect ${connector.name}`,exact:true}).click();
  const panel=page.getByRole('region',{name:`${connector.name} setup`});
  await expect(panel).toBeVisible();
  if (!connector.oauth) {
   await panel.getByLabel('OpenSlot API key',{exact:true}).fill(agentKey);
   await expect(panel.getByLabel('OpenSlot API key',{exact:true})).toHaveAttribute('type','password');
  }
  const config=await panel.locator('.connector-code').textContent();
  expect(config).toContain(base+'/mcp');
  if (connector.format==='json') expect(JSON.parse(config!)).toBeTruthy();
  if (connector.public_url) await expect(panel.getByText('A public HTTPS address is needed.')).toBeVisible();
  if (connector.id==='antigravity') expect(JSON.parse(config!).mcpServers.openslot.serverUrl).toBe(base+'/mcp');
  if (connector.id==='gemini-cli') expect(JSON.parse(config!).mcpServers.openslot.httpUrl).toBe(base+'/mcp');
  if (connector.id==='vscode') expect(JSON.parse(config!).servers.openslot.type).toBe('http');
  if (connector.format==='json') {
   expect(config).toContain(agentKey);
   await expect(panel.locator('.connector-code')).toHaveAttribute('data-sensitive');
   if (connector.id==='claude-code') await shot(page,'agent-json-masked');
  } else expect(config).not.toContain(agentKey);
  if (connector.id==='codex') expect(config).toContain('bearer_token_env_var = "OPENSLOT_API_KEY"');
  if (connector.id==='openai') expect(config).toContain('"require_approval": "always"');
  if (connector.id==='grok') expect(config).toContain('base_url="https://api.x.ai/v1"');
  if (connector.id==='glm') expect(config).toContain('"transport_type": "streamable-http"');
  if (connector.id==='bedrock') expect(JSON.parse(config!)[0].config.remoteMcp.url).toBe(base+'/mcp');
  const pendingDownload=page.waitForEvent('download');
  await panel.getByRole('button',{name:'Download',exact:true}).click();
  const download=await pendingDownload;
  expect(download.suggestedFilename()).toBe(connector.filename);
  expect(await download.failure()).toBeNull();
  expect(await readFile((await download.path())!,'utf8')).toBe(config);
  const downloadDirectory=resolve('../artifacts/connector-downloads',connector.id);
  await mkdir(downloadDirectory,{recursive:true});
  await download.saveAs(join(downloadDirectory,download.suggestedFilename()));
  downloads.push({connector:connector.id,filename:download.suggestedFilename(),format:connector.format});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(1023);
 }
 await page.getByRole('button',{name:'Connect OpenAI API',exact:true}).click();
 await shot(page,'provider-desktop-light');
 await page.setViewportSize({width:390,height:844});
 await page.goto(base+'/admin/apps');
 await expect(page.getByRole('heading',{name:'Gmail',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Connect Notion',exact:true}).click();
 await page.getByText('Google setup and permissions',{exact:true}).click();
 await expect(page.getByLabel('Gmail redirect URI',{exact:true})).toHaveValue(base+'/v1/apps/gmail/callback');
 await shot(page,'apps-mobile-light');
 expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
 await page.goto(base+'/admin/agent');
 await page.getByRole('button',{name:'Connect Codex',exact:true}).click();
 await page.getByLabel('OpenSlot API key',{exact:true}).fill(agentKey);
 await page.getByRole('button',{name:'Check OpenSlot connection',exact:true}).click();
 await expect(page.getByText('This key is invalid or has been revoked.')).toBeVisible();
 await shot(page,'agent-mobile-light');
 expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
 expect(errors).toEqual([]);
 expect(externalRequests).toEqual([]);
});
