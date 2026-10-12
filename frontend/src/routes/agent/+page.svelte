<script lang="ts">
 import { onMount } from 'svelte';
 import { base } from '$app/paths';
 import { Plug, Copy, Download, ArrowRight, ExternalLink, Check, Search, RefreshCw } from '@lucide/svelte';
 import { Button } from '$lib/components/ui/button';
 import { Input } from '$lib/components/ui/input';
 import { api } from '$lib/api';
 import { toast } from 'svelte-sonner';
 import APIKeyManager from '$lib/components/api-key-manager.svelte';
 import { connectorSetup, connectorCommand, checkMCPConnection, needsPublicURL, type AgentConnector } from '$lib/agent-connectors';

 let connectors = $state<AgentConnector[]>([]);
 let selectedId = $state('codex');
 let category = $state<'all'|'app'|'provider'>('all');
 let search = $state('');
 let endpoint = $state('');
 let error = $state('');
 let loading = $state(true);
 let token = $state('');
 let tokenId = $state('');
 let checking = $state(false);
 let checkResult = $state('');
 let checkError = $state('');
 let generation = 0;
 const selected = $derived(connectors.find(item=>item.id===selectedId));
 const visible = $derived(connectors.filter(item=>(category==='all'||category===item.category) && `${item.name} ${item.description}`.toLowerCase().includes(search.toLowerCase())));
 const code = $derived.by(()=>{try{return selected&&endpoint?connectorSetup(selected,endpoint,token):'';}catch{return '';}});
 const command = $derived.by(()=>{try{return selected&&endpoint?connectorCommand(selected,endpoint):null;}catch{return null;}});
 const cloudWarning = $derived(selected?.public_url && needsPublicURL(endpoint));

 onMount(()=>{void load();return()=>{generation++;token='';};});
 async function load() {
  loading=true;error='';
  try {const result=await api.get<{items:AgentConnector[];mcp_url:string}>('/v1/agent-connectors');connectors=result.items;endpoint=result.mcp_url||`${window.location.origin}/mcp`;}
  catch(e){error=e instanceof Error?e.message:'Could not load connectors.';}finally{loading=false;}
 }
 function choose(id:string){generation++;selectedId=id;token='';tokenId='';checkResult='';checkError='';checking=false;}
 async function copy(text:string){try{await navigator.clipboard.writeText(text);toast.success('Copied');}catch{toast.error('Could not copy. Select the text and copy it.');}}
 function download(){if(!selected||!code)return;const url=URL.createObjectURL(new Blob([code],{type:'text/plain;charset=utf-8'}));const a=document.createElement('a');a.href=url;a.download=selected.filename;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}
 function created(value:{id:string;key:string}){generation++;token=value.key;tokenId=value.id;checkResult='';checkError='';}
 function revoked(id:string){if(id===tokenId){generation++;token='';tokenId='';checkResult='';checkError='';checking=false;}}
 async function check(){const current=++generation;checking=true;checkResult='';checkError='';try{const tools=await checkMCPConnection(`${window.location.origin}/mcp`,token);if(current===generation)checkResult=`OpenSlot responded with ${tools.length} tools. Finish setup in ${selected?.name} to connect the agent.`;}catch(e){if(current===generation)checkError=e instanceof Error?e.message:'Could not reach OpenSlot.';}finally{if(current===generation)checking=false;}}
</script>

