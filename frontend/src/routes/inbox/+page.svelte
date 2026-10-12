<script lang="ts">
 import { onMount } from 'svelte';
 import { base } from '$app/paths';
 import { api, type EventType } from '$lib/api';
 import { currentUser } from '$lib/stores';
 import { Button, buttonVariants } from '$lib/components/ui/button';
 import { Input } from '$lib/components/ui/input';
 import { Switch } from '$lib/components/ui/switch';
 import * as Select from '$lib/components/ui/select';
 import { Mail, ArrowRight, RefreshCw, FlaskConical, Copy } from '@lucide/svelte';
 import { toast } from 'svelte-sonner';
 type Settings={enabled:boolean;address:string;event_slug:string;timezone:string;last_poll:string|null;last_error:string;worker_seen:string|null;worker_engine:string};
 type Activity={id:string;status:string;preview:boolean;created_at:string;result:string};
 let settings=$state<Settings>({enabled:false,address:'',event_slug:'',timezone:'UTC',last_poll:null,last_error:'',worker_seen:null,worker_engine:''});
 let items=$state<Activity[]>([]);let events=$state<EventType[]>([]);let loading=$state(true);let busy=$state(false);let error=$state('');let saved=$state(false);let gmailReady=$state(false);let gmailAddress=$state('');let engine=$state('codex');let serverURL=$state('');
 const canManage=$derived(!!($currentUser?.is_admin||$currentUser?.is_owner));
 const meetingName=$derived(events.find(e=>e.slug===settings.event_slug)?.name??'Choose a meeting type');
 const command=$derived(`./openslot agent --url '${serverURL.replaceAll("'", "'\\''")}' --engine ${engine}${engine==='webhook'?" --webhook-url 'https://your-agent.example/inbox'":''}`);
 onMount(()=>{serverURL=window.location.origin;void load(true);});
 async function load(initial=false){error='';try{const [r,types,apps]=await Promise.all([api.get<{settings:Settings;activity:Activity[]}>('/v1/inbox'),api.get<{items:EventType[]}>('/v1/event-types'),api.get<{items:{provider:string;connected:boolean;config:{email?:string;inbox_access?:boolean}}[]}>('/v1/apps')]);if(initial){settings=r.settings;saved=!!settings.event_slug;}else{settings.last_poll=r.settings.last_poll;settings.last_error=r.settings.last_error;settings.worker_seen=r.settings.worker_seen;settings.worker_engine=r.settings.worker_engine;}items=r.activity;events=types.items.filter(e=>e.is_active&&e.is_public);const gmail=apps.items.find(a=>a.provider==='gmail');gmailReady=!!(gmail?.connected&&gmail.config.inbox_access);gmailAddress=gmail?.config.email??'';if(initial&&!settings.address)settings.address=gmailAddress;}catch(e){error=e instanceof Error?e.message:'Could not load inbox settings.';}finally{loading=false;}}
 async function save(){busy=true;error='';try{await api.put('/v1/inbox',settings);saved=true;toast.success(settings.enabled?'Inbox scheduling enabled':'Settings saved. Inbox scheduling is paused.');await load();}catch(e){error=e instanceof Error?e.message:'Could not save settings.';}finally{busy=false;}}
 async function preview(){busy=true;try{await api.post('/v1/inbox/preview');toast.success('Sample queued. Start your worker to run it. No email will be sent.');await load();}catch(e){error=e instanceof Error?e.message:'Could not queue sample.';}finally{busy=false;}}
 async function copy(){try{await navigator.clipboard.writeText(command);toast.success('Copied');}catch{toast.error('Select the command to copy it.');}}
 function date(value:string|null){return value?new Date(value).toLocaleString():'Not yet';}
