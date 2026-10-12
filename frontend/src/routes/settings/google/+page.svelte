<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type GoogleSettings } from '$lib/api';
	import { currentUser } from '$lib/stores';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { toast } from 'svelte-sonner';
	import { saveOnCmdS } from '$lib/save-shortcut';
	import { createAsyncFlag } from '$lib/async-action.svelte';

	const loadingFlag = createAsyncFlag(true);
	const savingFlag = createAsyncFlag();

	let googleSettings = $state<GoogleSettings | null>(null);
	let clientID = $state('');
	let clientSecret = $state('');

	// Host the server builds its OAuth redirect URIs from. Prefer the server's
	// configured base_url so the displayed URIs match exactly what we send to
	// Google; fall back to the current origin if it's somehow blank.
	const redirectBase = $derived(
		googleSettings?.base_url || (typeof window !== 'undefined' ? window.location.origin : '')
	);
	const isLocal = $derived(redirectBase.includes('localhost') || redirectBase.includes('127.0.0.1'));

	// Catches the "moved to a custom domain but never updated BASE_URL" trap: the server
	// still computes redirect URIs from its own configured base_url, which can silently
	// drift from whatever domain an admin is actually browsing this page at (e.g. after
	// pointing a custom domain at a host whose BASE_URL secret still says the old default).
	const browserOrigin = $derived(typeof window !== 'undefined' ? window.location.origin : '');
	const originMismatch = $derived(
		!!googleSettings?.base_url && !!browserOrigin &&
		googleSettings.base_url.replace(/\/+$/, '') !== browserOrigin
	);

	onMount(() => loadingFlag.run(async () => {
		googleSettings = await api.get<GoogleSettings>('/v1/settings/google');
		clientID = googleSettings.client_id;
	}, 'Could not load Google settings'));

	async function save() {
		await savingFlag.run(async () => {
			const body: Record<string, unknown> = { client_id: clientID };
			if (clientSecret) body.client_secret = clientSecret;
			googleSettings = await api.patch<GoogleSettings>('/v1/settings/google', body);
			clientSecret = '';
			toast.success(googleSettings.configured ? 'Credentials saved. Choose a connection below.' : 'Google setup cleared.');
		}, 'Could not save Google settings');
	}
</script>

<svelte:window onkeydown={saveOnCmdS(save, () => !savingFlag.active)} />

