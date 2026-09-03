<script>
	import '../app.css';
	import { status, config, role, reachable, clockSkewMs } from '$lib/stores.js';
	import { dedupeSubtitle } from '$lib/text.js';
	import { logout, getRooms } from '$lib/api.js';
	import GlowFrame from '$lib/components/GlowFrame.svelte';
	import NowTray from '$lib/components/NowTray.svelte';
	import ChatPanel from '$lib/components/ChatPanel.svelte';
	import Lobby from '$lib/components/Lobby.svelte';
	import RoomWall from '$lib/components/RoomWall.svelte';

	let chatHidden = $state(false);
	let frame = $state();

	// The rooms list drives the overview: with more than one room the front
	// page becomes the screen wall, and scoped theaters grow a way back to
	// it. Polled so the wall follows rooms being created, deleted, and
	// going live — no refresh.
	const scoped =
		typeof location !== 'undefined' && location.pathname.startsWith('/t/');
	// The room THIS mount serves, read from the URL once — the {#key} in
	// t/[room] guarantees a fresh mount per room, so mount-time is correct,
	// and unlike the status poll it can never lag a navigation.
	const roomId = scoped
		? (location.pathname.match(/^\/t\/([a-z][a-z0-9-]*)/)?.[1] ?? 'main')
		: 'main';
	let roomsList = $state(null);
	$effect(() => {
		let stopped = false;
		let timer;
		async function poll() {
			try {
				const d = await getRooms();
				if (!stopped) roomsList = d.rooms ?? [];
			} catch {}
			if (!stopped) timer = setTimeout(poll, 10000);
		}
		poll();
		return () => {
			stopped = true;
			clearTimeout(timer);
		};
	});
	const multiRoom = $derived((roomsList?.length ?? 0) > 1);
	const showWall = $derived(!scoped && multiRoom);

	const live = $derived($status?.online === true);
	const viewers = $derived($status?.viewerCount ?? 0);
	const roomName = $derived.by(() => {
		if (!scoped) return null;
		const id = location.pathname.match(/^\/t\/([a-z][a-z0-9-]*)/)?.[1];
		return roomsList?.find((r) => r.id === id)?.name ?? null;
	});
	const theaterName = $derived(roomName || $config?.name || 'Main Theater');

	const tabTitle = $derived.by(() => {
		if (showWall) return theaterName + ' — rooms';
		const np = $status?.nowPlaying;
		if (live && np?.title) {
			const sub = dedupeSubtitle(np.title, np.subtitle);
			return sub ? `${np.title} — ${sub}` : np.title;
		}
		return theaterName + (live ? ' · live' : '');
	});
</script>

<svelte:head>
	<title>{tabTitle}</title>
	<link rel="icon" href={live ? '/favicon-live.svg' : '/favicon.svg'} type="image/svg+xml" />
</svelte:head>

