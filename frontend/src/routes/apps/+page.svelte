<script lang="ts">
 import { onMount } from 'svelte';
 import { resolve } from '$app/paths';
	const base = resolve('/').replace(/\/$/, '');
 import { api, type CalendarStatus } from '$lib/api';
 import { currentUser } from '$lib/stores';
 import { Button, buttonVariants } from '$lib/components/ui/button';
 import { Input } from '$lib/components/ui/input';
 import * as Dialog from '$lib/components/ui/dialog';
 import * as Tooltip from '$lib/components/ui/tooltip';
 import { ConfirmDialog } from '$lib/components/ui/confirm-dialog';
 import { Calendar, Mail, FileText, Plug, ArrowRight, ExternalLink, RefreshCw, Copy, Info } from '@lucide/svelte';
 import { toast } from 'svelte-sonner';
 type Connection={provider:string;connected:boolean;config:{email?:string;name?:string;url?:string;inbox_access?:boolean};connected_at?:string;last_success_at?:string;error?:string};
 let loading=$state(true);let error=$state('');let calendar=$state<CalendarStatus|null>(null);let items=$state<Connection[]>([]);
 let gmailConfigured=$state(false);let gmailCallback=$state('');let busy=$state('');let message=$state('');
 let gmailHelpOpen=$state(false);let notionOpen=$state(false);let notionToken=$state('');let notionParent=$state('');
 let disconnectTarget=$state<Connection|null>(null);let disconnectOpen=$state(false);let backfillOpen=$state(false);
 const canManage=$derived(!!($currentUser?.is_admin||$currentUser?.is_owner));
 const gmail=$derived(items.find(item=>item.provider==='gmail'));const notion=$derived(items.find(item=>item.provider==='notion'));
 const calendarApps=[{id:'google',name:'Google Calendar',description:'Check busy times and add meetings to your Google calendar.'},{id:'microsoft',name:'Outlook / Microsoft 365',description:'Sync availability and meeting invitations with Microsoft.'},{id:'caldav',name:'Apple, Fastmail & CalDAV',description:'Connect iCloud, Fastmail, Nextcloud, or another CalDAV calendar.'}];
 onMount(()=>{void load();if(new URLSearchParams(window.location.search).get('error')==='gmail_denied')error='Gmail access was not approved. You can reconnect when ready.';return()=>{notionToken='';};});
 async function load(){loading=true;try{const [cal,apps]=await Promise.all([api.get<CalendarStatus>('/v1/calendar/status'),canManage?api.get<{items:Connection[];gmail_oauth_configured:boolean;gmail_callback_url:string}>('/v1/apps'):Promise.resolve(null)]);calendar=cal;if(apps){items=apps.items;gmailConfigured=apps.gmail_oauth_configured;gmailCallback=apps.gmail_callback_url;}}catch(e){error=e instanceof Error?e.message:'Could not load connections.';}finally{loading=false;}}
 async function run(name:string,action:()=>Promise<unknown>,success:string){busy=name;error='';message='';try{await action();message=success;await load();}catch(e){error=e instanceof Error?e.message:'The connection could not be updated.';}finally{busy='';}}
 async function connectNotion(){await run('notion',async()=>{await api.put('/v1/apps/notion',{token:notionToken,parent_page_id:notionParent});notionToken='';notionOpen=false;},'Notion is connected. New bookings and changes will sync to the new database.');}
 async function disconnect(){if(!disconnectTarget)return;const name=disconnectTarget.provider;await run(name,()=>api.del(`/v1/apps/${name}`),'Disconnected. Existing Notion pages are preserved.');}
 async function backfill(){await run('notion',async()=>{const result=await api.post<{queued:number}>('/v1/apps/notion/sync');toast.success(`${result.queued} bookings queued for sync`);},'Existing bookings are queued. The worker will update the Notion database.');}
 async function copy(text:string){try{await navigator.clipboard.writeText(text);toast.success('Copied');}catch{toast.error('Could not copy. Select the address and copy it.');}}
 function calendarTarget(provider:string,count:number){
  if(count)return `${base}/calendar`;
  if(provider==='caldav')return `${base}/calendar?provider=caldav`;
  if(calendar?.providers?.includes(provider))return `/v1/calendar/connect?provider=${provider}`;
  return provider==='google'?`${base}/settings/google`:`${base}/calendar`;
 }
 function safeNotionLink(value?:string){if(!value)return '';try{const u=new URL(value);return u.protocol==='https:'&&(u.hostname==='notion.so'||u.hostname.endsWith('.notion.so')||u.hostname==='notion.site'||u.hostname.endsWith('.notion.site'))?u.href:'';}catch{return '';}}
</script>