</script>
<svelte:head><title>Inbox scheduling | OpenSlot</title></svelte:head>
<div class="page-heading"><div><p class="eyebrow">ADD YOUR ASSISTANT TO THE CONVERSATION</p><h1>Turn an email into a meeting.</h1><p>CC your scheduling mailbox. Your agent checks availability and replies with times to choose from.</p></div><span class="feature-icon"><Mail size={25}/></span></div>
{#if !canManage}<p class="surface inbox-panel">A workspace administrator can configure inbox scheduling.</p>{:else if loading}<p role="status">Loading inbox settings…</p>{:else}
{#if error}<p class="error-panel" role="alert">{error}</p>{/if}
<div class="inbox-grid">
<section class="surface inbox-panel">
 <div class="inbox-heading"><span class="soft-badge">01 · MAILBOX</span><span class="soft-badge">{settings.enabled?'Listening enabled':'Paused'}</span></div>
 <h2>Where should people include you?</h2><p>Use a dedicated Gmail mailbox in To or CC. The address must match the Gmail account connected to OpenSlot.</p>
 {#if !gmailReady}<p class="inbox-notice">Connect Gmail with read and send access to enable listening. An older send-only connection needs to be reconnected.</p><a class={buttonVariants({variant:'outline'})} href={`${base}/apps`}>Manage Gmail <ArrowRight size={15}/></a>{:else}<p class="field-hint">Connected mailbox: {gmailAddress}</p>{/if}
 <form onsubmit={(e)=>{e.preventDefault();void save();}} class="inbox-form">
 <label for="inbox-address" class="field-label">Dedicated email address</label><Input id="inbox-address" type="email" required bind:value={settings.address} placeholder="scheduling@your-domain.com"/>
 <label for="inbox-meeting" class="field-label">Meeting type</label><Select.Root type="single" bind:value={settings.event_slug}><Select.Trigger id="inbox-meeting" class="w-full">{meetingName}</Select.Trigger><Select.Content>{#each events as event}<Select.Item value={event.slug} label={event.name}>{event.name}</Select.Item>{/each}</Select.Content></Select.Root>
 {#if !events.length}<a class="text-link" href={`${base}/event-types`}>Create a public meeting type first <ArrowRight size={14}/></a>{/if}
 <label for="inbox-timezone" class="field-label">Timezone for offered times</label><Input id="inbox-timezone" required bind:value={settings.timezone} placeholder="Europe/London"/>
 <div class="inbox-toggle"><div><label for="inbox-enabled">Send available slots automatically</label><p class="field-hint">Checks new addressed mail every minute while your worker runs. Replies go to the original sender.</p></div><Switch id="inbox-enabled" bind:checked={settings.enabled} disabled={!gmailReady&&!settings.enabled}/></div>
 <Button type="submit" disabled={busy||!settings.event_slug}>{busy?'Saving…':'Save settings'}</Button><p class="field-hint">Saving restarts listening from now and cancels queued requests from the previous configuration. Existing emails are not imported.</p>
 </form>
</section>
<section class="surface inbox-panel"><span class="soft-badge">02 · YOUR AGENT</span><h2>Choose who handles the conversation.</h2><p>Run a worker on the machine where your agent is signed in. Provider keys stay there.</p>
 <div class="inbox-engines" aria-label="Agent runtime">{#each [{id:'codex',name:'Codex'},{id:'claude',name:'Claude Code'},{id:'webhook',name:'External webhook'}] as runtime}<Button variant={engine===runtime.id?'default':'outline'} onclick={()=>engine=runtime.id}>{runtime.name}</Button>{/each}</div>
 <p class="field-hint">Create an administrator’s OpenSlot key in <a class="text-link" href={`${base}/agent`}>Your agent</a> and set <code>OPENSLOT_API_KEY</code> in the worker’s environment.</p>
 <pre class="embed-code"><code>{command}</code></pre><Button variant="outline" size="sm" onclick={copy}><Copy size={14}/>Copy command</Button>
 {#if engine==='webhook'}<p class="field-hint">Set <code>OPENSLOT_WEBHOOK_TOKEN</code> if your endpoint requires a bearer token. The worker posts a request ID, instructions, and a temporary MCP URL and token. Your agent calls its four tools to complete the task. Use any provider, including OpenAI, Anthropic, Bedrock, GLM, or Grok.</p>{:else}<p class="field-hint">Requires a current {engine==='codex'?'Codex':'Claude Code'} CLI. The worker starts a fresh session with only the inbox MCP connection. Add <code>--model</code> to choose a model available to your account.</p>{/if}
 <div class="inbox-health"><p><strong>Last worker check-in</strong><br/>{date(settings.worker_seen)}{settings.worker_engine?` · ${settings.worker_engine}`:''}</p><p><strong>Last inbox check</strong><br/>{date(settings.last_poll)}</p>{#if settings.last_error}<p role="alert" class="text-destructive">{settings.last_error}</p>{/if}</div>
 <div class="inbox-sample"><FlaskConical size={20}/><div><h3>Try a sample conversation</h3><p>Run the full agent and availability flow with a synthetic email. The result appears below. This test cannot send email.</p><Button variant="outline" disabled={busy||!saved} onclick={preview}>Run sample</Button></div></div>
</section>
</div>
<section class="surface inbox-panel inbox-activity"><div class="inbox-heading"><div><p class="eyebrow">RECENT REQUESTS</p><h2>What your assistant has done</h2></div><Button variant="outline" size="sm" onclick={()=>load()}><RefreshCw size={14}/>Refresh</Button></div>
{#if !items.length}<p>Requests will appear here when someone includes your mailbox, or when you run a sample.</p>{:else}<div class="inbox-activity-list">{#each items as item}<article><div class="inbox-heading"><strong>{item.preview?'Sample conversation':'Email request'}</strong><span class="soft-badge">{item.status==='sending'?'Delivery started':item.status}</span></div><p class="field-hint">{date(item.created_at)}</p>{#if item.result}<pre class="inbox-result">{item.result}</pre>{/if}{#if item.status==='pending'||item.status==='processing'}<p class="field-hint">{item.status==='pending'?'Waiting for an agent worker.':'Agent is working. An expired task can be claimed again after 15 minutes.'}</p>{/if}</article>{/each}</div>{/if}
</section><p class="quiet-footer">Your agent offers times. Guests choose and book through your booking page. No meeting is created by the email workflow.</p>
{/if}
<style>
 .inbox-grid{display:grid;grid-template-columns:1fr 1fr;gap:1.25rem;align-items:start}.inbox-panel{padding:1.5rem}.inbox-panel h2{font-size:1.15rem;font-weight:650;margin:.8rem 0}.inbox-panel p{color:var(--muted-foreground);line-height:1.6;font-size:.9rem}.inbox-heading{display:flex;align-items:center;justify-content:space-between;gap:1rem}.inbox-form{display:grid;gap:.7rem;margin-top:1.4rem}.inbox-form .field-label{margin-top:.3rem}.inbox-toggle{display:flex;align-items:center;gap:1rem;padding:1rem 0}.inbox-toggle label{font-size:.9rem;font-weight:600}.inbox-toggle p{font-size:.78rem;margin:.4rem 0 0}.inbox-engines{display:flex;flex-wrap:wrap;gap:.5rem;margin:1.4rem 0}.inbox-health{display:grid;grid-template-columns:1fr 1fr;gap:1rem;border-top:1px solid var(--border);margin-top:1.5rem;padding-top:.5rem}.inbox-sample{display:flex;gap:.8rem;background:var(--muted);border-radius:.8rem;padding:1rem;margin-top:1rem}.inbox-sample :global(svg){flex-shrink:0;margin-top:.2rem}.inbox-sample h3{font-size:.9rem;font-weight:650}.inbox-sample p{font-size:.8rem;margin:.5rem 0 .8rem}.inbox-notice{padding:.8rem;background:var(--muted);border-radius:.5rem}.inbox-activity{margin-top:1.25rem}.inbox-activity-list article{padding:1rem 0;border-top:1px solid var(--border)}.inbox-result{white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;font-size:.85rem;background:var(--muted);padding:1rem;border-radius:.6rem}.embed-code{overflow:auto;max-width:100%;margin:1rem 0}.inbox-panel{min-width:0}@media(max-width:1000px){.inbox-grid{grid-template-columns:1fr}}@media(max-width:500px){.inbox-panel{padding:1rem}.inbox-health{grid-template-columns:1fr}.inbox-heading{flex-wrap:wrap}}
</style>
