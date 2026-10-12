import { test, expect, type Page, type APIRequestContext, type BrowserContext } from 'playwright/test';
import { spawn, execFileSync, type ChildProcess } from 'node:child_process';
import { createServer } from 'node:net';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { openSync, closeSync } from 'node:fs';
import { homedir } from 'node:os';
import { resolve, join } from 'node:path';
import { randomBytes, createHash } from 'node:crypto';

// Each worker starts a disposable server from an explicitly selected binary. No .env,
// provider credentials, user database, or normal browser profile enters this fixture.
test.use({ actionTimeout: 5_000 });

const artifacts = resolve(process.env.OPENSLOT_TEST_ARTIFACTS ?? '../artifacts/workflows');
let child: ChildProcess;
let directory: string;
let base: string;
let identityOrigin: string;
const publicOrigin = 'https://public.openslot.test';
let log: number;
let cookies: Awaited<ReturnType<BrowserContext['cookies']>>;
const runtimeErrors: string[] = [];
const blockedRequests: string[] = [];

async function api(request: APIRequestContext, path: string, method = 'GET', data?: unknown) {
 const response = await request.fetch(base + path, { method, data });
 expect(response.ok(), `${method} ${path}: ${await response.text()}`).toBeTruthy();
 return response.status() === 204 ? null : response.json();
}
async function meeting(page: Page, slug: string) {
 return api(page.request, '/v1/event-types', 'POST', { slug, name: slug, duration_minutes: 30, location_type: 'link', location_value: 'https://example.test/room' });
}
async function select(page: Page, label: string, option: string) {
 await page.getByLabel(label, { exact: true }).click();
 await page.getByRole('option', { name: option, exact: true }).click();
}
async function screenshot(page: Page, name: string) {
 await page.screenshot({ path: join(artifacts, name + '.png'), fullPage: true, animations: 'disabled' });
}
async function theme(page: Page, value: 'light' | 'dark', mobile = false) {
 if (await page.locator('html').evaluate(el => el.classList.contains('dark')) === (value === 'dark')) return;
 if (mobile) await page.getByRole('button', {name: 'Open navigation'}).click();
 await page.getByRole('button', {name: value === 'dark' ? 'Use dark theme' : 'Use light theme'}).click();
 if (mobile) await page.keyboard.press('Escape');
}
function seedManageToken(bookingID: string) {
 const token = randomBytes(32).toString('hex');
 const hash = createHash('sha256').update(token).digest('hex');
 execFileSync('python3',['-c', 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("INSERT INTO booking_manage_tokens(token_hash,booking_id,expires_at) VALUES(?,?,?)",sys.argv[2:]); db.commit()', join(directory,'test.db'), hash, bookingID, new Date(Date.now()+86400000).toISOString()]);
 return token;
}
async function nextSlots(page: Page, slug: string) {
 const from = new Date(Date.now() + 2 * 86400000).toISOString().slice(0,10);
 const to = new Date(Date.now() + 9 * 86400000).toISOString().slice(0,10);
 const response = await api(page.request, `/v1/event-types/${slug}/slots?from=${from}&to=${to}&timezone=UTC`);
 expect(response.slots.length).toBeGreaterThan(2);
 return response.slots as {start:string; end:string}[];
}

test.beforeAll(async ({ request }) => {
 await mkdir(artifacts, {recursive:true});
 const root = join(homedir(), '.cache', 'openslot-release', 'browser');
 await mkdir(root, {recursive:true});
 directory = await mkdtemp(join(root, 'synthetic-'));
 const port = await new Promise<number>((res, rej) => {
  const socket = createServer(); socket.on('error', rej);
  socket.listen(0, '127.0.0.1', () => { const address = socket.address(); if (!address || typeof address === 'string') return rej(new Error('no port')); socket.close(err => err ? rej(err) : res(address.port)); });
 });
 base = `http://127.0.0.1:${port}`;
 identityOrigin = `http://localhost:${port}`;
 log = openSync(join(artifacts, `server-${process.pid}.log`), 'w');
 child = spawn(resolve(process.env.OPENSLOT_TEST_BINARY ?? '../openslot'), [], {
  cwd: directory, stdio: ['ignore', log, log],
  env: {PATH:process.env.PATH, HOME:directory, GOMAXPROCS:'2', PORT:String(port), BIND_HOST:'127.0.0.1', BASE_URL:identityOrigin, PUBLIC_BASE_URL:publicOrigin, DATABASE_URL:`sqlite://${join(directory,'test.db')}`, DATA_DIR:join(directory,'uploads'), CALNODE_ENCRYPTION_KEY:randomBytes(32).toString('hex'), CALNODE_RECOVERY_SECRET:randomBytes(32).toString('hex'), LOG_LEVEL:'error'},
 });
 await expect.poll(async () => {
  if (child.exitCode !== null) throw new Error(`Synthetic server exited: ${child.exitCode}`);
  try { return (await request.get(base + '/v1/auth/status')).status(); } catch { return 0; }
 }, {timeout:30_000}).toBe(200);
 await api(request, '/v1/auth/claim', 'POST', {name:'Workflow Owner',email:'owner@example.test',password:'Hermetic-test-password-2026',timezone:'UTC'});
 cookies = (await request.storageState()).cookies;
 for (let day = 0; day < 7; day++) await api(request, '/v1/availability-rules', 'POST', {day_of_week:day,start_time:'09:00',end_time:'17:00'});
});

test.beforeEach(async ({page}) => {
 await page.context().addCookies(cookies);
 page.on('pageerror', error => runtimeErrors.push(`${test.info().title}: ${error.message}`));
 await page.context().route('**/*', async route => {
  const url = new URL(route.request().url());
  if (url.origin !== base && url.protocol !== 'data:') {blockedRequests.push(url.origin); await route.abort();}
  else await route.continue();
 });
});
test.afterEach(async ({page}, info) => {
 if (info.status !== info.expectedStatus) await screenshot(page, `failure-${info.title.replace(/[^a-z0-9]+/gi,'-').slice(0,80)}`);
});
test.afterAll(async () => {
 await writeFile(join(artifacts, `runtime-${process.pid}.json`), JSON.stringify({runtimeErrors,blockedRequests},null,2));
 if (child && child.exitCode === null) await new Promise<void>(res => {const timer=setTimeout(()=>child.kill('SIGKILL'),10_000);child.once('exit',()=>{clearTimeout(timer);res();});child.kill('SIGTERM');});
 if (log !== undefined) closeSync(log);
 if (directory) await rm(directory,{recursive:true,force:true});
});

test('sharing copies the configured public origin from a different admin origin', async ({page}) => {
 const slug = 'sharing-origin';
 await meeting(page,slug);
 expect((await api(page.request,'/v1/event-types')).public_base_url).toBe(publicOrigin);
 // Capture clipboard writes without relying on host clipboard access.
 await page.addInitScript(() => {
  Object.defineProperty(navigator,'clipboard',{value:{writeText:async(text:string)=>{
   (window as unknown as {copiedText:string}).copiedText=text;
  }},configurable:true});
 });
 await page.goto(base+'/admin');
 await page.getByRole('button',{name:`Copy ${slug} link`,exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>(window as unknown as {copiedText:string}).copiedText)).toBe(publicOrigin+'/book/'+slug);
 await expect(page.getByRole('link',{name:`Preview ${slug}`,exact:true})).toHaveAttribute('href',publicOrigin+'/book/'+slug);
 await page.goto(base+'/admin/share');
 await page.getByRole('button',{name:/A simple link/}).click();
 await expect(page.locator('.embed-code code')).toContainText(publicOrigin+'/book/');
 await page.getByRole('button',{name:'Copy',exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>(window as unknown as {copiedText:string}).copiedText)).toContain(publicOrigin+'/book/');
 await page.getByRole('button',{name:/An inline calendar/}).click();
 await expect(page.locator('.embed-code code')).toContainText(publicOrigin+'/embed.js');
 await page.getByRole('button',{name:'Copy',exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>(window as unknown as {copiedText:string}).copiedText)).toContain(publicOrigin+'/embed.js');
 await screenshot(page,'sharing-public-origin');
});

test('meeting editor persists scheduling, fallback location, visibility and validation', async ({page}) => {
 await meeting(page,'editor-flow');
 await page.goto(base+'/admin/event-types/editor-flow');
 await page.getByLabel('Name',{exact:true}).fill('Consultation');
 await page.getByLabel('Duration (minutes)',{exact:true}).fill('45');
 await page.getByLabel('Slot interval (minutes)',{exact:true}).fill('15');
 await page.getByLabel('Buffer before (min)').fill('10');
 await page.getByLabel('Buffer after (min)').fill('5');
 await page.getByLabel('Minimum notice (min)').fill('60');
 await page.getByLabel('Booking window (days)').fill('14');
 await page.getByLabel('Max active bookings per person').fill('2');
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await expect(page.getByText('Changes saved',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Save changes',exact:true})).toBeEnabled();
 expect(await api(page.request,'/v1/event-types/editor-flow')).toMatchObject({name:'Consultation',duration_minutes:45,slot_interval_minutes:15,buffer_before_minutes:10,buffer_after_minutes:5,min_notice_minutes:60,max_future_days:14,max_active_bookings:2});
 await page.reload();
 await expect(page.getByLabel('Slot interval (minutes)',{exact:true})).toHaveValue('15');
 await select(page,'Type','Google Meet');
 await expect(page.getByText(/No calendar is connected/)).toBeVisible();
 await page.getByLabel('Google Meet link').fill('https://meet.google.com/abc-defg-hij');
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,'/v1/event-types/editor-flow')).location_type).toBe('google_meet');
 await expect(page.getByRole('button',{name:'Save changes',exact:true})).toBeEnabled();
 await page.getByLabel('Slot interval (minutes)',{exact:true}).fill('0');
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await expect(page.getByText('Slot interval must be at least 1 minute.',{exact:true})).toBeVisible();
 expect((await api(page.request,'/v1/event-types/editor-flow')).slot_interval_minutes).toBe(15);
 await screenshot(page,'meeting-editor-desktop-light');
});

test('intake questions create edit delete and required public answer', async ({page}) => {
 await meeting(page,'question-flow');
 await page.goto(base+'/admin/event-types/question-flow');
 await page.getByRole('button',{name:'Questions',exact:true}).click();
 await page.getByLabel('Label',{exact:true}).fill('Project goals');
 await page.getByRole('checkbox',{name:'Required',exact:true}).check();
 await page.getByRole('button',{name:'Add question',exact:true}).click();
 await expect(page.getByRole('cell',{name:'Project goals',exact:true})).toBeVisible();
 const row = page.getByRole('row').filter({hasText:'Project goals'});
 await row.getByRole('button',{name:'Edit question',exact:true}).click();
 await page.getByLabel('Label',{exact:true}).first().fill('What should we prepare?');
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect(page.getByRole('cell',{name:'What should we prepare?',exact:true})).toBeVisible();
 await page.getByLabel('Label',{exact:true}).fill('Optional detail');
 await page.getByRole('button',{name:'Add question',exact:true}).click();
 await page.getByRole('row').filter({hasText:'Optional detail'}).getByRole('button',{name:'Remove question',exact:true}).click();
 await page.getByRole('alertdialog').getByRole('button',{name:/Remove/}).click();
 await expect(page.getByRole('cell',{name:'Optional detail',exact:true})).toHaveCount(0);
 await page.goto(base+'/book/question-flow');
 await page.locator('.cd.available').first().click();
 await page.locator('.slot-btn:not(.taken)').first().click();
 await expect(page.getByLabel(/What should we prepare/)).toBeVisible();
 await expect(page.getByLabel(/What should we prepare/)).toHaveAttribute('required','');
 await page.getByLabel(/What should we prepare/).fill('Review our timeline');
 await page.locator('#f-name').fill('Question Guest');
 await page.locator('#f-email').fill('question@example.test');
 const createdResponse = page.waitForResponse(r=>r.url()===base+'/v1/bookings' && r.request().method()==='POST');
 await page.getByRole('button',{name:'Confirm booking',exact:true}).click();
 const created = await (await createdResponse).json();
 await expect(page.getByText('Booking confirmed',{exact:true})).toBeVisible();
 const answers = await api(page.request,`/v1/bookings/${created.id}/answers`);
 expect(answers.items).toEqual(expect.arrayContaining([expect.objectContaining({label:'What should we prepare?',value:'Review our timeline'})]));
 await screenshot(page,'public-required-question');
});

test('public management reschedules and cancels a real local booking', async ({page}) => {
 await meeting(page,'manage-flow');
 const slots = await nextSlots(page,'manage-flow');
 const booking = await api(page.request,'/v1/bookings','POST',{event_type_slug:'manage-flow',start_at:slots[0].start,name:'Manage Guest',email:'manage@example.test',timezone:'UTC'});
 // A synthetic manage token avoids needing a real email transport.
 const token = seedManageToken(booking.id);
 await page.goto(base+'/manage/'+token);
 await page.getByRole('button',{name:/Reschedule/,exact:false}).click();
 await page.locator('.cd.available').first().click();
 await page.locator('.slot-btn:not(.taken)').last().click();
 await page.getByRole('button',{name:/Confirm.*reschedule|Confirm new time/}).click();
 await expect.poll(async()=> (await api(page.request,`/v1/bookings/${booking.id}`)).start_at).not.toBe(booking.start_at);
 await page.reload();
 await expect(page.getByText('Link expired or invalid',{exact:true})).toBeVisible();
 // Rescheduling intentionally rotates the emailed token. Test cancellation on a
 // second synthetic booking; email delivery of the replacement is a separate prerequisite.
 const second = await api(page.request,'/v1/bookings','POST',{event_type_slug:'manage-flow',start_at:slots[1].start,name:'Cancel Guest',email:'cancel@example.test',timezone:'UTC'});
 await page.goto(base+'/manage/'+seedManageToken(second.id));
 await page.getByRole('button',{name:'Cancel booking',exact:true}).click();
 const confirm = page.getByRole('button',{name:/Yes.*cancel|Confirm cancellation/});
 if (await confirm.count()) await confirm.click();
 await expect.poll(async()=> (await api(page.request,`/v1/bookings/${second.id}`)).status).toBe('cancelled');
 await screenshot(page,'public-manage-cancelled');
});

test('booking admin filters, details, reschedule and cancellation', async ({page}) => {
 await meeting(page,'admin-booking-flow');
 const slots = await nextSlots(page,'admin-booking-flow');
 const booking = await api(page.request,'/v1/bookings','POST',{event_type_slug:'admin-booking-flow',start_at:slots[0].start,name:'Admin Guest',email:'admin-guest@example.test',timezone:'UTC'});
 await page.goto(base+'/admin/bookings');
 await select(page,'Filter by event type','admin-booking-flow');
 await select(page,'Filter by status','Confirmed');
 await expect(page.getByText('Admin Guest',{exact:true})).toBeVisible();
 const row = page.getByRole('row').filter({hasText:'Admin Guest'});
 await row.getByRole('button',{name:'Show responses',exact:true}).click();
 await expect(page.getByText('No intake responses for this booking.')).toBeVisible();
 await row.getByRole('button',{name:'Reschedule',exact:true}).click();
 await expect(page.getByText(/Reschedule · Admin Guest/)).toBeVisible();
 await page.getByRole('button',{name:'Pick a date'}).click();
 const day = new Date(slots[0].start).getUTCDate();
 await page.locator('[data-bits-day]:not([data-disabled]):not([data-outside-month])').filter({hasText:new RegExp(`^${day}$`)}).click();
 const slotButton = page.getByRole('button',{name:/^\d{1,2}:\d{2}/}).last();
 await slotButton.click();
 await page.getByRole('button',{name:/^Confirm ·/}).click();
 await expect.poll(async()=> (await api(page.request,`/v1/bookings/${booking.id}`)).start_at).not.toBe(booking.start_at);
 await row.getByRole('button',{name:'Cancel booking',exact:true}).click();
 await page.getByRole('alertdialog').getByRole('button',{name:'Cancel booking',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,`/v1/bookings/${booking.id}`)).status).toBe('cancelled');
 await select(page,'Filter by status','Cancelled');
 await expect(page.getByText('Admin Guest',{exact:true})).toBeVisible();
});