{#if !$currentUser?.is_admin}
	<p class="text-sm text-muted-foreground">Admin access required.</p>
{:else}

{#if loadingFlag.active}
	<p class="py-8 text-sm text-muted-foreground">Loading…</p>
{:else}
	<div class="max-w-lg space-y-4">
        {#if googleSettings?.configuration_error && (googleSettings.client_id || googleSettings.client_secret_set)}
         <div class="rounded-lg bg-destructive/10 p-4 text-sm text-destructive" role="alert">
          <p class="font-medium">Google setup needs attention</p>
          <p class="mt-1">{googleSettings.configuration_error}</p>
         </div>
        {/if}

		{#if originMismatch}
			<div class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
				<p class="font-medium">This page is being viewed at a different domain than OpenSlot is configured for</p>
				<p class="mt-1 text-amber-800">
					You're browsing <code class="rounded bg-amber-100 px-1 font-mono">{browserOrigin}</code>, but this
					server's <code class="rounded bg-amber-100 px-1 font-mono">BASE_URL</code> is set to
					<code class="rounded bg-amber-100 px-1 font-mono">{googleSettings?.base_url}</code>. The redirect
					URIs below are built from <code class="rounded bg-amber-100 px-1 font-mono">BASE_URL</code> —
					if <code class="rounded bg-amber-100 px-1 font-mono">{browserOrigin}</code> is your real domain
					(for example, after pointing a custom domain at this instance), update the
					<code class="rounded bg-amber-100 px-1 font-mono">BASE_URL</code> environment variable/secret with
					your hosting provider and redeploy, then reload this page.
				</p>
			</div>
		{/if}

		{#if !googleSettings?.configured}
		<div class="rounded-lg border bg-card p-6">
			<h2 class="mb-4 text-sm font-semibold">Setup instructions</h2>
			<ol class="space-y-4 text-sm">
				<li class="flex gap-3">
					<span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold text-muted-foreground">1</span>
					<div>
						Go to <a href="https://console.cloud.google.com" target="_blank" rel="noopener noreferrer" class="font-medium text-primary underline">console.cloud.google.com</a>.
						If you don't have a project, create one · any name works.
					</div>
				</li>
				<li class="flex gap-3">
					<span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold text-muted-foreground">2</span>
					<div>
						Open <span class="font-medium">APIs &amp; Services → Library</span> and enable
                        <span class="font-medium">Google Calendar API</span> and <span class="font-medium">Gmail API</span>.
					</div>
				</li>
				<li class="flex gap-3">
					<span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold text-muted-foreground">3</span>
					<div>
						Go to <span class="font-medium">Google Auth platform → Branding and Audience</span>.
						Choose <span class="font-medium">External</span> (or Internal if you have Google Workspace).
						Fill in the app name and your email, then save.
						<p class="mt-1.5 text-xs text-muted-foreground">
                            In <span class="font-medium">Testing</span>, add the accounts that will connect under
                            <span class="font-medium">Audience → Test users</span>. Public use may require Google's verification of the requested permissions.
                            <a href="https://developers.google.com/identity/protocols/oauth2/web-server" target="_blank" rel="noopener noreferrer" class="text-primary underline">Google setup guide</a>.
						</p>
					</div>
				</li>
				<li class="flex gap-3">
					<span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold text-muted-foreground">4</span>
					<div>
						Go to <span class="font-medium">Google Auth platform → Clients → Create client</span>.
						Set application type to <span class="font-medium">Web application</span>.
					</div>
				</li>
				<li class="flex gap-3">
					<span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold text-muted-foreground">5</span>
					<div>
						Under <span class="font-medium">Authorised redirect URIs</span>, add all three:
						<code class="mt-1 block rounded bg-muted px-2 py-1 text-xs font-mono break-all">{redirectBase}/v1/calendar/callback</code>
 <code class="mt-1 block rounded bg-muted px-2 py-1 text-xs font-mono break-all">{redirectBase}/v1/apps/gmail/callback</code>
 <p class="mt-2 text-xs text-muted-foreground">For Gmail sending, also enable the Gmail API and register the Gmail callback above.</p>
						<code class="mt-1 block rounded bg-muted px-2 py-1 text-xs font-mono break-all">{redirectBase}/v1/auth/callback</code>
						Click <span class="font-medium">Create</span>. Copy the Client ID and Client Secret shown.
						{#if !isLocal}
							<p class="mt-1.5 text-xs text-muted-foreground">
								If you also run OpenSlot locally, add the
								<code class="rounded bg-muted px-1">http://127.0.0.1:8788/…</code> variants of all three URIs too.
							</p>
						{/if}
					</div>
				</li>
				<li class="flex gap-3">
					<span class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold text-muted-foreground">6</span>
					<div>Paste them into the form below and save.</div>
				</li>
			</ol>
		</div>
		{/if}

		<div class="rounded-lg border bg-card p-6">
			<div class="mb-4 flex items-start justify-between gap-2">
				<div>
					<h2 class="text-sm font-semibold">Google OAuth</h2>
					<p class="mt-0.5 text-xs text-muted-foreground">Connect Google Calendar, Gmail, and Google sign-in.</p>
				</div>
				{#if googleSettings !== null}
					<span class="flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium {googleSettings.configured ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}">
						<span class="h-1.5 w-1.5 rounded-full {googleSettings.configured ? 'bg-primary' : 'bg-muted-foreground'}"></span>
						{googleSettings.configured ? 'Credentials saved' : 'Setup required'}
					</span>
				{/if}
			</div>

			<div class="space-y-3">
				<div class="space-y-1.5">
					<Label for="g-client-id">Client ID</Label>
					<Input id="g-client-id" type="text" autocomplete="off" spellcheck="false" data-sensitive placeholder="123456789-abc.apps.googleusercontent.com" bind:value={clientID} />
				</div>
				<div class="space-y-1.5">
					<Label for="g-client-secret">Client Secret</Label>
					<Input id="g-client-secret" type="password" autocomplete="off" data-sensitive
						placeholder={googleSettings?.client_secret_set ? '•••••••• (stored)' : 'Enter client secret'}
						bind:value={clientSecret} />
					{#if googleSettings?.client_secret_set && !clientSecret}
						<p class="text-xs text-muted-foreground">Stored. Leave blank to keep it for the same Client ID. A new Client ID needs its matching secret.</p>
					{/if}
				</div>
			</div>

			{#if googleSettings?.configured}
				<div class="mt-5 border-t pt-4">
					<p class="text-sm text-muted-foreground">Google checks these credentials when you connect. Choose an account and approve the requested access.</p>
                    <div class="my-4 flex flex-wrap gap-2">
                     <a class={buttonVariants()} href="/v1/calendar/connect?provider=google" data-sveltekit-reload>Connect Google Calendar</a>
                     <a class={buttonVariants({variant:'outline'})} href="/v1/apps/gmail/connect" data-sveltekit-reload>Connect Gmail</a>
                    </div>
                    <p class="text-xs font-medium text-muted-foreground">Authorised redirect URIs</p>
					<p class="mt-0.5 text-xs text-muted-foreground">
						These must be registered on your OAuth client in Google Cloud (Credentials → your client → Authorised redirect URIs).
					</p>
					<code class="mt-2 block rounded bg-muted px-2 py-1 text-xs font-mono break-all">{redirectBase}/v1/calendar/callback</code>
 <code class="mt-1 block rounded bg-muted px-2 py-1 text-xs font-mono break-all">{redirectBase}/v1/apps/gmail/callback</code>
 <p class="mt-2 text-xs text-muted-foreground">For Gmail sending, also enable the Gmail API and register the Gmail callback above.</p>
					<code class="mt-1 block rounded bg-muted px-2 py-1 text-xs font-mono break-all">{redirectBase}/v1/auth/callback</code>
					{#if !isLocal}
						<p class="mt-1.5 text-xs text-muted-foreground">
							If you also run OpenSlot locally, add the
							<code class="rounded bg-muted px-1">http://127.0.0.1:8788/…</code> variants too.
						</p>
					{/if}
				</div>
			{/if}

			<div class="mt-5">
				<Button onclick={save} disabled={savingFlag.active}>
					{savingFlag.active ? 'Saving…' : 'Save'}
				</Button>
			</div>
		</div>
	</div>
{/if}

{/if}
