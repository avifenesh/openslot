<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	const base = resolve('/').replace(/\/$/, '');
	import type { Snippet } from 'svelte';
	import { Calendar, Clock, Link, Plug, Settings, Sun, Moon, Menu, X, LogOut, Users, Webhook, LayoutDashboard, Video, Mail, Bot } from '@lucide/svelte';
	import '../app.css';
	import { api, type User } from '$lib/api';
	import { currentUser, authStatus, type AuthStatus } from '$lib/stores';
	import { prefs, prefsFromUser } from '$lib/prefs';
	import { Toaster } from '$lib/components/ui/sonner';
	import { Button } from '$lib/components/ui/button';
	import DemoBanner from '$lib/components/demo-banner.svelte';
	let { children }: { children: Snippet } = $props();
	let checking = $state(true);
	let menuOpen = $state(false);
	let dark = $state(false);
	let moreOpen = $state(false);
	let mobile = $state(false);
	let recordingsConfigured = $state(false);
	const isPublicRoute = $derived(['/login', '/claim', '/forgot-password', '/reset-password', '/invite/[token]'].includes(page.route.id ?? ''));
	const navigation = [
		{ path: '/', label: 'Overview', icon: LayoutDashboard },
		{ path: '/bookings', label: 'Bookings', icon: Calendar },
		{ path: '/event-types', label: 'Meeting types', icon: Link },
		{ path: '/availability', label: 'Availability', icon: Clock },
	];
	const connections = [
		{ path: '/inbox', label: 'Inbox scheduling', icon: Mail },
		{ path: '/apps', label: 'App connections', icon: Plug },
		{ path: '/agent', label: 'Your agent', icon: Bot },
		{ path: '/share', label: 'Share & embed', icon: Link },
	];
	function active(path: string) {
		return path === '/' ? page.url.pathname === base || page.url.pathname === `${base}/` : page.url.pathname.startsWith(`${base}${path}`);
	}
	function setTheme(value: boolean) {
		dark = value;
		document.documentElement.classList.toggle('dark', value);
		try { localStorage.setItem('openslot-theme', value ? 'dark' : 'light'); } catch { /* Theme still applies for this visit. */ }
	}
	onMount(() => {
		const query = matchMedia('(max-width: 767px)');
		const update = () => { mobile = query.matches; if (!mobile) menuOpen = false; };
		update();
		query.addEventListener('change', update);
		return () => query.removeEventListener('change', update);
	});
	async function toggleMenu() {
		menuOpen = !menuOpen;
		await tick();
		if (menuOpen) document.querySelector<HTMLAnchorElement>('#workspace-navigation nav a')?.focus();
		else document.getElementById('navigation-toggle')?.focus();
	}
	function closeMenu() {
		if (!menuOpen) return;
		menuOpen = false;
		document.getElementById('navigation-toggle')?.focus();
	}
	function navigationKey(event: KeyboardEvent) {
		if (!mobile || !menuOpen) return;
		if (event.key === 'Escape') { event.preventDefault(); closeMenu(); }
		if (event.key !== 'Tab') return;
		const controls = [document.getElementById('navigation-toggle'), ...document.querySelectorAll<HTMLElement>('#workspace-navigation a, #workspace-navigation button')].filter((el): el is HTMLElement => !!el && el.getClientRects().length > 0);
		const index = controls.indexOf(document.activeElement as HTMLElement);
		if (event.shiftKey && index <= 0) { event.preventDefault(); controls.at(-1)?.focus(); }
		else if (!event.shiftKey && (index === -1 || index === controls.length - 1)) { event.preventDefault(); controls[0]?.focus(); }
	}
	onMount(async () => {
		let preference: string | null = null;
		try { preference = localStorage.getItem('openslot-theme'); } catch { /* Private browsing may disable storage. */ }
		setTheme(preference ? preference === 'dark' : matchMedia('(prefers-color-scheme: dark)').matches);
		if (isPublicRoute) { checking = false; return; }
		try {
			const me = await api.get<User>('/v1/users/me');
			currentUser.set(me);
			prefs.set(prefsFromUser(me));
			authStatus.set(await api.get<AuthStatus>('/v1/auth/status'));
			if (me.is_admin) {
				try { recordingsConfigured = (await api.get<{configured: boolean}>('/v1/settings/livekit')).configured; }
				catch { /* Optional recording configuration must not block scheduling. */ }
			}
		} catch { window.location.href = `${base}/login`; return; }
		checking = false;
	});
	async function logout() {
		await api.post('/v1/auth/logout');
		window.location.href = `${base}/login`;
	}
