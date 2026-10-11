<script lang="ts">
	import { onMount } from 'svelte';
	import { base } from '$app/paths';
	import { ArrowRight, Calendar, Check, Clock, Copy, ExternalLink, Link, Plus, Plug, Code } from '@lucide/svelte';
	import { api, type CalendarStatus, type AvailabilityRule, type EventType, type Booking } from '$lib/api';
	import { currentUser, authStatus } from '$lib/stores';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import { toast } from 'svelte-sonner';
	let loading = $state(true);
	let error = $state('');
	let origin = $state('');
	let calendar = $state<CalendarStatus | null>(null);
	let events = $state<EventType[]>([]);
	let meetings = $state<Booking[]>([]);
	let upcoming = $state(0);
	let rules = $state<AvailabilityRule[]>([]);
	const activeEvents = $derived(events.filter(event => event.is_active && !event.archived));
	const steps = $derived([
		{ label: 'Connect your calendar', detail: 'Keep your busy times in sync.', done: !!calendar?.connected, href: `${base}/calendar`, icon: Calendar },
		{ label: 'Choose your hours', detail: 'Make space for the rest of your day.', done: rules.length > 0, href: `${base}/availability`, icon: Clock },
		{ label: 'Create a meeting link', detail: 'One link. No back-and-forth.', done: activeEvents.length > 0, href: `${base}/event-types`, icon: Link },
	]);
	const completed = $derived(steps.filter(step => step.done).length);
	onMount(load);
	async function load() {
		origin = window.location.origin;
		error = ''; loading = true;
		try {
			const [cal, availability, types, bookings] = await Promise.all([
				api.get<CalendarStatus>('/v1/calendar/status'),
				api.get<{ items: AvailabilityRule[] }>('/v1/availability-rules'),
				api.get<{ items: EventType[] }>('/v1/event-types'),
				api.get<{ items: Booking[]; counts: {upcoming: number} }>('/v1/bookings?when=upcoming&status=confirmed&order=asc&limit=4'),
			]);
			calendar = cal; rules = availability.items ?? []; events = types.items ?? [];
			meetings = bookings.items ?? []; upcoming = bookings.counts?.upcoming ?? meetings.length;
		} catch (e) { error = e instanceof Error ? e.message : 'Could not load your workspace.'; }
		finally { loading = false; }
	}
	async function copy(slug: string) {
		try { await navigator.clipboard.writeText(`${origin}/book/${slug}`); toast.success('Booking link copied'); }
		catch { toast.error('Could not copy. Open the booking page to copy its address.'); }
	}
	function date(iso: string, options: Intl.DateTimeFormatOptions) {
		return new Date(iso).toLocaleString(undefined, { ...options, timeZone: $currentUser?.timezone || 'UTC' });
	}
</script>

