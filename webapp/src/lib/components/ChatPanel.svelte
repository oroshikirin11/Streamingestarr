<script>
	import { onMount, onDestroy, tick } from 'svelte';
	import {
		messages,
		me,
		connected,
		connectChat,
		disconnectChat,
		sendChatMessage,
		requestNameChange
	} from '../chat.js';

	let draft = $state('');
	let renaming = $state(false);
	let nameDraft = $state('');
	let scroller;

	onMount(connectChat);
	onDestroy(disconnectChat);

	// stick to the bottom as messages arrive
	$effect(() => {
		$messages;
		tick().then(() => {
			if (scroller) scroller.scrollTop = scroller.scrollHeight;
		});
	});

	function send(e) {
		e.preventDefault();
		const body = draft.trim();
		if (!body) return;
		sendChatMessage(body);
		draft = '';
	}

	function startRename() {
		nameDraft = $me?.displayName ?? '';
		renaming = true;
	}

	function submitRename(e) {
		e.preventDefault();
		const name = nameDraft.trim();
		if (name && name !== $me?.displayName) requestNameChange(name);
		renaming = false;
	}

	const initials = (name) =>
		(name ?? '??')
			.split(/\s+/)
			.map((w) => w[0])
			.join('')
			.slice(0, 2)
			.toUpperCase();

	// map the server's displayColor onto our four warm avatar tints
	const tint = (user) => `f${((user?.displayColor ?? 0) % 4) + 1}`;
	const isRecent = (i, list) => i >= list.length - 12;
</script>

<aside class="chat">
	<div class="msgs" bind:this={scroller}>
		{#each $messages as m, i (m.id)}
			{#if m.type === 'CHAT'}
				<div class="m" class:fade={!isRecent(i, $messages)}>
					<div class="face {tint(m.user)}">{initials(m.user?.displayName)}</div>
					<div class="body">
						<div class="who">{m.user?.displayName}</div>
						<!-- chat bodies arrive as server-rendered, server-sanitized HTML
						     (markdown + emoji), same contract the old web app relied on -->
						{@html m.body}
					</div>
				</div>
			{:else if m.type === 'NAME_CHANGE'}
				<div class="m sys">· {m.oldName} is now {m.user?.displayName} ·</div>
			{:else}
				<div class="m sys">· {@html m.body} ·</div>
			{/if}
		{/each}
	</div>
	<div class="compose">
		{#if renaming}
			<form onsubmit={submitRename}>
				<input
					bind:value={nameDraft}
					maxlength="30"
					placeholder="Pick a name…"
					onblur={() => (renaming = false)}
				/>
			</form>
		{:else}
			<div class="me-row">
				{#if $me}
					<button class="me" onclick={startRename} title="Change name">
						you are <b>{$me.displayName}</b>
					</button>
				{/if}
				{#if !$connected}<span class="conn">reconnecting…</span>{/if}
			</div>
			<form onsubmit={send}>
				<input bind:value={draft} maxlength="500" placeholder="Say something soft…" />
			</form>
		{/if}
	</div>
</aside>

<style>
	.chat {
		width: 320px;
		flex: none;
		display: flex;
		flex-direction: column;
		min-height: 0;
		background: color-mix(in srgb, var(--surface) 72%, transparent);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		backdrop-filter: blur(10px);
		overflow: hidden;
	}
	.msgs {
		flex: 1;
		overflow-y: auto;
		padding: 20px 18px 10px;
		display: flex;
		flex-direction: column;
		gap: 13px;
		scrollbar-width: thin;
		scrollbar-color: var(--border) transparent;
	}
	.m {
		display: flex;
		gap: 10px;
		font-size: 13.5px;
		line-height: 1.5;
		transition: opacity 0.6s;
	}
	.m.fade {
		opacity: 0.45;
	}
	.face {
		flex: none;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-size: 10px;
		font-weight: 700;
		margin-top: 1px;
	}
	.face.f1 { background: #4a3038; color: #e8b3a0; }
	.face.f2 { background: #30404a; color: #a0cbe8; }
	.face.f3 { background: #37452f; color: #b3d9a0; }
	.face.f4 { background: #4a4430; color: #e8d5a0; }
	.body {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.who {
		font-size: 11.5px;
		font-weight: 700;
		color: var(--accent);
		margin-bottom: 1px;
	}
	.m.sys {
		justify-content: center;
		font-size: 11.5px;
		color: color-mix(in srgb, var(--accent) 65%, var(--muted));
		text-align: center;
	}
	.compose {
		flex: none;
		padding: 8px 14px 16px;
	}
	.me-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0 6px 6px;
	}
	.me {
		background: none;
		border: 0;
		color: var(--muted);
		font-size: 11px;
		cursor: pointer;
		padding: 2px 4px;
	}
	.me b {
		color: var(--accent);
		font-weight: 650;
	}
	.me:hover b {
		text-decoration: underline;
	}
	.conn {
		font-size: 10.5px;
		color: var(--muted);
	}
	.compose input {
		width: 100%;
		background: var(--surface-2);
		border: 1px solid var(--border);
		color: var(--text);
		border-radius: 99px;
		padding: 11px 18px;
		font-size: 13px;
	}
	.compose input:focus {
		outline: 0;
		border-color: color-mix(in srgb, var(--accent) 60%, var(--border));
	}
</style>