<svelte:head><title>Your agent | OpenSlot</title></svelte:head>
<div class="page-heading"><div><p class="eyebrow">BRING THE AGENT YOU ALREADY USE</p><h1>Connect your agent.</h1><p>Give it your real availability and booking tools. Your model account stays with your agent.</p></div><span class="feature-icon"><Plug size={25}/></span></div>
{#if error}<div class="error-panel" role="alert">{error}<Button variant="outline" onclick={load}>Try again</Button></div>{/if}
{#if loading}<div class="surface loading-panel" role="status">Loading connectors…</div>{:else if !error}
 <div class="connector-picker-tools"><div class="segmented" aria-label="Connector category">{#each [{id:'all',label:'All'},{id:'app',label:'Agent apps'},{id:'provider',label:'Model providers'}] as tab}<Button variant={category===tab.id?'default':'ghost'} aria-pressed={category===tab.id} onclick={()=>category=tab.id as typeof category}>{tab.label}</Button>{/each}</div><div class="connector-search"><Search size={16}/><Input aria-label="Search connectors" placeholder="Find your agent" bind:value={search}/></div></div>
<div class="connector-workspace">
<section class="surface connector-picker" aria-label="Agent connectors">
 <div class="connector-grid">{#each visible as item}<Button variant="ghost" class={`connector-card ${item.id===selectedId?'selected':''}`} aria-label={`Connect ${item.name}`} aria-pressed={item.id===selectedId} onclick={()=>choose(item.id)}><span class="connector-monogram" aria-hidden="true">{item.name.slice(0,2)}</span><span><strong>{item.name}</strong><small>{item.description}</small></span>{#if selectedId===item.id}<Check class="connector-check" size={16}/>{/if}</Button>{/each}</div>
 {#if visible.length===0}<p class="field-hint">No matching connector. Try another name or choose All.</p>{/if}
</section>
{#if selected}
<div class="connector-setup">
<section class="surface connector-detail" aria-label={`${selected.name} setup`}>
 <div class="section-heading"><div><p class="eyebrow">SELECTED CONNECTOR</p><h2>{selected.name}</h2></div><a class="text-link" href={selected.docs} target="_blank" rel="noopener noreferrer">Provider instructions <ExternalLink size={14}/></a></div>
 <div class="connector-detail-body">
 <label class="field-label" for="mcp-endpoint">MCP server URL</label><div class="copy-field"><Input id="mcp-endpoint" readonly value={endpoint}/><Button variant="outline" onclick={()=>copy(endpoint)}><Copy size={15}/> Copy URL</Button></div>
 {#if cloudWarning}<div class="connector-notice" role="status"><strong>A public HTTPS address is needed.</strong><p>{selected.name} connects from the cloud. Deploy OpenSlot at a reachable HTTPS address and set BASE_URL to it. This local address will work with agents running on this computer.</p></div>{/if}
 <ol class="connector-steps"><li>{selected.oauth?'Add the server URL in your app and sign in to OpenSlot.':'Create a separate OpenSlot key below, or use one you already have.'}</li><li>{selected.destination}</li><li>Ask your agent: “List my OpenSlot meeting types.”</li></ol>
 {#if !selected.oauth}
 <div class="connector-credential"><label class="field-label" for="connector-token">OpenSlot API key</label><Input id="connector-token" type="password" autocomplete="off" data-sensitive bind:value={token} oninput={()=>{generation++;checkResult='';checkError='';checking=false;}} placeholder="Create a key below or paste an existing key"/><p class="field-hint">This field holds an OpenSlot key, never your OpenAI, Anthropic, AWS, GLM, or Grok credentials. It is cleared when you leave.</p></div>
 {#if selected.format==='python'||selected.id==='codex'}<p class="field-hint">Set <code>OPENSLOT_API_KEY</code> in the environment that launches your agent. The downloaded example reads it there.</p>{/if}
 {/if}
 <div class="snippet-header"><span class="field-label">{selected.format==='python'?'Agent-side example':selected.format==='text'?'Connection steps':'Connection configuration'}</span><div class="connector-actions"><Button variant="outline" size="sm" onclick={()=>copy(code)}><Copy size={14}/> Copy</Button><Button variant="outline" size="sm" onclick={download}><Download size={14}/> Download</Button></div></div>
 <pre class="embed-code connector-code" data-sensitive={token&&selected.format==='json'?true:undefined}><code>{code}</code></pre>
 {#if command}<details class="connector-command"><summary>Or add it from your terminal</summary><pre class="embed-code"><code>{command}</code></pre><Button variant="outline" size="sm" onclick={()=>copy(command!)}><Copy size={14}/> Copy command</Button></details>{/if}
 {#if selected.id==='bedrock'}<p class="field-hint">This is an AgentCore harness connector. It is not a native Converse API request. Agents using Bedrock through Codex or Claude Code use that app’s connector above.</p>{/if}
 {#if selected.id==='grok'}<p class="field-hint">xAI remote MCP does not support OpenAI’s approval parameter. Keep booking confirmations in your agent’s workflow.</p>{/if}
 {#if !selected.oauth}<div class="connector-test"><Button onclick={check} disabled={checking||!token.trim()}><RefreshCw size={15} class={checking?'animate-spin':''}/>{checking?'Checking…':'Check OpenSlot connection'}</Button><p class="field-hint">Discovers scheduling tools without reading bookings or changing your calendar. This checks OpenSlot, not your model account or cloud reachability.</p></div>{/if}
 {#if checkResult}<p class="connector-result" role="status"><Check size={17}/>{checkResult}</p>{/if}{#if checkError}<p class="error-panel" role="alert">{checkError}</p>{/if}
 </div>
</section>
{#if !selected.oauth}<section class="surface key-manager">{#key selectedId}<APIKeyManager connectorId={selectedId} suggestedName={`${selected.name} agent`} oncreated={created} onrevoked={revoked}/>{/key}</section>{/if}
</div>
{/if}
</div>
<div class="agent-guidance"><Plug size={20}/><div><strong>Your agent, your permissions.</strong><p>Keys inherit your OpenSlot role. Revoke a key to stop its access. Creating a key does not mean the external agent has connected.</p></div></div>
<a class="text-link" href={`${base}/connections`}>Manage signed-in agents <ArrowRight size={16}/></a>
{/if}
