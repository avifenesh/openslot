<script lang="ts">
 import { onMount } from 'svelte';
 import { base } from '$app/paths';
 import { api, type EventType } from '$lib/api';
 import { currentUser } from '$lib/stores';
 import { Button, buttonVariants } from '$lib/components/ui/button';
 import { Input } from '$lib/components/ui/input';
 import { Switch } from '$lib/components/ui/switch';
 import * as Select from '$lib/components/ui/select';
 import { Mail, Bot, ArrowRight, RefreshCw, FlaskConical, Copy, Check, ChevronDown, Inbox } from '@lucide/svelte';
 import { toast } from 'svelte-sonner';
 type Settings={enabled:boolean;address:string;event_slug:string;timezone:string;last_poll:string|null;last_error:string;worker_seen:string|null;worker_engine:string};
 type Activity={id:string;status:string;preview:boolean;created_at:string;result:string};
 const empty:Settings={enabled:false,address:'',event_slug:'',timezone:'UTC',last_poll:null,last_error:'',worker_seen:null,worker_engine:''};
 let settings=$state<Settings>({...empty});
 let persisted=$state<Settings|null>(null);
 let items=$state<Activity[]>([]);
 let events=$state<EventType[]>([]);
 let loading=$state(true);
 let loaded=$state(false);
 let busy=$state('');
 let error=$state('');
 let gmailReady=$state(false);
 let gmailAddress=$state('');
 let engine=$state('codex');
 let serverURL=$state('');
 let refreshVersion=0;
 const canManage=$derived(!!($currentUser?.is_admin||$currentUser?.is_owner));
 const meetingName=$derived(events.find(e=>e.slug===settings.event_slug)?.name??'Choose a meeting type');
 function values(s:Settings){return JSON.stringify([s.address,s.event_slug,s.timezone,s.enabled]);}
 const dirty=$derived(!persisted||values(settings)!==values(persisted));
 const workerName=$derived(({codex:'Codex',claude:'Claude Code',webhook:'Webhook'} as Record<string,string>)[persisted?.worker_engine??'']??'Agent');
 const status=$derived(!persisted?.event_slug?'Setup needed':!persisted?.enabled?'Paused':gmailReady&&!persisted?.last_error?'Enabled':'Needs attention');
 const command=$derived(`./openslot agent --url '${serverURL.replaceAll("'", "'\\''")}' --engine ${engine}${engine==='webhook'?" --webhook-url 'https://your-agent.example/inbox'":''}`);
 onMount(()=>{
  serverURL=window.location.origin;
  if(canManage)void load();else loading=false;
  // Status refresh stays separate from form state so edits are never overwritten.
  const timer=setInterval(()=>{if(canManage&&loaded&&!busy)void refresh(true);},30_000);
  return()=>clearInterval(timer);
 });
 async function load(){
  loading=true;error='';
  try{
   const [r,types,apps]=await Promise.all([
    api.get<{settings:Settings;activity:Activity[]}>('/v1/inbox'),
    api.get<{items:EventType[]}>('/v1/event-types'),
    api.get<{items:{provider:string;connected:boolean;config:{email?:string;inbox_access?:boolean}}[]}>('/v1/apps')
   ]);
   persisted=r.settings;settings={...r.settings};items=r.activity;
   events=types.items.filter(e=>e.is_active&&e.is_public&&!e.archived);
   const gmail=apps.items.find(a=>a.provider==='gmail');
   gmailReady=!!(gmail?.connected&&gmail.config.inbox_access);gmailAddress=gmail?.config.email??'';
   if(!settings.address)settings.address=gmailAddress;
   if(['codex','claude','webhook'].includes(r.settings.worker_engine))engine=r.settings.worker_engine;
   loaded=true;
  }catch(e){error=e instanceof Error?e.message:'Could not load inbox settings.';}finally{loading=false;}
 }
 async function refresh(quiet=false){
  const version=++refreshVersion;
  if(!quiet){busy='refresh';error='';}
  try{const r=await api.get<{settings:Settings;activity:Activity[]}>('/v1/inbox');if(version===refreshVersion){persisted=r.settings;items=r.activity;}}
  catch(e){if(!quiet&&version===refreshVersion)error=e instanceof Error?e.message:'Could not refresh activity.';}
  finally{if(!quiet&&version===refreshVersion)busy='';}
 }
 async function save(){
  refreshVersion++;busy='save';error='';const submitted={...settings};
  try{await api.put('/v1/inbox',submitted);persisted=submitted;toast.success(submitted.enabled?'Inbox scheduling enabled':'Settings saved. Inbox scheduling is paused.');await refresh(true);}
  catch(e){error=e instanceof Error?e.message:'Could not save settings.';}finally{busy='';}
 }
 async function preview(){
  busy='sample';error='';
  try{await api.post('/v1/inbox/preview');toast.success('Sample queued. Start your worker to run it. No email will be sent.');await refresh(true);}
  catch(e){error=e instanceof Error?e.message:'Could not queue sample.';}finally{busy='';}
 }
 async function copy(){try{await navigator.clipboard.writeText(command);toast.success('Copied');}catch{toast.error('Select the command to copy it.');}}
 function date(value:string|null){return value?new Date(value).toLocaleString():'Not yet';}
 function activityLabel(value:string){return ({pending:'Queued',processing:'Working',previewed:'Sample ready',sent:'Offer sent',ignored:'No reply needed',cancelled:'Cancelled',sending:'Check delivery',uncertain:'Check delivery'} as Record<string,string>)[value]??value;}