</script>

<svelte:window onkeydown={navigationKey} />
<Toaster position="top-center" mobileOffset={{ top: 80, left: 16, right: 16 }} closeButton />
{#snippet navGroup(items: typeof navigation)}
	{#each items as item}
		<a href={`${base}${item.path}`} class:current={active(item.path)} aria-current={active(item.path) ? 'page' : undefined} onclick={() => menuOpen = false}>
			<item.icon size={18} strokeWidth={1.7} /><span>{item.label}</span>
		</a>
	{/each}
{/snippet}

{#if isPublicRoute}
	{@render children()}
{:else if checking}
	<div class="loading-screen"><span class="brand-mark" aria-hidden="true"><span></span><span></span><span></span><span></span></span><p>Opening your workspace…</p></div>
{:else}
	<div class="app-shell">
		{#if $authStatus.demo_mode}<div class="demo-strip"><DemoBanner /></div>{/if}
		<header class="mobile-bar">
			<a class="brand" href={`${base}/`}>OpenSlot<span class="brand-dot">.</span></a>
			<Button id="navigation-toggle" variant="ghost" size="icon" aria-label={menuOpen ? 'Close navigation' : 'Open navigation'} aria-expanded={menuOpen} aria-controls="workspace-navigation" onclick={toggleMenu}>
				{#if menuOpen}<X size={20} />{:else}<Menu size={20} />{/if}
			</Button>
		</header>
		{#if menuOpen}<button class="nav-scrim" tabindex="-1" aria-label="Close navigation" onclick={closeMenu}></button>{/if}
		<aside class:mobile-open={menuOpen} class="app-sidebar" id="workspace-navigation" inert={mobile && !menuOpen}>
			<a class="brand brand-desktop" href={`${base}/`} aria-label="OpenSlot overview"><span class="brand-mark" aria-hidden="true"><span></span><span></span><span></span><span></span></span>OpenSlot<span class="brand-dot">.</span></a>
			<div class="workspace-label">YOUR WORKSPACE</div>
			<nav class="app-nav" aria-label="Main navigation">
				{@render navGroup(navigation)}
				<div class="nav-label">CONNECT & SHARE</div>
				{@render navGroup(connections)}
				<div class="nav-divider"></div>
				<a href={`${base}/settings`} class:current={active('/settings')} onclick={() => menuOpen = false}><Settings size={18} strokeWidth={1.7}/><span>Settings</span></a>
				<Button variant="ghost" class="more-toggle" aria-expanded={moreOpen} onclick={() => moreOpen = !moreOpen}>More tools <span aria-hidden="true">{moreOpen ? '−' : '+'}</span></Button>
				{#if moreOpen}
					{#if $currentUser?.is_admin}<a href={`${base}/members`} onclick={() => menuOpen = false}><Users size={18}/><span>Members</span></a><a href={`${base}/teams`} onclick={() => menuOpen = false}><Users size={18}/><span>Teams</span></a>{/if}
					<a href={`${base}/webhooks`} onclick={() => menuOpen = false}><Webhook size={18}/><span>Webhooks</span></a>
					<a href={`${base}/connections`} onclick={() => menuOpen = false}><Plug size={18}/><span>Agent authorizations</span></a>
					{#if $currentUser?.is_admin && recordingsConfigured}<a href={`${base}/recordings`} onclick={() => menuOpen = false}><Video size={18}/><span>Recordings</span></a>{/if}
				{/if}
			</nav>
			<div class="sidebar-note"><span class="status-dot"></span>Free & open source<p>Your calendar. Your tools. Your rules.</p></div>
			<div class="sidebar-account">
				<a href={`${base}/settings/profile`} class="account-link"><span class="account-avatar">{($currentUser?.name ?? 'U').slice(0, 1).toUpperCase()}</span><span><strong>{$currentUser?.name}</strong><small>{$currentUser?.timezone}</small></span></a>
				<Button variant="ghost" size="icon" aria-label={dark ? 'Use light theme' : 'Use dark theme'} onclick={() => setTheme(!dark)}>{#if dark}<Sun size={17}/>{:else}<Moon size={17}/>{/if}</Button>
				<Button variant="ghost" size="icon" aria-label="Sign out" onclick={logout}><LogOut size={17}/></Button>
			</div>
		</aside>
		<main class="app-main" id="main-content" inert={mobile && menuOpen}><div class="page-container">{@render children()}</div></main>
	</div>
{/if}