<svelte:head><title>Overview | OpenSlot</title></svelte:head>
<div class="page-heading"><div><p class="eyebrow">A LITTLE LESS BACK-AND-FORTH</p><h1>Your time, well spent.</h1><p>Welcome back{$currentUser?.name ? `, ${$currentUser.name.split(' ')[0]}` : ''}. Here’s what’s coming up.</p></div><a class={buttonVariants()} href={`${base}/event-types`}><Plus size={17}/> New meeting type</a></div>
{#if error}<div class="error-panel" role="alert"><p>{error}</p><Button variant="outline" onclick={load}>Try again</Button></div>{/if}
{#if loading}<div class="surface loading-panel" role="status">Loading your schedule…</div>{:else if !error}
	{#if completed < 3}
	<section class="setup-panel" aria-labelledby="setup-heading"><div class="setup-title"><div><span class="eyebrow">MAKE IT YOURS</span><h2 id="setup-heading">Your booking page starts here.</h2><p>{completed} of 3 steps complete{$authStatus.demo_mode ? ' · Sample workspace' : ''}</p></div><span class="setup-progress" aria-label={`${completed} of 3 setup steps complete`}>{completed}<small>/ 3</small></span></div>
		<div class="setup-steps">{#each steps as step, i}<a href={step.href} class:done={step.done}><span class="step-number">{#if step.done}<Check size={17}/>{:else}{i + 1}{/if}</span><div><strong>{step.label}</strong><p>{step.detail}</p></div><ArrowRight size={17}/></a>{/each}</div>
		{#if !calendar?.connected}<p class="setup-footnote">Your booking rules work now. Connect a calendar to also check meetings booked elsewhere.</p>{/if}
	</section>
	{/if}
	<div class="overview-grid">
		<section class="surface meetings-panel"><div class="section-heading"><div><span class="eyebrow">ON THE HORIZON</span><h2>Upcoming meetings <span class="count-pill">{upcoming}</span></h2></div><a href={`${base}/bookings`} class="text-link">View all <ArrowRight size={15}/></a></div>
			{#if meetings.length === 0}<div class="empty-schedule"><span class="empty-icon"><Calendar size={28} strokeWidth={1.5}/></span><h3>A little breathing room.</h3><p>Share a meeting link and your next booking will appear here.</p><a class={buttonVariants({ variant: 'outline' })} href={`${base}/share`}>Share your availability <ArrowRight size={16}/></a></div>
			{:else}<div class="meeting-list">{#each meetings as meeting}<a class="meeting-row" href={`${base}/bookings`}><span class="date-tile"><small>{date(meeting.start_at, {month:'short'})}</small><strong>{date(meeting.start_at, {day:'numeric'})}</strong></span><div class="meeting-description"><strong>{events.find(e => e.slug === meeting.event_type_slug)?.name ?? meeting.event_type_slug}</strong><p>{meeting.attendees?.map(a => a.name).join(', ') || 'Booked guest'}</p></div><div class="meeting-time"><strong>{date(meeting.start_at, {hour:'numeric',minute:'2-digit',hour12:$currentUser?.time_format !== '24h'})}</strong><small>{date(meeting.start_at, {weekday:'long'})}</small></div></a>{/each}</div><div class="panel-footer"><Clock size={14}/> Times shown in {$currentUser?.timezone ?? 'UTC'}</div>{/if}
		</section>
		<section class="surface links-panel"><div class="section-heading"><div><span class="eyebrow">READY TO SHARE</span><h2>Your meeting links</h2></div><Link size={19}/></div>
			{#if activeEvents.length === 0}<div class="empty-links"><p>Start with the kind of meeting you have most often.</p><a class={buttonVariants({variant:'outline'})} href={`${base}/event-types`}><Plus size={16}/> Create a meeting type</a></div>{:else}<div class="event-link-list">{#each activeEvents.slice(0, 4) as event, i}<div class="event-link"><span class="event-marker" style={`--marker:var(--event-${i % 3})`}></span><div><a href={`${base}/event-types/${event.slug}`}><strong>{event.name}</strong></a><p>{event.duration_minutes} min · {event.is_public ? 'Public link' : 'Private link'}</p></div><Button variant="ghost" size="icon" aria-label={`Copy ${event.name} link`} onclick={() => copy(event.slug)}><Copy size={16}/></Button><a class={buttonVariants({variant:'ghost',size:'icon'})} href={`${origin}/book/${event.slug}`} target="_blank" rel="noopener noreferrer" aria-label={`Preview ${event.name}`}><ExternalLink size={16}/></a></div>{/each}</div>{/if}
			<a href={`${base}/event-types`} class="panel-footer text-link">Manage meeting types <ArrowRight size={15}/></a>
		</section>
	</div>
	<section class="connection-grid" aria-label="Make OpenSlot work for you"><a class="connection-card" href={`${base}/agent`}><span class="feature-icon"><Plug size={23}/></span><div><span class="eyebrow">BRING YOUR OWN</span><h3>Let your agent handle it.</h3><p>Connect the agent you already use to your real availability and bookings.</p><span class="text-link">Connect an agent <ArrowRight size={16}/></span></div></a><a class="connection-card warm" href={`${base}/share`}><span class="feature-icon"><Code size={23}/></span><div><span class="eyebrow">RIGHT WHERE YOU NEED IT</span><h3>Your calendar, on your site.</h3><p>Drop a booking calendar or a simple button into any website.</p><span class="text-link">Get your embed <ArrowRight size={16}/></span></div></a></section>
	<p class="quiet-footer"><span class="status-dot"></span>Free software. No feature tiers. Made to fit your day.</p>
{/if}