test('calendar setup empty state and CalDAV validation make no provider requests', async ({page}) => {
 await page.goto(base+'/admin/calendar');
 await expect(page.getByText('CalDAV (Apple iCloud, Fastmail, Nextcloud)',{exact:true})).toBeVisible();
 await page.goto(base+'/admin/calendar?provider=caldav');
 await page.getByRole('button',{name:'Connect calendar',exact:true}).click();
 await expect(page.getByText('Username and app password are both required.')).toBeVisible();
 await screenshot(page,'calendar-empty-validation');
});

test('calendar provider fixture covers conflict picks, destination and expired grant', async ({page}) => {
 const connection = {id:'fixture-cal',provider:'google',account_email:'fixture@example.test',calendar_id:'primary',is_destination:true};
 await page.route('**/v1/calendar/status',route=>route.fulfill({json:{connected:true,provider:'google',connections:[connection],configured:true,providers:['google'],account_email:connection.account_email}}));
 await page.route('**/v1/calendar/connections/fixture-cal/calendars?**',route=>route.fulfill({json:{calendars:[{id:'primary',name:'Main calendar',primary:true,writable:true,check_conflicts:true,is_destination:true},{id:'secondary',name:'Team calendar',writable:true,check_conflicts:false,is_destination:false}]}}));
 await page.goto(base+'/admin/calendar');
 await page.getByRole('button',{name:'Manage calendars',exact:true}).click();
 await expect(page.getByLabel('Check Team calendar for conflicts',{exact:true})).toBeVisible();
 let selection: any;
 await page.route('**/v1/calendar/connections/fixture-cal/calendars',async route=>{selection=route.request().postDataJSON();await route.fulfill({json:{ok:true}});});
 await page.getByLabel('Check Team calendar for conflicts',{exact:true}).check();
 await page.getByLabel('Write bookings into Team calendar (select again to clear)',{exact:true}).check();
 await expect(page.getByLabel('Write bookings into Main calendar (select again to clear)',{exact:true})).not.toBeChecked();
 await screenshot(page,'calendar-provider-fixture');
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect(page.getByRole('button',{name:'Manage calendars',exact:true})).toBeVisible();
 expect(selection).toMatchObject({provider:'google',account_email:'fixture@example.test',calendars:expect.arrayContaining([expect.objectContaining({id:'secondary',check_conflicts:true,is_destination:true})])});
 await writeFile(join(artifacts,'calendar-provider-selection.json'),JSON.stringify(selection,null,2));
 await page.getByRole('button',{name:'Manage calendars',exact:true}).click();
 await page.unroute('**/v1/calendar/connections/fixture-cal/calendars?**');
 await page.route('**/v1/calendar/connections/fixture-cal/calendars?**',route=>route.fulfill({status:409,json:{error:'Reconnect this account'}}));
 await page.getByRole('button',{name:'Hide calendars',exact:true}).click();
 await page.getByRole('button',{name:'Manage calendars',exact:true}).click();
 await expect(page.getByText(/Reconnect this account/)).toBeVisible();
});

