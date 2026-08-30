<script>
	import '../app.css';
	import { status, config, role } from '$lib/stores.js';
	import { logout } from '$lib/api.js';
	import GlowFrame from '$lib/components/GlowFrame.svelte';
	import NowTray from '$lib/components/NowTray.svelte';
	import ChatPanel from '$lib/components/ChatPanel.svelte';
	import Lobby from '$lib/components/Lobby.svelte';

	let chatHidden = $state(false);
	let frame = $state();

	const live = $derived($status?.online === true);
	const viewers = $derived($status?.viewerCount ?? 0);
	const theaterName = $derived($config?.name || 'Main Theater');
</script>

<svelte:head>
	<title>{theaterName}{live ? ' · live' : ''}</title>
</svelte:head>

<div class="room">
	<header class="soft">
		<div class="name"><b>●</b> {theaterName}</div>
		{#if $status?.streamTitle && live}
			<div class="mood">{$status.streamTitle}</div>
		{/if}
		<div class="spacer"></div>
		{#if live}
			<div class="presence">{viewers} watching</div>
		{/if}
		<div class="iconbar">
			{#if $role === 'admin'}
				<a class="icon-btn" href="/admin/" target="_blank" rel="noopener" title="Admin settings" aria-label="Admin settings">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
				</a>
			{/if}
			<button class="icon-btn" title="Log out" aria-label="Log out" onclick={logout}>
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
			</button>
			{#if live}
				<button
					class="icon-btn"
					class:active={!chatHidden}
					title={chatHidden ? 'Show chat' : 'Hide chat'}
					aria-label="Toggle chat"
					onclick={() => (chatHidden = !chatHidden)}
				>
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
				</button>
				<button class="icon-btn" title="Fullscreen" aria-label="Fullscreen" onclick={() => frame?.goFullscreen()}>
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"/></svg>
				</button>
			{/if}
		</div>
	</header>

	{#if live}
		<div class="lounge" class:chat-hidden={chatHidden}>
			<div class="screen-zone">
				<GlowFrame bind:this={frame} channelId={$status?.channelId || 'main'} />
				<NowTray status={$status} />
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
		padding: 22px 26px;
		gap: 18px;
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
		gap: 20px;
		min-height: 0;
	}
	.screen-zone {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 16px;
		min-width: 0;
	}
	.loading {
		flex: 1;
		display: grid;
		place-items: center;
		color: var(--muted);
	}
</style>
