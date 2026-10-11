<script lang="ts">
	import { onMount } from 'svelte';
	import { base } from '$app/paths';
	import { api, type EventType } from '$lib/api';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import { Copy, ExternalLink, Link, Code, MousePointer2, Plus } from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import * as Select from '$lib/components/ui/select';
	let events = $state<EventType[]>([]);
	let slug = $state('');
	let origin = $state('');
	let loading = $state(true);
	let widgetReady = $state(false);
	let error = $state('');
	let mode = $state<'link' | 'inline' | 'popup'>('inline');
	const bookingURL = $derived(`${origin}/book/${encodeURIComponent(slug)}`);
	const snippet = $derived(mode === 'link' ? bookingURL : mode === 'inline'
		? `<script src="${origin}/embed.js" async><\/script>\n<openslot-booking slug="${slug}"></openslot-booking>`
		: `<script src="${origin}/embed.js" async><\/script>\n<button data-openslot-popup="${slug}">Find a time</button>`);
	onMount(() => {
		let cancelled = false;
		origin = window.location.origin;
		async function load() {
			try {
				const result = await api.get<{items:EventType[]}>('/v1/event-types');
				if (cancelled) return;
				events = (result.items ?? []).filter(event => event.is_active && !event.archived);
				slug = events[0]?.slug ?? '';
				if (!customElements.get('openslot-booking')) {
					await new Promise<void>((resolve, reject) => {
						const script = document.createElement('script');
						script.src = `${origin}/embed.js`; script.async = true;
						script.onload = () => resolve(); script.onerror = () => { script.remove(); reject(new Error('The embed preview could not load. Reload to try again.')); };
						document.head.appendChild(script);
					});
				}
				if (!cancelled) widgetReady = true;
			} catch(e) { if (!cancelled) error = e instanceof Error ? e.message : 'Could not load your meeting types.'; }
			finally { if (!cancelled) loading = false; }
		}
		void load();
		return () => { cancelled = true; };
	});
	async function copy() { try { await navigator.clipboard.writeText(snippet); toast.success(mode === 'link' ? 'Booking link copied' : 'Embed code copied'); } catch { toast.error('Could not copy. Select and copy the code below.'); } }
</script>

<svelte:head><title>Share & embed | OpenSlot</title></svelte:head>
<div class="page-heading"><div><p class="eyebrow">MEET PEOPLE WHERE THEY ARE</p><h1>A good time, anywhere.</h1><p>Share a link or put your booking calendar right on your website.</p></div><span class="feature-icon warm"><Code size={25}/></span></div>
{#if error}<div class="error-panel" role="alert">{error}</div>{/if}
{#if loading}<div class="surface loading-panel" role="status">Loading your meeting types…</div>{:else if events.length === 0 && !error}<div class="surface empty-schedule"><Link size={30}/><h2>Create a meeting type first.</h2><p>Then you can share it anywhere you like.</p><a class={buttonVariants()} href={`${base}/event-types`}><Plus size={17}/> Create meeting type</a></div>{:else if !error}
<section class="surface share-controls"><div><label for="share-event" class="field-label">Which meeting?</label><Select.Root type="single" bind:value={slug}><Select.Trigger id="share-event" class="w-full">{events.find(event => event.slug === slug)?.name ?? 'Choose a meeting'}</Select.Trigger><Select.Content>{#each events as event}<Select.Item value={event.slug} label={event.name}>{event.name} · {event.duration_minutes} min</Select.Item>{/each}</Select.Content></Select.Root></div><a class={buttonVariants({variant:'outline'})} href={bookingURL} target="_blank" rel="noopener noreferrer">Open booking page <ExternalLink size={16}/></a></section>
<div class="share-layout"><section class="surface embed-options"><h2>How would you like to share?</h2><div class="embed-mode-list">{#each [{id:'link' as const,title:'A simple link',description:'For messages, bios, and email signatures.',icon:Link},{id:'inline' as const,title:'An inline calendar',description:'A booking flow that lives inside your page.',icon:Code},{id:'popup' as const,title:'A booking button',description:'Open your calendar when someone clicks.',icon:MousePointer2}] as option}<Button variant="ghost" class={`embed-option ${mode === option.id ? 'selected' : ''}`} aria-pressed={mode === option.id} onclick={() => mode = option.id}><option.icon size={20}/><span><strong>{option.title}</strong><small>{option.description}</small></span><span class="radio-dot"></span></Button>{/each}</div><div class="snippet-header"><span class="field-label">{mode === 'link' ? 'Your booking link' : 'Paste this into your website'}</span><Button variant="outline" size="sm" onclick={copy}><Copy size={14}/> Copy</Button></div><pre class="embed-code"><code>{snippet}</code></pre><p class="field-hint">Guests can book without an account. The widget keeps your site’s styles separate from the booking flow.</p></section>
<section class="embed-preview" aria-label="Live booking preview"><div class="preview-toolbar"><span class="browser-dots"><i></i><i></i><i></i></span><span>YOUR WEBSITE</span><span class="soft-badge">Live preview</span></div><div class="preview-content">{#if mode === 'link'}<div class="preview-message"><span class="account-avatar">You</span><div><p>Let’s find a time that works.</p><a href={bookingURL} target="_blank" rel="noopener noreferrer">{bookingURL}</a></div></div>{:else if mode === 'popup'}<div class="popup-example"><p class="eyebrow">LET’S TALK</p><h2>A conversation starts here.</h2><p>Make it easy for someone to take the next step.</p>{#key slug}<Button class="preview-book-button" data-openslot-popup={slug}>Find a time <span>↗</span></Button>{/key}</div>{:else if widgetReady}{#key slug}<openslot-booking {slug}></openslot-booking>{/key}{/if}</div><div class="preview-note">This preview uses your real booking flow. Submitting it creates a booking.</div></section></div>
{/if}