test('email no-sender validation and local preferences persist without mail', async ({page}) => {
 await page.goto(base+'/admin/settings/email');
 await expect(page.getByText('Not configured',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Send test email',exact:true})).toBeDisabled();
 const unsent = await page.request.post(base+'/v1/settings/email/test');
 expect(unsent.ok()).toBe(false);
 expect((await unsent.json()).error).toContain('Configure an SMTP or Resend sender');
 await page.getByLabel('From name',{exact:true}).fill('Synthetic Workspace');
 await page.getByRole('button',{name:'Save',exact:true}).first().click();
 await expect(page.getByText('Email settings saved',{exact:true})).toBeVisible();
 await page.reload();
 await expect(page.getByLabel('From name',{exact:true})).toHaveValue('Synthetic Workspace');
 expect((await api(page.request,'/v1/settings/email')).enabled).toBe(false);
});

test('profile, notification preferences and theme survive reload', async ({page}) => {
 await page.goto(base+'/admin/settings/profile');
 await page.getByLabel('Name',{exact:true}).fill('Updated Owner');
 await page.getByLabel('Booking handle',{exact:true}).fill('workflow-owner');
 await select(page,'Date format','YYYY-MM-DD');
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,'/v1/users/me')).name).toBe('Updated Owner');
 await page.reload();
 await expect(page.getByLabel('Name',{exact:true})).toHaveValue('Updated Owner');
 expect(await api(page.request,'/v1/users/me')).toMatchObject({name:'Updated Owner',handle:'workflow-owner',date_format:'ymd'});
 await theme(page,'dark');
 await page.reload();
 await expect(page.locator('html')).toHaveClass(/dark/);
 await page.goto(base+'/admin/settings/notifications');
 const confirmation = page.getByRole('switch',{name:'Booking confirmation',exact:true});
 await confirmation.uncheck();
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect(page.getByText('Preferences saved',{exact:true})).toBeVisible();
 await page.reload();
 await expect(confirmation).not.toBeChecked();
 await screenshot(page,'profile-preferences-desktop-dark');
});

