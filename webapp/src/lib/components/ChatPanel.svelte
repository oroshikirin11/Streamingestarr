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

	// autofocus + select when the rename field appears
	function focusSelect(node) {
		node.focus();
		node.select();
	}

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
		e?.preventDefault();
		const name = nameDraft.trim();
		if (name && name !== $me?.displayName) requestNameChange(name);
		renaming = false;
	}

	function renameKeys(e) {
		if (e.key === 'Escape') renaming = false;
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
		<div class="me-row">
			{#if $me}
				<span class="face {tint($me)} small">{initials($me.displayName)}</span>
				{#if renaming}
					<form class="rename-form" onsubmit={submitRename}>
						<input
							class="rename-input"
							bind:value={nameDraft}
							maxlength="30"
							placeholder="Pick a new name…"
							use:focusSelect
							onkeydown={renameKeys}
						/>
						<button type="submit" class="chip ok" title="Save name" aria-label="Save name">
							<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
						</button>
						<button type="button" class="chip" title="Cancel" aria-label="Cancel" onclick={() => (renaming = false)}>
							<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
						</button>
					</form>
				{:else}
					<button class="me" onclick={startRename} title="Change your name">
						<b>{$me.displayName}</b>
						<svg class="pencil" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5z"/></svg>
					</button>
				{/if}
			{/if}
			<span class="dot" class:on={$connected} title={$connected ? 'connected' : 'reconnecting…'}></span>
		</div>
		<form onsubmit={send}>
			<input bind:value={draft} maxlength="500" placeholder="Say something soft…" disabled={renaming} />
		</form>
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
		gap: 8px;
		padding: 0 4px 8px;
		min-height: 30px;
	}
	.face.small {
		width: 22px;
		height: 22px;
		font-size: 9px;
		margin: 0;
	}
	.me {
		display: flex;
		align-items: center;
		gap: 6px;
		background: none;
		border: 1px solid transparent;
		border-radius: 8px;
		color: var(--muted);
		font-size: 12.5px;
		cursor: pointer;
		padding: 3px 8px;
	}
	.me b {
		color: var(--accent);
		font-weight: 650;
	}
	.me .pencil {
		width: 12px;
		height: 12px;
		opacity: 0.5;
		transition: opacity 0.15s;
	}
	.me:hover {
		border-color: var(--border);
		background: color-mix(in srgb, var(--surface-2) 60%, transparent);
	}
	.me:hover .pencil {
		opacity: 1;
		color: var(--accent);
	}
	.rename-form {
		display: flex;
		align-items: center;
		gap: 6px;
		flex: 1;
	}
	.rename-input {
		flex: 1;
		min-width: 0;
		background: var(--surface-2);
		border: 1px solid color-mix(in srgb, var(--accent) 55%, var(--border));
		color: var(--text);
		border-radius: 8px;
		padding: 5px 10px;
		font-size: 12.5px;
	}
	.rename-input:focus {
		outline: 0;
		border-color: var(--accent);
	}
	.chip {
		width: 24px;
		height: 24px;
		flex: none;
		border-radius: 7px;
		border: 1px solid var(--border);
		background: transparent;
		color: var(--muted);
		display: grid;
		place-items: center;
		cursor: pointer;
	}
	.chip svg {
		width: 12px;
		height: 12px;
	}
	.chip:hover {
		color: var(--text);
		border-color: var(--muted);
	}
	.chip.ok {
		color: var(--accent);
		border-color: color-mix(in srgb, var(--accent) 50%, var(--border));
	}
	.chip.ok:hover {
		background: var(--accent);
		color: #141416;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--danger);
		margin-left: auto;
		flex: none;
		transition: background 0.3s;
	}
	.dot.on {
		background: #5fc493;
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