<Dialog.Root bind:open={gmailHelpOpen}>
 <Dialog.Content class="w-[calc(100vw-2rem)] max-h-[calc(100dvh-2rem)] overflow-y-auto rounded-xl">
  <Dialog.Header><Dialog.Title>Google setup and permissions</Dialog.Title><Dialog.Description>Configure Google once, then connect your Gmail mailbox.</Dialog.Description></Dialog.Header>
  <p class="text-sm text-muted-foreground leading-relaxed">Enable the Gmail API in your Google Cloud project and register this redirect URI. Google requests read and send access. Inbox listening starts only when enabled in Inbox scheduling. Connecting Gmail makes it the workspace sender.</p>
  <label class="field-label" for="gmail-callback">Gmail redirect URI</label>
  <div class="copy-field app-callback"><Input id="gmail-callback" readonly value={gmailCallback}/><Button variant="outline" size="icon" aria-label="Copy Gmail redirect URI" onclick={()=>copy(gmailCallback)}><Copy size={15}/></Button></div>
  <Dialog.Footer><a class={buttonVariants()} href={`${base}/settings/google`}>Open Google settings <ArrowRight size={15}/></a></Dialog.Footer>
 </Dialog.Content>
</Dialog.Root>
<Dialog.Root bind:open={notionOpen} onOpenChange={(open)=>{if(!open)notionToken='';}}>
 <Dialog.Content class="w-[calc(100vw-2rem)] max-h-[calc(100dvh-2rem)] overflow-y-auto rounded-xl">
  <Dialog.Header><Dialog.Title>Connect Notion</Dialog.Title><Dialog.Description>Choose a parent page for your bookings database.</Dialog.Description></Dialog.Header>