test('member invite claims locally, team membership, rename, archive and restore', async ({page,browser}) => {
 await page.goto(base+'/admin/members');
 await page.getByRole('button',{name:'Invite member',exact:true}).click();
 await page.getByLabel('Email address').fill('member@example.test');
 const response = page.waitForResponse(r=>r.url()===base+'/v1/invites' && r.request().method()==='POST');
 await page.getByRole('button',{name:'Generate invite link',exact:true}).click();
 const invite = await (await response).json();
 expect(invite.email_sent).toBe(false);
 expect(invite.invite_url).toBeTruthy();
 const guest = await browser.newContext();
 try {
  await guest.route('**/*',route=>new URL(route.request().url()).origin===base?route.continue():route.abort());
  const member = await guest.newPage();
  expect(new URL(invite.invite_url).origin).toBe(identityOrigin);
  await member.goto(base + new URL(invite.invite_url).pathname);
  await member.getByLabel('Full name',{exact:true}).fill('Workflow Member');
  await member.getByLabel('Password',{exact:true}).fill('Hermetic-member-password-2026');
  await member.getByRole('button',{name:/Create account|Join workspace|Accept invite/}).click();
  await expect(member.getByRole('heading',{name:'Your time, well spent.'})).toBeVisible();
 } finally {await guest.close();}
 await page.reload();
 await expect(page.getByRole('row').filter({hasText:'Workflow Member'})).toBeVisible();
 await page.goto(base+'/admin/teams');
 await page.getByRole('button',{name:'New team',exact:true}).click();
 await page.getByLabel('Team name').fill('Scheduling Team');
 await page.getByRole('button',{name:'Create team',exact:true}).click();
 await expect(page.getByText('Scheduling Team',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Manage',exact:true}).click();
 await page.getByRole('button',{name:'Add a member…',exact:true}).click();
 await page.getByRole('option',{name:/Workflow Member/}).click();
 await page.getByRole('button',{name:'Add',exact:true}).click();
 const teams = await api(page.request,'/v1/teams');
 expect((await api(page.request,`/v1/teams/${teams.items[0].id}`)).members).toHaveLength(1);
 await page.getByRole('button',{name:'Rename',exact:true}).click();
 await page.locator('input').first().fill('Renamed Team');
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect(page.getByText('Renamed Team',{exact:true})).toBeVisible();
 await page.goto(base+'/admin/members');
 await page.getByRole('row').filter({hasText:'Workflow Member'}).getByRole('button',{name:'Archive',exact:true}).click();
 await page.getByRole('alertdialog').getByRole('button',{name:'Archive',exact:true}).click();
 await page.getByRole('button',{name:'Show archived',exact:true}).click();
 const archived = page.getByRole('row').filter({hasText:'Workflow Member'});
 await expect(archived.getByText('Archived',{exact:true})).toBeVisible();
 await archived.getByRole('button',{name:'Restore',exact:true}).click();
 await expect(archived.getByText('Archived',{exact:true})).toHaveCount(0);
});

test('webhook validates empty URL, creates, shows empty deliveries and deletes', async ({page}) => {
 await page.goto(base+'/admin/webhooks');
 await page.getByRole('button',{name:'New webhook',exact:true}).click();
 await page.getByRole('button',{name:'Create webhook',exact:true}).click();
 await expect(page.getByText(/URL is required|Enter.*URL/)).toBeVisible();
 // Registration alone does not deliver anything. Delete before making any bookings.
 await page.getByLabel('Endpoint URL').fill('https://192.0.2.10/hooks');
 await page.getByRole('button',{name:'Create webhook',exact:true}).click();
 await expect(page.getByRole('cell',{name:'https://192.0.2.10/hooks',exact:true})).toBeVisible();
 const hooks = await api(page.request,'/v1/webhooks');
 expect(hooks.items).toHaveLength(1);
 await page.getByRole('row').filter({hasText:'https://192.0.2.10/hooks'}).getByRole('button',{name:'Recent deliveries',exact:true}).click();
 await expect(page.getByText(/No deliveries/)).toBeVisible();
 await page.getByRole('row').filter({hasText:'https://192.0.2.10/hooks'}).getByRole('button',{name:'Delete webhook',exact:true}).click();
 await page.getByRole('alertdialog').getByRole('button',{name:'Delete',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,'/v1/webhooks')).items.length).toBe(0);
});

test('advanced settings persist local configuration without running providers or models', async ({page}) => {
 await page.goto(base+'/admin/settings/payments');
 await expect(page.getByText('Not configured',{exact:true})).toBeVisible();
 await page.getByLabel('Secret key',{exact:true}).fill('sk_test_synthetic_local');
 await page.getByLabel('Publishable key',{exact:true}).fill('pk_test_synthetic_local');
 await page.getByLabel('Webhook signing secret',{exact:true}).fill('whsec_synthetic_local');
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect(page.getByText(/Saved · set a price/)).toBeVisible();
 expect(await api(page.request,'/v1/settings/stripe')).toMatchObject({configured:true,secret_key_set:true,publishable_key:'pk_test_synthetic_local'});
 await page.getByRole('button',{name:'Disconnect',exact:true}).click();
 await page.getByRole('alertdialog').getByRole('button',{name:'Remove',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,'/v1/settings/stripe')).configured).toBe(false);
 await page.goto(base+'/admin/settings/video');
 await page.getByLabel('Server URL',{exact:true}).fill('ws://127.0.0.1:1');
 await page.getByLabel('API Key',{exact:true}).fill('synthetic-key');
 await page.getByLabel('API Secret',{exact:true}).fill('synthetic-secret');
 await page.getByRole('button',{name:'Save',exact:true}).first().click();
 await expect(page.getByText(/Saved · "OpenSlot Video/)).toBeVisible();
 expect((await api(page.request,'/v1/settings/livekit')).configured).toBe(true);
 await page.getByRole('button',{name:'Disconnect',exact:true}).click();
 await expect(page.getByText('LiveKit disconnected',{exact:true})).toBeVisible();
 await page.goto(base+'/admin/settings/ai');
 await page.getByLabel('API endpoint',{exact:true}).fill('http://127.0.0.1:1/v1');
 await page.getByLabel('Model',{exact:true}).fill('synthetic-model');
 await page.getByLabel('Additional instructions',{exact:true}).fill('Ask what the guest wants to discuss.');
 await page.getByRole('button',{name:'Save',exact:true}).first().click();
 await expect(page.getByText('Saved',{exact:true})).toBeVisible();
 expect(await api(page.request,'/v1/settings/llm')).toMatchObject({enabled:false,model:'synthetic-model'});
 await page.reload();
 await expect(page.getByLabel('Additional instructions',{exact:true})).toHaveValue('Ask what the guest wants to discuss.');
});

test('settings fit desktop and mobile in both themes', async ({page}) => {
 const metrics: unknown[] = [];
 for (const width of [1023,390]) {
  await page.setViewportSize({width,height:991});
  for (const value of ['light','dark'] as const) {
   await page.goto(base+'/admin/settings/profile');
   await expect(page.getByLabel('Name',{exact:true})).toBeVisible();
   await theme(page,value,width===390);
   for (const path of ['profile','email','payments','video','ai','storage','google','zoom','branding','tracking','notifications']) {
    await page.goto(base+'/admin/settings/'+path);
    await expect(page.getByRole('heading',{name:'Settings',exact:true})).toBeVisible();
    await expect(page.getByText('Loading…',{exact:true})).toHaveCount(0);
    const metric = await page.evaluate(() => {
     const input = document.querySelector('main input:not([type="hidden"]):not([type="file"]):not([type="radio"])') as HTMLElement | null;
     const box = input?.getBoundingClientRect();
     return {viewport:innerWidth,documentWidth:document.documentElement.scrollWidth,inputWidth:box?.width,inputRight:box?.right};
    });
    metrics.push({path,theme:value,...metric});
    expect.soft(metric.documentWidth,`${path} at ${width} ${value}`).toBeLessThanOrEqual(width);
    if (path==='profile'||path==='email') await screenshot(page,`settings-${path}-${width}-${value}`);
   }
  }
 }
 await writeFile(join(artifacts,'settings-layout.json'),JSON.stringify(metrics,null,2));
 expect(runtimeErrors).toEqual([]);
});

test('collective secondary host must resolve upcoming meetings before archive', async ({page,browser}) => {
 const invitation = await api(page.request,'/v1/invites','POST',{email:'secondary@example.test'});
 const context = await browser.newContext();
 let secondary: {id:string};
 try {
  const token = new URL(invitation.invite_url).pathname.split('/').pop();
  const response = await context.request.post(base+`/v1/invites/${token}/claim`,{data:{name:'Secondary Host',password:'Secondary-host-password-2026',timezone:'UTC'}});
  expect(response.ok(),await response.text()).toBe(true);
  secondary = await api(context.request,'/v1/users/me');
  for (let day=0;day<7;day++) await api(context.request,'/v1/availability-rules','POST',{day_of_week:day,start_time:'09:00',end_time:'17:00'});
 } finally {await context.close();}
 const owner = await api(page.request,'/v1/users/me');
 await meeting(page,'collective-archive');
 await api(page.request,'/v1/event-types/collective-archive','PATCH',{routing_mode:'collective'});
 await api(page.request,'/v1/event-types/collective-archive/hosts','PUT',{hosts:[{user_id:owner.id,role:'required',priority:0},{user_id:secondary!.id,role:'required',priority:1}]});
 const slots = await nextSlots(page,'collective-archive');
 const booking = await api(page.request,'/v1/bookings','POST',{event_type_slug:'collective-archive',start_at:slots[0].start,name:'Collective Guest',email:'collective@example.test',timezone:'UTC'});
 expect(booking.host_id).toBe(owner.id);
 const upcoming = await api(page.request,`/v1/users/${secondary!.id}/upcoming-bookings`);
 await writeFile(join(artifacts,'secondary-host-archive.json'),JSON.stringify({booking,upcoming},null,2));
 expect.soft(upcoming.items.some((b:{id:string})=>b.id===booking.id),'secondary host is included in the resolve-meetings list').toBe(true);
 await page.goto(base+'/admin/members');
 await page.getByRole('row').filter({hasText:'Secondary Host'}).getByRole('button',{name:'Archive',exact:true}).click();
 await screenshot(page,'secondary-host-archive-dialog');
 await expect.soft(page.locator('[role=dialog], [role=alertdialog]')).toContainText("Resolve Secondary Host's upcoming meetings",{timeout:2000});
 const archived = await page.request.post(base+`/v1/users/${secondary!.id}/archive`);
 await writeFile(join(artifacts,'secondary-host-archive-response.json'),JSON.stringify({status:archived.status(),body:await archived.json()},null,2));
 expect(archived.status(),'API prevents archiving any still-assigned host').toBe(409);
});

test('weekly availability edits reject reversed hours and support remove and add', async ({page}) => {
 await page.goto(base+'/admin/availability');
 const sunday = page.getByText('Sunday',{exact:true}).locator('..');
 await sunday.locator('[data-slot=select-trigger]').first().click();
 await page.getByRole('option',{name:'9:30am',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,'/v1/availability-rules')).items.find((r:{day_of_week:number})=>r.day_of_week===0)?.start_time).toBe('09:30');
 await sunday.locator('[data-slot=select-trigger]').first().click();
 await page.getByRole('option',{name:'5pm',exact:true}).click();
 await expect(sunday.getByText('End must be after start',{exact:true})).toBeVisible();
 expect((await api(page.request,'/v1/availability-rules')).items.find((r:{day_of_week:number})=>r.day_of_week===0).start_time).toBe('09:30');
 await sunday.getByRole('button').filter({hasNotText:'Add block'}).last().click();
 await expect(sunday.getByText('No hours set',{exact:true})).toBeVisible();
 await sunday.getByRole('button',{name:'+ Add hours',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,'/v1/availability-rules')).items.filter((r:{day_of_week:number})=>r.day_of_week===0).length).toBe(1);
 await page.reload();
 await expect(sunday.locator('[data-slot=select-trigger]').first()).toContainText('9am');
});

