<script>
	// A scoped theater: /t/<room> is the same page as the front door — the
	// room comes from the URL. The {#key} matters: navigating room A →
	// room B stays on this route with only the param changing, and SvelteKit
	// would reuse the mounted components — leaving room A's player and chat
	// socket running inside room B. Keying on the param remounts everything
	// for the new room.
	import { page } from '$app/state';
	import Page from '../../+page.svelte';
</script>

{#key page.params.room}
	<Page />
{/key}
