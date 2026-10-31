<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	const base = resolve('/').replace(/\/$/, '');
	import { onMount } from 'svelte';

	onMount(() => {
		goto(`${base}/settings/profile`, { replaceState: true });
	});
</script>