test('meeting duplicate is inactive, activation persists, deletion is confirmed', async ({page}) => {
 await meeting(page,'duplicate-flow');
 await page.goto(base+'/admin/event-types');
 const row = page.getByRole('row').filter({hasText:'duplicate-flow'});
 await row.getByRole('button').nth(2).click();
 await expect(page.getByText(/Duplicated as/)).toBeVisible();
 const types = await api(page.request,'/v1/event-types');
 const copy = types.items.find((et:{slug:string})=>et.slug.startsWith('duplicate-flow-'));
 expect(copy).toBeTruthy();
 expect(copy.is_active).toBe(false);
 await page.goto(base+'/admin/event-types/'+copy.slug);
 await page.getByRole('checkbox',{name:'Active (accepting bookings)',exact:true}).check();
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await expect(page.getByText('Changes saved',{exact:true})).toBeVisible();
 expect((await api(page.request,'/v1/event-types/'+copy.slug)).is_active).toBe(true);
 await page.goto(base+'/admin/event-types');
 const copiedRow = page.getByRole('row').filter({hasText:copy.slug});
 await copiedRow.getByRole('button').last().click();
 await page.getByRole('alertdialog').getByRole('button',{name:'Delete',exact:true}).click();
 await expect(copiedRow).toHaveCount(0);
});