</script>

<svelte:head><title>Inbox scheduling | OpenSlot</title></svelte:head>
<div class="page-heading inbox-page-heading">
 <div><p class="eyebrow">YOUR EMAIL ASSISTANT</p><h1>Inbox scheduling</h1><p>CC your assistant. It finds a time and sends the options.</p></div>
 {#if canManage&&loaded}<Button variant="outline" disabled={!!busy||!persisted?.event_slug||dirty} onclick={preview}><FlaskConical size={17}/>{busy==='sample'?'Queuing…':'Run sample'}</Button>{/if}
</div>
{#if !canManage}
 <p class="surface inbox-panel">A workspace administrator can configure inbox scheduling.</p>
{:else if loading}
 <p class="surface loading-panel" role="status">Loading inbox settings…</p>
{:else}
 {#if error}<div class="error-panel" role="alert">{error}{#if !loaded}<Button variant="outline" onclick={load}>Try again</Button>{/if}</div>{/if}
 {#if loaded}
 <div class="inbox-status-strip" aria-label="Scheduling status">
  <span class="inbox-state" class:active={status==='Enabled'}><span class="status-dot"></span>{status}</span>
  <span><Mail size={16}/>{gmailReady?gmailAddress:'Mailbox not connected'}</span>
  <span><Bot size={16}/>{persisted?.worker_seen?`Last worker: ${workerName}`:'Agent not connected'}</span>
 </div>
 <div class="inbox-grid">
  <section class="surface inbox-panel">
   <div class="inbox-section-title"><span class="inbox-step">1</span><div><h2>Connect your mailbox</h2><p>Use a dedicated address in To or CC.</p></div></div>
   {#if !gmailReady}<div class="inbox-notice"><Mail size={19}/><div><strong>Gmail needs to be connected</strong><p>Allow read and send access for your scheduling mailbox.</p><a class="text-link" href={`${base}/apps`}>Manage Gmail <ArrowRight size={14}/></a></div></div>{/if}
   <form onsubmit={(e)=>{e.preventDefault();void save();}} class="inbox-form">
    <div><label for="inbox-address" class="field-label">Dedicated email address</label><Input id="inbox-address" type="email" required bind:value={settings.address} placeholder="scheduling@your-domain.com"/><p class="field-hint">Must match your connected Gmail account.</p></div>
    <div><label for="inbox-meeting" class="field-label">Meeting type</label><Select.Root type="single" bind:value={settings.event_slug}><Select.Trigger id="inbox-meeting" class="w-full">{meetingName}</Select.Trigger><Select.Content>{#each events as event}<Select.Item value={event.slug} label={event.name}>{event.name}</Select.Item>{/each}</Select.Content></Select.Root>{#if !events.length}<a class="text-link" href={`${base}/event-types`}>Create a public meeting type <ArrowRight size={14}/></a>{/if}</div>
    <div><label for="inbox-timezone" class="field-label">Timezone for offered times</label><Input id="inbox-timezone" required bind:value={settings.timezone} placeholder="Europe/London"/></div>
    <div class="inbox-toggle"><div><label for="inbox-enabled">Send available slots automatically</label><p>{gmailReady?'Reply to new requests with available times.':'Connect Gmail to turn on automatic replies.'}</p></div><Switch id="inbox-enabled" bind:checked={settings.enabled} disabled={!gmailReady&&!settings.enabled}/></div>
    <div class="inbox-save"><Button type="submit" disabled={!!busy||!settings.event_slug||!dirty}>{busy==='save'?'Saving…':'Save settings'}</Button><span role="status">{dirty?'Unsaved changes':persisted?.event_slug?'Settings saved':'Not configured'}</span></div>
    <p class="field-hint">Saving applies to new email and clears pending requests.</p>
   </form>
  </section>
  <section class="surface inbox-panel">
   <div class="inbox-section-title"><span class="inbox-step">2</span><div><h2>Connect your agent</h2><p>Choose the agent you already use.</p></div></div>
   <div class="inbox-engines" aria-label="Agent runtime">{#each [{id:'codex',name:'Codex'},{id:'claude',name:'Claude Code'},{id:'webhook',name:'External webhook'}] as runtime}<Button variant={engine===runtime.id?'default':'ghost'} aria-pressed={engine===runtime.id} onclick={()=>engine=runtime.id}>{runtime.name}</Button>{/each}</div>
   <ol class="inbox-agent-steps"><li>Create an OpenSlot key in <a class="text-link" href={`${base}/agent`}>Your agent <ArrowRight size={13}/></a>.</li><li>Set <code>OPENSLOT_API_KEY</code> on your agent’s machine.</li><li>Run this command there.</li></ol>
   <div class="inbox-command"><div><span>{engine==='webhook'?'Webhook worker':engine==='claude'?'Claude Code worker':'Codex worker'}</span><Button variant="ghost" size="sm" onclick={copy} aria-label="Copy command"><Copy size={14}/>Copy</Button></div><pre class="embed-code"><code>{command}</code></pre></div>
   <details class="inbox-details"><summary>Connection details <ChevronDown size={15}/></summary><div>{#if engine==='webhook'}<p>Use any provider, including OpenAI, Anthropic, Bedrock, GLM, or Grok. Your endpoint receives instructions and a temporary MCP connection for each request.</p><p>For a protected endpoint, set <code>OPENSLOT_WEBHOOK_TOKEN</code>. Use a public HTTPS OpenSlot URL when your agent runs remotely.</p>{:else}<p>Sign in to the current {engine==='codex'?'Codex':'Claude Code'} CLI first. Add <code>--model</code> to use a model available to your account.</p>{/if}<p>Your model credentials stay with your agent.</p></div></details>
   <div class="inbox-health"><div><span>Agent check-in</span><strong>{date(persisted?.worker_seen??null)}</strong></div><div><span>Inbox checked</span><strong>{date(persisted?.last_poll??null)}</strong></div></div>
   {#if persisted?.last_error}<p role="alert" class="inbox-error">{persisted.last_error}</p>{/if}
   <div class="inbox-sample"><FlaskConical size={19}/><p><strong>Try it before going live.</strong> Run a sample to see the reply here. No email is sent.{#if dirty}<span>Save your settings first.</span>{/if}</p></div>
  </section>
 </div>
 <section class="surface inbox-activity">
  <div class="inbox-activity-heading"><div><h2>Recent activity</h2><p>Requests and replies from your assistant.</p></div><Button variant="ghost" size="sm" disabled={!!busy} onclick={()=>refresh()}><RefreshCw size={15} class={busy==='refresh'?'animate-spin':''}/>Refresh</Button></div>
  {#if !items.length}<div class="inbox-empty"><Inbox size={28}/><div><h3>Your conversations will appear here</h3><p>Run a sample, or include your connected mailbox in an email.</p></div></div>
  {:else}<div class="inbox-activity-list">{#each items as item,index}<article><div class="inbox-activity-row"><span class="inbox-activity-icon">{#if item.preview}<FlaskConical size={18}/>{:else}<Mail size={18}/>{/if}</span><div><strong>{item.preview?'Sample conversation':'Email request'}</strong><p>{date(item.created_at)}</p></div><span class="soft-badge" class:needs-attention={item.status==='sending'||item.status==='uncertain'}>{activityLabel(item.status)}</span></div>
   {#if item.result}<details class="inbox-reply" open={index===0}><summary>{item.status==='sent'||item.status==='previewed'?'View reply':'View details'}<ChevronDown size={15}/></summary><pre class="inbox-result">{item.result}</pre></details>{/if}
   {#if item.status==='pending'||item.status==='processing'}<p class="inbox-request-note">{item.status==='pending'?'Waiting for an agent worker.':'Your agent is checking this request.'}</p>{/if}
  </article>{/each}</div>{/if}
 </section>
 <p class="quiet-footer"><Check size={14}/>Times are checked before sending. Guests choose and book through your booking page.</p>
 {/if}
{/if}

<style>
 .inbox-page-heading{margin-bottom:24px}.inbox-page-heading>div{flex:1}.inbox-status-strip{display:flex;align-items:center;flex-wrap:wrap;gap:12px 24px;margin-bottom:24px;font-size:13px;color:var(--muted-foreground)}.inbox-status-strip>span{display:flex;align-items:center;gap:8px;overflow-wrap:anywhere}.inbox-state{font-weight:600;color:var(--foreground)}.inbox-state .status-dot{margin:0;background:var(--muted-foreground)}.inbox-state.active .status-dot{background:var(--primary)}
 .inbox-grid{display:grid;grid-template-columns:1fr 1fr;gap:24px;align-items:start}.inbox-panel{padding:24px;min-width:0}.inbox-section-title{display:flex;gap:12px;align-items:flex-start;margin-bottom:24px}.inbox-section-title h2{font-size:17px;line-height:1.5;margin:0}.inbox-section-title p{font-size:13px;color:var(--muted-foreground);margin-top:3px}.inbox-step{display:flex;align-items:center;justify-content:center;border:1px solid var(--border);border-radius:8px;width:30px;height:30px;flex-shrink:0;font-size:13px;font-weight:600;color:var(--muted-foreground)}
 .inbox-notice{display:flex;gap:12px;padding:14px;background:var(--muted);border-radius:8px;margin-bottom:20px}.inbox-notice :global(svg){flex-shrink:0;color:var(--primary);margin-top:2px}.inbox-notice strong{font-size:13px}.inbox-notice p{font-size:13px;color:var(--muted-foreground);margin:4px 0 8px;line-height:1.5}.inbox-notice .text-link{font-size:13px}.inbox-form{display:grid;gap:18px}.field-label{font-size:13px;margin-bottom:7px}.field-hint{font-size:12px;margin-top:6px;line-height:1.5}.inbox-toggle{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:18px 0;border-top:1px solid var(--border);border-bottom:1px solid var(--border)}.inbox-toggle label{font-size:13px;font-weight:600}.inbox-toggle p{color:var(--muted-foreground);font-size:12px;line-height:1.5;margin-top:5px}.inbox-save{display:flex;align-items:center;gap:16px}.inbox-save span{font-size:12px;color:var(--muted-foreground)}.inbox-form>.field-hint{margin-top:-5px}
 .inbox-engines{display:flex;gap:3px;background:var(--muted);padding:4px;border-radius:10px}.inbox-engines :global(button){flex:1;padding-inline:10px;font-size:13px}.inbox-agent-steps{list-style:decimal;padding-left:20px;margin:24px 0;font-size:13px;line-height:1.7;color:var(--muted-foreground)}.inbox-agent-steps li{padding-left:4px;margin-block:8px}.inbox-agent-steps .text-link{font-size:13px}.inbox-command{border:1px solid var(--border);background:var(--background);border-radius:10px;overflow:hidden}.inbox-command>div{display:flex;justify-content:space-between;align-items:center;padding:7px 12px;border-bottom:1px solid var(--border);font-size:12px;color:var(--muted-foreground)}.inbox-command .embed-code{border:0;border-radius:0;margin:0;padding:16px;font-size:12px;line-height:1.7}.inbox-details{margin-top:14px}.inbox-details summary,.inbox-reply summary{display:flex;align-items:center;gap:8px;font-size:13px;cursor:pointer;color:var(--primary);padding-block:8px;list-style:none}.inbox-details summary::-webkit-details-marker,.inbox-reply summary::-webkit-details-marker{display:none}.inbox-details>div{padding:4px 0 12px;color:var(--muted-foreground);font-size:13px;line-height:1.6}.inbox-details p+p{margin-top:10px}.inbox-details[open]>summary :global(svg),.inbox-reply[open]>summary :global(svg){transform:rotate(180deg)}
 .inbox-health{display:grid;grid-template-columns:1fr 1fr;gap:16px;border-top:1px solid var(--border);margin-top:16px;padding-top:20px;font-size:12px}.inbox-health span{display:block;color:var(--muted-foreground);margin-bottom:5px}.inbox-health strong{font-weight:500;font-size:12px}.inbox-error{font-size:13px;color:var(--destructive);margin-top:16px}.inbox-sample{display:flex;gap:10px;padding-top:20px;color:var(--muted-foreground);font-size:13px;line-height:1.6}.inbox-sample :global(svg){flex-shrink:0;margin-top:3px;color:var(--primary)}.inbox-sample strong{color:var(--foreground);font-weight:500}.inbox-sample span{display:block}
 .inbox-activity{margin-top:24px}.inbox-activity-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:20px 24px;border-bottom:1px solid var(--border)}.inbox-activity-heading h2{font-size:17px}.inbox-activity-heading p{font-size:13px;color:var(--muted-foreground);margin-top:4px}.inbox-empty{display:flex;align-items:center;justify-content:center;gap:16px;padding:36px 24px;color:var(--muted-foreground)}.inbox-empty h3{font-size:14px;font-weight:500;color:var(--foreground)}.inbox-empty p{font-size:13px;margin-top:4px}.inbox-activity-list article{padding:20px 24px;border-bottom:1px solid var(--border)}.inbox-activity-list article:last-child{border:0}.inbox-activity-row{display:flex;gap:12px;align-items:center}.inbox-activity-icon{display:flex;align-items:center;justify-content:center;width:36px;height:36px;flex-shrink:0;background:var(--muted);border-radius:8px;color:var(--muted-foreground)}.inbox-activity-row>div{flex:1}.inbox-activity-row strong{font-size:14px;font-weight:500}.inbox-activity-row p{font-size:12px;color:var(--muted-foreground);margin-top:3px}.inbox-activity-row .soft-badge{font-size:12px;background:var(--muted);color:var(--muted-foreground)}.inbox-activity-row .needs-attention{color:var(--destructive)}.inbox-result{white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;font-size:13px;line-height:1.65;background:var(--background);border:1px solid var(--border);padding:16px;border-radius:8px}.inbox-reply{margin-top:10px}.inbox-request-note{font-size:13px;color:var(--muted-foreground);margin:12px 0 0 48px}.quiet-footer{display:flex;align-items:center;justify-content:center;gap:8px;font-size:12px;line-height:1.6}
 @media(max-width:1100px){.inbox-grid{grid-template-columns:1fr}.inbox-engines{max-width:450px}}@media(max-width:767px){.inbox-status-strip{gap:10px;flex-direction:column;align-items:flex-start}.inbox-panel{padding:20px}.inbox-engines :global(button){font-size:12px;padding-inline:6px;white-space:normal;line-height:1.3}.inbox-page-heading{align-items:flex-start}.inbox-activity-heading,.inbox-activity-list article{padding:18px}.inbox-health{gap:12px}.inbox-empty{align-items:flex-start}.inbox-empty :global(svg){flex-shrink:0}.quiet-footer{align-items:flex-start;text-align:left}.inbox-activity-row{flex-wrap:wrap}.inbox-command .embed-code{font-size:12px}.inbox-save{flex-wrap:wrap}}
</style>