<div class="room">
	{#if !$reachable}
		<div class="offline-banner"><span class="pulse"></span> connection lost — retrying…</div>
	{/if}
	<header class="soft">
		<div class="name"><b>●</b> {theaterName}</div>
		{#if $status?.streamTitle && live}
			<div class="mood">{$status.streamTitle}</div>
		{/if}
		<div class="spacer"></div>
		{#if live && !showWall}
			<div class="presence">{viewers} watching</div>
		{/if}
		<div class="iconbar">
			{#if multiRoom && scoped}
				<a class="icon-btn" href="/" title="All rooms" aria-label="All rooms">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
				</a>
			{/if}
			{#if live && !showWall}
				<button
					class="icon-btn"
					class:active={!chatHidden}
					title={chatHidden ? 'Show chat' : 'Hide chat'}
					aria-label="Toggle chat"
					onclick={() => (chatHidden = !chatHidden)}
				>
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
				</button>
			{/if}
			{#if $role === 'admin'}
				<a class="icon-btn" href="/admin/" title="Admin settings" aria-label="Admin settings">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
				</a>
			{/if}
			<button class="icon-btn" title="Log out" aria-label="Log out" onclick={logout}>
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
			</button>
		</div>
	</header>

	{#if !scoped && roomsList === null}
		<div class="loading">·</div>
	{:else if showWall}
		<RoomWall rooms={roomsList} />
	{:else if live}
		<div class="lounge" class:chat-hidden={chatHidden}>
			<div class="screen-zone">
				<GlowFrame bind:this={frame} channelId={roomId} clockSkewMs={$clockSkewMs} paused={$status?.nowPlaying?.paused === true} />
				<NowTray status={$status} clockSkewMs={$clockSkewMs} />
			</div>
			{#if !chatHidden}
				<ChatPanel />
			{/if}
		</div>
	{:else if $status}
		<Lobby status={$status} />
	{:else}
		<div class="loading">·</div>
	{/if}
</div>

<style>
	.room {
		height: 100vh;
		display: flex;
		flex-direction: column;
		padding: 11px 13px;
		gap: 9px;
	}
	header.soft {
		display: flex;
		align-items: center;
		gap: 14px;
		flex: none;
		padding: 0 6px;
	}
	.name {
		font-size: 17px;
		font-weight: 650;
	}
	.name b {
		color: var(--accent);
	}
	.mood {
		font-size: 12.5px;
		color: var(--muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 40vw;
	}
	.spacer {
		flex: 1;
	}
	.presence {
		font-size: 12.5px;
		color: var(--muted);
	}
	.iconbar {
		display: flex;
		gap: 8px;
		margin-left: 6px;
	}
	.icon-btn {
		text-decoration: none;
		width: 32px;
		height: 32px;
		border-radius: 10px;
		cursor: pointer;
		border: 1px solid var(--border);
		background: color-mix(in srgb, var(--surface) 70%, transparent);
		color: var(--muted);
		display: grid;
		place-items: center;
	}
	.icon-btn:hover {
		color: var(--accent);
		border-color: color-mix(in srgb, var(--accent) 50%, var(--border));
	}
	.icon-btn.active {
		color: var(--accent);
	}
	.icon-btn svg {
		width: 16px;
		height: 16px;
	}
	.lounge {
		flex: 1;
		display: flex;
		gap: 10px;
		min-height: 0;
	}
	.screen-zone {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 8px;
		min-width: 0;
	}
	.loading {
		flex: 1;
		display: grid;
		place-items: center;
		color: var(--muted);
	}

	.offline-banner {
		position: fixed;
		top: 14px;
		left: 50%;
		transform: translateX(-50%);
		z-index: 60;
		display: flex;
		align-items: center;
		gap: 8px;
		background: #000000c0;
		border: 1px solid var(--danger);
		color: var(--text);
		font-size: 12.5px;
		padding: 8px 16px;
		border-radius: 99px;
		backdrop-filter: blur(8px);
	}
	.offline-banner .pulse {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--danger);
		animation: bannerpulse 1.2s infinite;
	}
	@keyframes bannerpulse {
		50% {
			opacity: 0.3;
		}
	}

	/* ---------- mobile portrait: stack and allow scrolling ---------- */
	@media (max-width: 860px) and (orientation: portrait) {
		.room {
			padding: 8px;
			gap: 8px;
			height: 100dvh;
			overflow-y: auto;
		}
		.mood {
			display: none;
		}
		.lounge {
			flex-direction: column;
			min-height: 0;
		}
		.screen-zone {
			flex: none;
		}
		.lounge :global(.chat) {
			width: 100%;
			flex: 1;
			min-height: 200px;
		}
		.lounge :global(.frame-wrap) {
			flex: none;
			aspect-ratio: 16/9;
		}
	}

	/* ---------- phone landscape: keep the theater side-by-side ---------- */
	@media (max-height: 520px) {
		.room {
			padding: 6px 8px;
			gap: 6px;
		}
		header.soft {
			padding: 0 4px;
		}
		.name {
			font-size: 14px;
		}
		.mood,
		.presence {
			display: none;
		}
		.lounge {
			gap: 8px;
		}
		.lounge :global(.chat) {
			width: 240px;
		}
		.screen-zone :global(.now-tray) {
			display: none;
		}
	}
</style>