test('branding, language fallback and tracking persist with no analytics requests', async ({page}) => {
 await page.goto(base+'/admin/settings/branding');
 await page.getByLabel('Business name',{exact:true}).fill('Synthetic Studio');
 await page.getByLabel('Privacy policy URL',{exact:true}).fill('https://example.test/privacy');
 await page.getByLabel('Terms URL',{exact:true}).fill('https://example.test/terms');
 const branding = await api(page.request,'/v1/settings/branding');
 const language = branding.supported_locales.find((locale:{code:string;name:string})=>locale.code==='es');
 expect(language).toBeTruthy();
 await select(page,'Fallback language',language.name);
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect(page.getByText(/Branding saved|Settings saved|Saved/)).toBeVisible();
 await page.reload();
 await expect(page.getByLabel('Business name',{exact:true})).toHaveValue('Synthetic Studio');
 expect((await api(page.request,'/v1/settings/branding')).fallback_locale).toBe('es');
 await expect(page.getByLabel('Fallback language',{exact:true})).toContainText(language.name);
 await page.goto(base+'/admin/settings/tracking');
 await page.getByLabel('GTM Container ID',{exact:true}).fill('invalid');
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect(page.getByText('GTM container ID must look like GTM-XXXXXX',{exact:true})).toBeVisible();
 await page.getByLabel('GTM Container ID',{exact:true}).fill('');
 await page.getByLabel('<head> HTML',{exact:true}).fill('<meta name="synthetic-audit" content="local">');
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,'/v1/settings/tracking')).head_html).toBe('<meta name="synthetic-audit" content="local">');
 await page.reload();
 await expect(page.getByLabel('<head> HTML',{exact:true})).toHaveValue('<meta name="synthetic-audit" content="local">');
});

test('avatar crop upload, reload and removal work with an in-memory synthetic PNG', async ({page}) => {
 await page.goto(base+'/admin/settings/profile');
 const tinyPNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aC1kAAAAASUVORK5CYII=','base64');
 await page.locator('input[type=file]').setInputFiles({name:'synthetic-avatar.png',mimeType:'image/png',buffer:tinyPNG});
 await expect(page.getByRole('dialog')).toBeVisible();
 await expect(page.locator('.cropper-container')).toBeVisible();
 await page.getByRole('dialog').getByRole('button',{name:/Save/}).click();
 await expect(page.getByText('Avatar updated',{exact:true})).toBeVisible();
 const me = await api(page.request,'/v1/users/me');
 expect(me.avatar_url).toBeTruthy();
 expect((await page.request.get(base+me.avatar_url)).status()).toBe(200);
 await page.reload();
 await page.getByRole('button',{name:'Remove photo',exact:true}).click();
 await expect.poll(async()=> (await api(page.request,'/v1/users/me')).avatar_url ?? '').toBe('');
});

test('meeting save action remains clickable while its success notification is visible', async ({page}) => {
 await meeting(page,'toast-hit-target');
 await page.goto(base+'/admin/event-types/toast-hit-target');
 const save = page.getByRole('button',{name:'Save changes',exact:true});
 await save.click();
 await expect(page.getByText('Changes saved',{exact:true})).toBeVisible();
 await expect(save).toBeEnabled();
 await save.scrollIntoViewIfNeeded();
 const hit = await save.evaluate(el=>{const box=el.getBoundingClientRect();const top=document.elementFromPoint(box.x+box.width/2,box.y+box.height/2);return {clickable:!!top && (top===el || el.contains(top)),interceptor:top?.closest('[data-sonner-toast]')?.textContent};});
 await screenshot(page,'meeting-save-toast-hit-target');
 await writeFile(join(artifacts,'meeting-save-hit-target.json'),JSON.stringify(hit,null,2));
 expect(hit.clickable,'success notification must not cover the persistent Save changes action').toBe(true);
 await page.getByLabel('Name',{exact:true}).fill('Consecutive save verified');
 await save.click();
 await expect.poll(async()=> (await api(page.request,'/v1/event-types/toast-hit-target')).name).toBe('Consecutive save verified');
});

test('logout, invalid login, reset and invite error states are clear without mail', async ({page}) => {
 await page.goto(base+'/admin/recordings');
 await expect(page.getByRole('heading',{name:'Recordings',exact:true})).toBeVisible();
 await expect(page.getByText(/No recordings/)).toBeVisible();
 await page.getByRole('button',{name:'Sign out',exact:true}).click();
 await expect(page).toHaveURL(/\/admin\/login/);
 await page.getByLabel('Email',{exact:true}).fill('owner@example.test');
 await page.getByLabel('Password',{exact:true}).fill('incorrect-password');
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await expect(page.getByText(/Invalid|invalid|incorrect/)).toBeVisible();
 await page.goto(base+'/admin/reset-password?token=invalid-synthetic');
 await page.getByLabel('New password',{exact:true}).fill('Reset-synthetic-password-2026');
 await page.getByLabel('Confirm new password',{exact:true}).fill('Reset-synthetic-password-2026');
 await page.getByRole('button',{name:'Set new password',exact:true}).click();
 await expect(page.getByText(/invalid|expired/i)).toBeVisible();
 await page.goto(base+'/admin/invite/invalid-synthetic');
 await expect(page.getByText(/invalid|expired/i)).toBeVisible();
 await screenshot(page,'invalid-invite-mobile-ready');
});

async function loginOAuthFixture(page:Page) {
 // The preceding logout workflow invalidates its session in a full suite run.
 await api(page.request,'/v1/auth/login/email','POST',{email:'owner@example.test',password:'Hermetic-test-password-2026'});
}

