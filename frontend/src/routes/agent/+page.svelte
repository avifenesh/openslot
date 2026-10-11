<script lang="ts">
	import { onMount } from 'svelte';
	import { base } from '$app/paths';
	import { Plug, Copy, Calendar, Clock, ArrowRight, Key, RefreshCw, X } from '@lucide/svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { toast } from 'svelte-sonner';
	import APIKeyManager from '$lib/components/api-key-manager.svelte';
	let origin = $state('');
	let example = $state<'mcp' | 'api'>('mcp');
	onMount(() => { origin = window.location.origin; });
	const endpoint = $derived(`${origin}/mcp`);
	const configuration = $derived(JSON.stringify({mcpServers:{openslot:{url:endpoint,headers:{Authorization:'Bearer YOUR_API_KEY'}}}}, null, 2));
	const request = $derived(`curl '${origin}/v1/event-types' \\\n  -H 'Authorization: Bearer YOUR_API_KEY'`);
	async function copy(text: string) { try { await navigator.clipboard.writeText(text); toast.success('Copied'); } catch { toast.error('Could not copy. Select and copy the text below.'); } }
</script>

<svelte:head><title>Your agent | OpenSlot</title></svelte:head>
<div class="page-heading"><div><p class="eyebrow">YOUR TOOLS. YOUR RULES.</p><h1>Bring your own agent.</h1><p>Use the agent you already trust. OpenSlot takes care of the schedule.</p></div><span class="feature-icon"><Plug size={26}/></span></div>
<section class="agent-connect surface"><div class="section-heading"><div><h2>One connection. Real availability.</h2><p>For any client that supports a remote MCP server.</p></div><span class="soft-badge">MCP + REST API</span></div><label class="field-label" for="mcp-endpoint">MCP server URL</label><div class="copy-field"><Input id="mcp-endpoint" readonly value={endpoint}/><Button variant="outline" onclick={() => copy(endpoint)}><Copy size={16}/> Copy URL</Button></div><p class="field-hint">Add this URL in your agent’s connector settings. Use OAuth where supported, or create an API key below.</p></section>
<div class="capability-grid"><div><Calendar size={20}/><h3>Find a time</h3><p>Read available slots in the right time zone.</p></div><div><Clock size={20}/><h3>Book a meeting</h3><p>Create a booking with the required guest details.</p></div><div><RefreshCw size={20}/><h3>Change plans</h3><p>Move a booking to another available time.</p></div><div><X size={20}/><h3>Cancel cleanly</h3><p>Cancel a booking and run its configured notifications.</p></div></div>
<section class="surface code-panel"><div class="section-heading"><div><h2>Connect it your way.</h2><p>Copy a starting configuration for your client.</p></div><div class="segmented" aria-label="Connection example"><Button variant={example === 'mcp' ? 'default' : 'ghost'} aria-pressed={example === 'mcp'} onclick={() => example = 'mcp'}>MCP</Button><Button variant={example === 'api' ? 'default' : 'ghost'} aria-pressed={example === 'api'} onclick={() => example = 'api'}>REST API</Button></div></div><div class="code-block"><pre><code>{example === 'mcp' ? configuration : request}</code></pre><Button variant="ghost" size="icon" aria-label="Copy connection example" onclick={() => copy(example === 'mcp' ? configuration : request)}><Copy size={17}/></Button></div><p class="field-hint">Replace YOUR_API_KEY in your agent’s secure configuration. Keys inherit your account’s role and can be revoked at any time.</p></section>
<section class="surface key-manager"><APIKeyManager/></section>
<div class="agent-guidance"><Key size={20}/><div><strong>You stay in control.</strong><p>Ask your agent to offer available times and get your confirmation before creating or changing a booking. OpenSlot validates each request through its booking engine.</p></div></div>
<a class="text-link" href={`${base}/connections`}>Manage connected apps <ArrowRight size={16}/></a>