<form class="app-connector-form notion-dialog-form" onsubmit={(e)=>{e.preventDefault();void connectNotion();}}>{#if error}<p class="text-sm text-destructive" role="alert">{error}</p>{/if}<p class="field-hint">Create a Notion internal integration with read, insert, and update content access. Share a parent page with that integration. OpenSlot will create its own bookings database there.</p><a class="text-link" href="https://www.notion.so/profile/integrations" target="_blank" rel="noopener noreferrer">Open Notion integrations <ExternalLink size={14}/></a><label class="field-label" for="notion-token">Notion integration token</label><Input id="notion-token" type="password" autocomplete="off" data-sensitive bind:value={notionToken} required/><label class="field-label" for="notion-parent">Parent page URL or ID</label><Input id="notion-parent" bind:value={notionParent} placeholder="https://www.notion.so/your-page…" required/><Button type="submit" disabled={!!busy}>{busy==='notion'?'Connecting…':'Connect and create bookings database'}</Button><p class="field-hint">The app token is encrypted on this server. Existing bookings are copied only when you choose Sync existing bookings.</p></form>
 </Dialog.Content>
</Dialog.Root>

<svelte:head><title>App connections | OpenSlot</title></svelte:head>
<ConfirmDialog bind:open={disconnectOpen} title={`Disconnect ${disconnectTarget?.provider==='gmail'?'Gmail':'Notion'}?`} description={disconnectTarget?.provider==='gmail'?'Gmail will stop sending workspace email. Delivery will return to the sender configured in Email settings.':'Booking sync will stop. Existing Notion pages will stay in your workspace.'} confirmText="Disconnect" destructive onConfirm={disconnect}/>
<ConfirmDialog bind:open={backfillOpen} title="Sync existing bookings to Notion?" description="Copies existing booking titles, guest names and email addresses, times, and status into your connected Notion database. Future changes already sync automatically." confirmText="Sync bookings" onConfirm={backfill}/>
<div class="page-heading"><div><p class="eyebrow">YOUR SCHEDULE, CONNECTED</p><h1>Make your tools work together.</h1><p>Connect calendars, send booking emails, and keep your Notion workspace up to date.</p></div><span class="feature-icon"><Plug size={25}/></span></div>
{#if error&&!notionOpen}<div class="error-panel" role="alert">{error}<Button variant="ghost" onclick={()=>{error='';void load();}}>Retry</Button></div>{/if}
{#if message}<p class="connector-result" role="status">{message}</p>{/if}
{#if loading}<div class="surface loading-panel" role="status">Loading app connections…</div>{:else}
<div class="app-connector-grid">
 {#each calendarApps as app}
 {@const count=calendar?.connections?.filter(connection=>connection.provider===app.id).length??0}
 <section class="surface app-connector-card"><div class="app-connector-heading"><span class="feature-icon"><Calendar size={22}/></span><span class="soft-badge">{count?`${count} connected`:app.id==='google'&&!calendar?.providers?.includes('google')?'Setup required':'Not connected'}</span></div><h2>{app.name}</h2><p>{app.description}</p><a class={buttonVariants({variant:'outline'})} href={calendarTarget(app.id,count)} data-sveltekit-reload={calendarTarget(app.id,count).startsWith('/v1/')}>{count?'Manage calendars':app.id==='google'&&!calendar?.providers?.includes('google')?'Set up Google OAuth':'Connect calendar'}<ArrowRight size={15}/></a></section>
 {/each}
 <section class="surface app-connector-card"><div class="app-connector-heading"><span class="feature-icon"><Mail size={22}/></span><span class="soft-badge">{gmail?.error||(gmail?.connected&&!gmailConfigured)?'Needs attention':gmail?.connected?'Connected':gmailConfigured?'Not connected':'Setup required'}</span></div><h2>Gmail</h2><p>Send workspace emails and let your agent read messages addressed to your dedicated scheduling mailbox.</p>
 {#if canManage}
 <div class="app-card-actions">
 {#if gmail?.connected}<p class="app-connection-identity">{gmail.config.email}</p>{#if !gmailConfigured}<a class="text-link" href={`${base}/settings/google`}>Restore Google OAuth setup <ArrowRight size={14}/></a>{/if}{#if gmail.error}<p class="text-destructive text-xs">{gmail.error}</p>{/if}<div class="connector-actions">{#if !gmail.config.inbox_access&&gmailConfigured}<a class={buttonVariants({variant:'outline'})} href="/v1/apps/gmail/connect" data-sveltekit-reload>Grant inbox access</a>{/if}<Button variant="outline" disabled={!!busy} onclick={()=>run('gmail',()=>api.post('/v1/apps/gmail/check'),'Gmail account access verified. No message was sent.')}><RefreshCw size={14}/>Check</Button><Button variant="ghost" disabled={!!busy} onclick={()=>{disconnectTarget=gmail!;disconnectOpen=true;}}>Disconnect</Button></div>{:else if gmailConfigured}<a class={buttonVariants()} href="/v1/apps/gmail/connect" data-sveltekit-reload>Connect Gmail <ArrowRight size={15}/></a>{:else}<a class={buttonVariants({variant:'outline'})} href={`${base}/settings/google`}>Set up Google OAuth <ArrowRight size={15}/></a>{/if}
 <Tooltip.Provider><Tooltip.Root><Tooltip.Trigger class={buttonVariants({variant:'ghost',size:'icon'})} aria-label="Google setup and permissions" onclick={()=>gmailHelpOpen=true}><Info size={18}/></Tooltip.Trigger><Tooltip.Content>Google setup and permissions</Tooltip.Content></Tooltip.Root></Tooltip.Provider>
 </div>
 {:else}<p class="field-hint">A workspace administrator connects the email sender.</p>{/if}
 </section>
 <section class="surface app-connector-card"><div class="app-connector-heading"><span class="feature-icon"><FileText size={22}/></span><span class="soft-badge">{notion?.error?'Needs attention':notion?.connected?'Connected':'Not connected'}</span></div><h2>Notion</h2><p>Keep a dedicated bookings database in Notion, including guest details, meeting times, and cancellations.</p>
 {#if canManage}
 {#if notion?.connected}
 {@const destination=safeNotionLink(notion.config.url)}
 {#if destination}<a class="text-link app-connection-identity" href={destination} target="_blank" rel="noopener noreferrer">Open bookings database <ExternalLink size={14}/></a>{/if}
 {#if notion.error}<p class="text-destructive text-xs">{notion.error}</p>{/if}
 <div class="connector-actions"><Button variant="outline" disabled={!!busy} onclick={()=>run('notion',()=>api.post('/v1/apps/notion/check'),'Notion database access verified.')}><RefreshCw size={14}/>Check</Button><Button variant="outline" disabled={!!busy} onclick={()=>backfillOpen=true}>Sync existing bookings</Button><Button variant="ghost" disabled={!!busy} onclick={()=>{disconnectTarget=notion!;disconnectOpen=true;}}>Disconnect</Button></div>
 <p class="field-hint">New bookings and changes sync automatically. Last successful operation: {notion.last_success_at?new Date(notion.last_success_at).toLocaleString():'Waiting for first sync'}.</p>
 {:else}<Button variant="outline" onclick={()=>{error='';notionOpen=true;}}>Connect Notion <ArrowRight size={15}/></Button>{/if}

 {:else}<p class="field-hint">A workspace administrator connects the shared Notion destination.</p>{/if}
 </section>
 <section class="surface app-connector-card"><div class="app-connector-heading"><span class="feature-icon"><Plug size={22}/></span><span class="soft-badge">MCP + API</span></div><h2>Your agent</h2><p>Connect Codex, Claude, Antigravity, OpenAI, Bedrock, GLM, Grok, and other supported clients.</p><a class={buttonVariants({variant:'outline'})} href={`${base}/agent`}>Choose an agent <ArrowRight size={15}/></a></section>
</div>
<a class="text-link" href={`${base}/inbox`}>Set up inbox scheduling <ArrowRight size={16}/></a>
<p class="quiet-footer">App tokens let OpenSlot run the connections you enable. Model-provider keys stay in your existing agent.</p>
{/if}

<style>
 .app-card-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; width: 100%; }
 .app-connector-card > p:first-of-type { flex: 1; }
 .notion-dialog-form { border-top: 0; padding-top: 0; }
</style>