test('Google OAuth rejects incomplete or malformed pairs without replacing active credentials', async ({page}) => {
 await loginOAuthFixture(page);
 const clientID = 'fixture.apps.googleusercontent.com';
 const settings = '/v1/settings/google';
 const storedPair = () => execFileSync('python3', ['-c', 'import sqlite3,sys,json; db=sqlite3.connect(sys.argv[1]); print(json.dumps(db.execute("SELECT google_client_id,google_client_secret_enc FROM server_settings WHERE id=1").fetchone()))', join(directory,'test.db')], {encoding:'utf8'});
 await api(page.request,settings,'PATCH',{client_id:''});
 try {
  const missing = await page.request.patch(base+settings,{data:{client_id:clientID}});
  expect(missing.status(),await missing.text()).toBe(400);
  expect(await api(page.request,settings)).toMatchObject({client_id:'',configured:false,client_secret_set:false,configuration_error:expect.any(String)});
  const valid = await api(page.request,settings,'PATCH',{client_id:clientID,client_secret:'synthetic-secret-never-sent-to-google'});
  expect(valid).toMatchObject({client_id:clientID,configured:true,client_secret_set:true,configuration_error:''});
  expect(valid).not.toHaveProperty('client_secret');
  const originalPair = storedPair();
  for (const invalid of [
   {client_id:'not-a-google-client',client_secret:'synthetic-replacement'},
   {client_id:'https://fixture.apps.googleusercontent.com',client_secret:'synthetic-replacement'},
   {client_id:'changed.apps.googleusercontent.com'},
   {client_id:'changed.apps.googleusercontent.com',client_secret:'   '},
  ]) {
   const rejected = await page.request.patch(base+settings,{data:invalid});
   expect(rejected.status(),await rejected.text()).toBe(400);
   expect(await api(page.request,settings)).toEqual(valid);
   // Compare ciphertext as well as public settings, without writing the secret to evidence.
   expect(storedPair()===originalPair,'the stored client ID and encrypted secret remain intact').toBe(true);
   const redirect = await page.request.get(base+'/v1/calendar/connect?provider=google',{maxRedirects:0});
   expect(redirect.status()).toBe(302);
   const target = new URL(redirect.headers().location);
   expect(target.hostname).toBe('accounts.google.com');
   expect(target.searchParams.get('client_id')).toBe(clientID);
  }
  expect(await api(page.request,settings,'PATCH',{client_id:clientID,client_secret:''})).toEqual(valid);
  expect(storedPair()===originalPair,'saving the same ID retains its encrypted secret').toBe(true);
 } finally {
  await api(page.request,settings,'PATCH',{client_id:''});
 }
 expect(await api(page.request,settings)).toMatchObject({client_id:'',configured:false,client_secret_set:false});
 expect((await api(page.request,'/v1/calendar/status')).providers??[]).not.toContain('google');
 expect((await api(page.request,'/v1/apps')).gmail_oauth_configured).toBe(false);
});

test('Google OAuth Apps and Settings links open isolated Google login with the correct callbacks', async ({page}) => {
 await loginOAuthFixture(page);
 const clientID = 'fixture.apps.googleusercontent.com';
 const consoleErrors: string[] = [];
 const navigations: {path:string;clientID:string|null;callback:string|null;scopes:string[]}[] = [];
 const initialRuntimeErrors = runtimeErrors.length;
 const initialBlockedRequests = blockedRequests.length;
 page.on('console',message=>{if(message.type()==='error')consoleErrors.push(message.text());});
 // Page routing takes precedence over the existing context network block. Fulfill
 // only the expected Google navigation; no Google request leaves this browser.
 await page.route('https://accounts.google.com/**',async route=>{
  expect(route.request().isNavigationRequest()).toBe(true);
  const target = new URL(route.request().url());
  navigations.push({path:target.pathname,clientID:target.searchParams.get('client_id'),callback:target.searchParams.get('redirect_uri'),scopes:(target.searchParams.get('scope')??'').split(' ')});
  await route.fulfill({contentType:'text/html',body:'<!doctype html><html><head><title>Isolated OAuth fixture</title></head><body><h1>Synthetic Google login</h1></body></html>'});
 });
 // Playwright does not route subsequent requests in an HTTP redirect chain.
 // Fetch the real local 302 without following it, then turn its Location into a
 // fresh browser navigation so the Google fixture above intercepts it reliably.
 for(const endpoint of ['/v1/calendar/connect?provider=google','/v1/apps/gmail/connect']) {
  await page.route(base+endpoint,async route=>{
   const response = await route.fetch({maxRedirects:0});
   expect(response.status()).toBe(302);
   const location = response.headers().location;
   expect(new URL(location).origin).toBe('https://accounts.google.com');
   await route.fulfill({status:200,contentType:'text/html',body:`<!doctype html><script>location.replace(${JSON.stringify(location).replace(/</g,'\\u003c')})</script>`});
  });
 }
 const card = (name:string) => page.locator('section').filter({has:page.getByRole('heading',{name,exact:true})});
 const openLogin = async (link:ReturnType<Page['getByRole']>, callback:string, gmail=false) => {
  const before = navigations.length;
  await expect(link).toHaveAttribute('data-sveltekit-reload',/^(true)?$/);
  await link.click();
  await expect(page.getByRole('heading',{name:'Synthetic Google login',exact:true})).toBeVisible();
  expect(navigations).toHaveLength(before+1);
  const target = new URL(page.url());
  expect(target.hostname).toBe('accounts.google.com');
  expect(target.searchParams.get('client_id')).toBe(clientID);
  expect(target.searchParams.get('redirect_uri')).toBe(identityOrigin+callback);
  expect(target.searchParams.get('response_type')).toBe('code');
  expect(target.searchParams.get('state')).toBeTruthy();
  if(gmail)expect(navigations.at(-1)?.scopes).toEqual(expect.arrayContaining(['https://www.googleapis.com/auth/gmail.readonly','https://www.googleapis.com/auth/gmail.send']));
 };
 await api(page.request,'/v1/settings/google','PATCH',{client_id:''});
 try {
  await page.goto(base+'/admin/apps');
  for(const name of ['Google Calendar','Gmail']) {
   const setup = card(name).getByRole('link',{name:'Set up Google OAuth',exact:true});
   await expect(setup).toHaveAttribute('href','/admin/settings/google');
   await setup.click();
   await expect(page).toHaveURL(base+'/admin/settings/google');
   await expect(page.getByLabel('Client ID',{exact:true})).toBeVisible();
   await page.goto(base+'/admin/apps');
  }
  await page.goto(base+'/admin/settings/google');
  const form = page.locator('div').filter({has:page.getByRole('heading',{name:'Google OAuth',exact:true})}).filter({has:page.getByLabel('Client ID',{exact:true})}).last();
  await form.getByLabel('Client ID',{exact:true}).fill(clientID);
  await form.getByLabel('Client Secret',{exact:true}).fill('synthetic-secret-never-sent-to-google');
  const savedResponse = page.waitForResponse(r=>r.url()===base+'/v1/settings/google'&&r.request().method()==='PATCH');
  await form.getByRole('button',{name:'Save',exact:true}).click();
  expect((await savedResponse).status()).toBe(200);
  await expect(page.getByText('Credentials saved',{exact:true})).toBeVisible();
  await expect(page.getByText('Configured',{exact:true})).toHaveCount(0);
  await expect(form.getByLabel('Client Secret',{exact:true})).toHaveValue('');
  expect(await api(page.request,'/v1/settings/google')).toMatchObject({configured:true,configuration_error:'',client_id:clientID});
  for(const width of [1023,390]) {
   await page.setViewportSize({width,height:991});
   for(const value of ['light','dark'] as const) {
    await page.goto(base+'/admin/settings/google');
    await expect(page.getByText('Credentials saved',{exact:true})).toBeVisible();
    await theme(page,value,width===390);
    await expect(page.getByRole('link',{name:'Connect Google Calendar',exact:true})).toHaveAttribute('href','/v1/calendar/connect?provider=google');
    await expect(page.getByRole('link',{name:'Connect Gmail',exact:true})).toHaveAttribute('href','/v1/apps/gmail/connect');
    expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await page.screenshot({path:join(artifacts,`oauth-settings-${width}-${value}.png`),fullPage:true,animations:'disabled',mask:[page.locator('[data-sensitive]')]});
    await page.goto(base+'/admin/apps');
    await expect(card('Google Calendar').getByRole('link',{name:'Connect calendar',exact:true})).toHaveAttribute('href','/v1/calendar/connect?provider=google');
    await expect(card('Gmail').getByRole('link',{name:'Connect Gmail',exact:true})).toHaveAttribute('href','/v1/apps/gmail/connect');
    expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await page.screenshot({path:join(artifacts,`oauth-apps-${width}-${value}.png`),fullPage:true,animations:'disabled',mask:[page.locator('[data-sensitive]')]});
   }
  }
  await page.goto(base+'/admin/apps');
  await openLogin(card('Google Calendar').getByRole('link',{name:'Connect calendar',exact:true}),'/v1/calendar/callback');
  await page.goto(base+'/admin/apps');
  await openLogin(card('Gmail').getByRole('link',{name:'Connect Gmail',exact:true}),'/v1/apps/gmail/callback',true);
  await page.goto(base+'/admin/settings/google');
  await openLogin(page.getByRole('link',{name:'Connect Google Calendar',exact:true}),'/v1/calendar/callback');
  await page.goto(base+'/admin/settings/google');
  await openLogin(page.getByRole('link',{name:'Connect Gmail',exact:true}),'/v1/apps/gmail/callback',true);
  await writeFile(join(artifacts,'oauth-navigation.json'),JSON.stringify({navigations,consoleErrors,googleRequestsFulfilledLocally:navigations.length},null,2));
  expect(consoleErrors).toEqual([]);
  expect(runtimeErrors.slice(initialRuntimeErrors)).toEqual([]);
  expect(blockedRequests.slice(initialBlockedRequests)).toEqual([]);
 } finally {
  await api(page.request,'/v1/settings/google','PATCH',{client_id:''});
 }
});

