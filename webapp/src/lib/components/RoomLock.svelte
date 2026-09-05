<script>
	// A room's own door, inline: one field where the theater or the relay
	// links would be. A right answer is remembered for the session, so it
	// is asked once. The site password already let the viewer this far.
	import { unlockRoom } from '../api.js';

	// heading=false when a parent already names the room (the relay card).
	let { roomName = 'this room', what = 'theater', heading = true, onUnlocked } = $props();

	let password = $state('');
	let busy = $state(false);
	let error = $state('');

	async function submit(e) {
		e.preventDefault();
		if (!password || busy) return;
		busy = true;
		error = '';
		try {
			const res = await unlockRoom(password);
			if (res?.success) {
				password = '';
				onUnlocked?.();
			} else {
				error = res?.message || 'That did not work';
			}
		} catch {
			error = 'The room did not answer';
		} finally {
			busy = false;
		}
	}

	function focus(node) {
		node.focus();
	}
</script>

<form class="lock" onsubmit={submit}>
	<div class="kicker">{what === 'relay' ? 'the links are behind a password' : 'this room has its own door'}</div>
	{#if heading}<h2>{roomName}</h2>{/if}
	<p class="sub">
		{what === 'relay'
			? 'Enter the room password to see the relay links.'
			: 'Enter the room password to take a seat.'}
	</p>
	<label class="field">
		<span>Room password</span>
		<input type="password" bind:value={password} autocomplete="current-password" use:focus disabled={busy} />
	</label>
	{#if error}<div class="error" role="alert">{error}</div>{/if}
	<button type="submit" disabled={!password || busy}>{busy ? '…' : 'Enter'}</button>
</form>

<style>
	.lock {
		width: min(360px, 100%);
		margin: 0 auto;
		display: flex;
		flex-direction: column;
		gap: 10px;
		text-align: center;
	}
	.kicker {
		font-size: 11px;
		letter-spacing: 0.28em;
		text-transform: uppercase;
		color: var(--muted);
	}
	h2 {
		font-size: 26px;
		font-weight: 400;
		letter-spacing: -0.01em;
		text-wrap: balance;
	}
	.sub {
		color: var(--muted);
		font-size: 13.5px;
		margin-bottom: 6px;
	}
	.field {
		display: flex;
		flex-direction: column;
		gap: 6px;
		text-align: left;
	}
	.field span {
		font-size: 12px;
		color: var(--muted);
	}
	input {
		width: 100%;
		background: var(--surface-2);
		border: 1px solid var(--border);
		color: var(--text);
		border-radius: 8px;
		padding: 10px 12px;
		font-size: 14px;
	}
	input:focus {
		outline: 0;
		border-color: var(--accent);
	}
	.error {
		color: var(--danger);
		font-size: 12.5px;
	}
	button {
		background: var(--accent);
		color: #101216;
		border: 0;
		border-radius: 8px;
		padding: 10px 16px;
		font-weight: 650;
		font-size: 14px;
		cursor: pointer;
	}
	button:disabled {
		opacity: 0.4;
		cursor: default;
	}
</style>