test('Google OAuth startup rejects malformed saved and environment credentials', async ({page}) => {
 const clientID = 'fixture.apps.googleusercontent.com';
 // Keep the fixture's generated key across restarts so its disposable keyvault
 // remains readable. This child was spawned with the fixture's whitelist only.
 const {readFile} = await import('node:fs/promises');
 const fixtureEnv = (await readFile(`/proc/${child.pid}/environ`,'utf8')).split('\0');
 const encryptionKey = fixtureEnv.find(value=>value.startsWith('CALNODE_ENCRYPTION_KEY='))?.slice('CALNODE_ENCRYPTION_KEY='.length);
 expect(encryptionKey,'the synthetic child has its generated encryption key').toBeTruthy();
 const restart = async (googleEnv:Record<string,string>={}) => {
  if(child.exitCode===null)await new Promise<void>(res=>{
   const timer=setTimeout(()=>child.kill('SIGKILL'),10_000);
   child.once('exit',()=>{clearTimeout(timer);res();});child.kill('SIGTERM');
  });
  child=spawn(resolve(process.env.OPENSLOT_TEST_BINARY??'../openslot'),[],{
   cwd:directory,stdio:['ignore',log,log],
   env:{PATH:process.env.PATH,HOME:directory,GOMAXPROCS:'2',TMPDIR:process.env.TMPDIR,PORT:new URL(base).port,BIND_HOST:'127.0.0.1',BASE_URL:identityOrigin,PUBLIC_BASE_URL:publicOrigin,COOKIE_SECURE:'false',DATABASE_URL:`sqlite://${join(directory,'test.db')}`,DATA_DIR:join(directory,'uploads'),CALNODE_ENCRYPTION_KEY:encryptionKey,CALNODE_RECOVERY_SECRET:randomBytes(32).toString('hex'),LOG_LEVEL:'error',...googleEnv},
  });
  await expect.poll(async()=>{
   if(child.exitCode!==null)throw new Error(`Synthetic OAuth server exited: ${child.exitCode}`);
   try{return(await page.request.get(base+'/v1/auth/status')).status();}catch{return 0;}
  },{timeout:30_000}).toBe(200);
  await loginOAuthFixture(page);
 };
 const assertDisabled = async () => {
  expect(await api(page.request,'/v1/settings/google')).toMatchObject({configured:false,configuration_error:expect.stringMatching(/\S/)});
  expect((await api(page.request,'/v1/calendar/status')).providers??[]).not.toContain('google');
  expect((await api(page.request,'/v1/apps')).gmail_oauth_configured).toBe(false);
  await page.goto(base+'/admin/apps');
  for(const name of ['Google Calendar','Gmail'])await expect(page.locator('section').filter({has:page.getByRole('heading',{name,exact:true})}).getByRole('link',{name:'Set up Google OAuth',exact:true})).toHaveAttribute('href','/admin/settings/google');
  for(const endpoint of ['/v1/calendar/connect?provider=google','/v1/apps/gmail/connect']) {
   const blocked=await page.request.get(base+endpoint,{maxRedirects:0});
   expect(blocked.status()).toBeGreaterThanOrEqual(400);
   expect(blocked.headers().location).toBeUndefined();
  }
 };
 await loginOAuthFixture(page);
 try {
  await restart();
  await api(page.request,'/v1/settings/google','PATCH',{client_id:''});
  await api(page.request,'/v1/settings/google','PATCH',{client_id:clientID,client_secret:'synthetic-startup-secret'});
  // Corrupt only the disposable DB to model settings written by an older build.
  execFileSync('python3',['-c','import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("UPDATE server_settings SET google_client_id=? WHERE id=1",("malformed-saved-client",)); db.commit()',join(directory,'test.db')]);
  await restart();
  await assertDisabled();
  await page.goto(base+'/admin/settings/google');
  await expect(page.getByRole('alert')).toContainText('Google setup needs attention');
  await expect(page.getByRole('link',{name:'Connect Google Calendar',exact:true})).toHaveCount(0);
  await api(page.request,'/v1/settings/google','PATCH',{client_id:''});
  execFileSync('python3',['-c','import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("UPDATE server_settings SET google_client_id=? WHERE id=1",(sys.argv[2],)); db.commit()',join(directory,'test.db'),clientID]);
  await restart();
  await assertDisabled();
  await api(page.request,'/v1/settings/google','PATCH',{client_id:''});
  for(const googleEnv of [{GOOGLE_CLIENT_ID:'malformed-env-client',GOOGLE_CLIENT_SECRET:'synthetic-env-secret'},{GOOGLE_CLIENT_ID:clientID,GOOGLE_CLIENT_SECRET:''}]) {
   await restart(googleEnv);
   await assertDisabled();
  }
 } finally {
  await api(page.request,'/v1/settings/google','PATCH',{client_id:''});
  await restart();
 }
});
