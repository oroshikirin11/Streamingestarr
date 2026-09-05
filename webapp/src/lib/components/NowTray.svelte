<script>
	// The bottom tray. With sender metadata (status.nowPlaying/schedule) it
	// shows the real film, true progress, up next and tonight's showing;
	// without, it falls back to the stream title and time-live.
	import { dedupeSubtitle } from '../text.js';
	import { pauseVote, me, sendPauseVote } from '../chat.js';
	// clockSkewMs: server clock minus this device's, measured by the store.
	// Every stamp below is the server's, so the arithmetic runs on server
	// time — a viewer whose clock lagged the server saw a negative drift
	// clamped to zero, and a ring frozen at the last push.
	let { status, clockSkewMs = 0 } = $props();

	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	const np = $derived(status?.nowPlaying ?? null);
	const title = $derived(np?.title || status?.streamTitle || 'Live');
	const subtitle = $derived(dedupeSubtitle(np?.title, np?.subtitle));

	// True position: pushed position + time since the push.
	const serverNow = $derived(now + (Number.isFinite(clockSkewMs) ? clockSkewMs : 0));
	const position = $derived.by(() => {
		// Guard everything a sender could get wrong — the ring must never
		// show NaN.
		if (!np || !Number.isFinite(np.duration) || np.duration <= 0) return null;
		const base = Number.isFinite(np.position) ? np.position : 0;
		if (np.paused) return Math.min(base, np.duration);
		const received = new Date(np.receivedAt).getTime();
		const drift = Number.isFinite(received) ? (serverNow - received) / 1000 : 0;
		return Math.min(base + Math.max(drift, 0), np.duration);
	});

	const fmt = (s) => {
		if (s == null || !Number.isFinite(s)) return '';
		s = Math.floor(s);
		const h = Math.floor(s / 3600);
		const m = Math.floor((s % 3600) / 60);
		const sec = s % 60;
		return h > 0
			? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
			: `${m}:${String(sec).padStart(2, '0')}`;
	};

	const elapsedLive = $derived.by(() => {
		if (!status?.lastConnectTime) return null;
		const ms = serverNow - new Date(status.lastConnectTime).getTime();
		return ms >= 0 ? fmt(ms / 1000) : null;
	});

	// Ring: real clip progress when known; otherwise an ambient hour-sweep.
	const CIRC = 119.4;
	const ringOffset = $derived.by(() => {
		if (position != null && np?.duration) return CIRC - (position / np.duration) * CIRC;
		if (!status?.lastConnectTime) return CIRC;
		const mins = ((serverNow - new Date(status.lastConnectTime).getTime()) / 60000) % 60;
		return CIRC - (mins / 60) * CIRC;
	});
	const ringText = $derived(np?.paused ? '⏸' : position != null ? fmt(position) : (elapsedLive ?? ''));

	// Viewer pause votes. The pill exists only once the sender has
	// advertised the control; what it says comes from the room's
	// PAUSE_VOTE_STATE frames (chat.js), with status as the fallback.
	const pv = $derived($pauseVote);
	const advertised = $derived(np?.controls != null || pv?.available === true);
	const iVoted = $derived(Boolean(pv?.voters?.includes($me?.id)));
	const hostPaused = $derived((pv?.paused ?? np?.paused) && (pv?.pausedBy ?? np?.pausedBy) === 'host');
	const pending = $derived(pv?.pending || np?.pending || '');
	const cooldownLeft = $derived.by(() => {
		const until = (pv?.cooldownUntil ?? 0) * 1000;
		if (!until) return 0;
		return Math.max(0, Math.ceil((until - serverNow) / 1000));
	});
	const unavailableWhy = $derived.by(() => {
		if (!pv) return 'waiting for the room';
		if (pv.available) return '';
		if (pv.reason) return pv.reason;
		if (!status?.pauseVote?.enabled) return 'pause votes are switched off for this room';
		if (!status?.pauseVote?.controlConnected) return 'the sender is not connected';
		return 'the sender does not allow pause votes';
	});
	const pill = $derived.by(() => {
		if (!advertised) return null;
		if (hostPaused) return { kind: 'host', label: 'Paused by the host', title: 'The host paused the stream — votes are off until it resumes.' };
		if (pending === 'pause') return { kind: 'pending', label: 'Pausing…', note: 'reaches the room in a moment' };
		if (pending === 'resume') return { kind: 'pending', label: 'Resuming…', note: 'reaches the room in a moment' };
		if (!pv || !pv.available) return { kind: 'off', label: 'Pause votes off', title: unavailableWhy };
		const verb = pv.action === 'resume' ? 'Resume' : 'Pause';
		const t = `${pv.needed} vote${pv.needed === 1 ? '' : 's'} needed — half the room`;
		const note = cooldownLeft > 0 ? `votes count again in ${cooldownLeft} s` : pv.reason ? pv.reason : '';
		return { kind: 'vote', label: `${verb} · ${pv.votes} of ${pv.viewers}`, title: iVoted ? 'Press again to withdraw your vote' : t, note };
	});
	function pressPill() {
		if (!pv || pill?.kind !== 'vote') return;
		sendPauseVote(iVoted ? 'withdraw' : pv.action);
	}
	// "Paused by viewers · 2:41", counting from pausedAt (server seconds).
	const pausedLine = $derived.by(() => {
		const paused = pv?.paused ?? np?.paused;
		if (!paused) return null;
		const by = pv?.pausedBy ?? np?.pausedBy ?? '';
		const at = (pv?.pausedAt || np?.pausedAt || 0) * 1000;
		const who = by === 'viewers' ? 'Paused by viewers' : by === 'host' ? 'Paused by the host' : 'Paused';
		return at ? `${who} · ${fmt(Math.max(0, (serverNow - at) / 1000))}` : who;
	});

	const nextShowing = $derived.by(() => {
		const item = status?.schedule?.[0];
		if (!item) return null;
		const when = new Date(item.startsAt);
		const sameDay = when.toDateString() === new Date(now).toDateString();
		return {
			...item,
			label: (sameDay ? 'Tonight' : when.toLocaleDateString(undefined, { weekday: 'long' })) +
				' · ' + when.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
		};
	});
</script>

<div class="now-tray">
	<div class="tray-cell now">
		{#if np?.artworkId}
			<img class="mini-poster" src={'/artwork/' + np.artworkId} alt="" />
		{:else}
			<div class="vinyl"></div>
		{/if}
		<div class="txt">
			<div class="cell-label hot">Now Playing</div>
			<div class="t">
				{title}
				{#if status?.videoRange}<span class="hdr-badge" title="This broadcast is high dynamic range ({status.videoRange})">HDR</span>{/if}
			</div>
			{#if pausedLine}
				<div class="s paused-line">{pausedLine}</div>
			{:else if subtitle}
				<div class="s">{subtitle}</div>
			{/if}
		</div>
		{#if pill}
			<div class="vote" title={pill.title ?? ''}>
				<button
					class="pill {pill.kind}"
					class:pressed={pill.kind === 'vote' && iVoted}
					aria-pressed={pill.kind === 'vote' ? iVoted : undefined}
					disabled={pill.kind !== 'vote'}
					onclick={pressPill}
				>
					{#if pill.kind === 'pending'}<span class="spin"></span>{/if}
					{pill.label}
				</button>
				{#if pill.note}<div class="vote-note">{pill.note}</div>{/if}
			</div>
		{/if}
		<div class="ring">
			<svg width="46" height="46">
				<circle cx="23" cy="23" r="19" fill="none" stroke="#ffffff14" stroke-width="3" />
				<circle
					cx="23"
					cy="23"
					r="19"
					fill="none"
					stroke="var(--accent)"
					stroke-width="3"
					stroke-linecap="round"
					stroke-dasharray={CIRC}
					stroke-dashoffset={ringOffset}
				/>
			</svg>
			<div class="pct">{ringText}</div>
		</div>
	</div>
	{#if np?.upNext}
		<div class="tray-sep"></div>
		<div class="tray-cell">
			{#if np.upNext.artworkId}<img class="mini-poster" src={'/artwork/' + np.upNext.artworkId} alt="" />{/if}
			<div class="txt">
				<div class="cell-label">Up Next</div>
				<div class="t">{np.upNext.title}</div>
				{#if np.upNext.subtitle}<div class="s">{dedupeSubtitle(np.upNext.title, np.upNext.subtitle)}</div>{/if}
			</div>
		</div>
	{/if}
	{#if nextShowing}
		<div class="tray-sep"></div>
		<div class="tray-cell">
			{#if nextShowing.artworkId}<img class="mini-poster" src={'/artwork/' + nextShowing.artworkId} alt="" />{/if}
			<div class="txt">
				<div class="cell-label">{nextShowing.label}</div>
				<div class="t">{nextShowing.title}</div>
				{#if nextShowing.subtitle}<div class="s">{nextShowing.subtitle}</div>{/if}
			</div>
		</div>
	{/if}
</div>

<style>
	.now-tray {
		flex: none;
		display: flex;
		align-items: stretch;
		background: color-mix(in srgb, var(--surface) 75%, transparent);
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		padding: 14px 20px;
		backdrop-filter: blur(10px);
	}
	.tray-cell {
		display: flex;
		align-items: center;
		gap: 14px;
		min-width: 0;
		flex: 1;
	}
	.tray-cell.now {
		flex: 1.25;
	}
	.tray-sep {
		width: 1px;
		background: var(--border-soft);
		margin: 2px 20px;
		flex: none;
	}
	.txt {
		min-width: 0;
		flex: 1;
	}
	.mini-poster {
		width: 34px;
		height: 48px;
		border-radius: 5px;
		object-fit: cover;
		flex: none;
		border: 1px solid var(--border);
	}
	.vinyl {
		width: 44px;
		height: 44px;
		border-radius: 50%;
		flex: none;
		position: relative;
		background: repeating-radial-gradient(circle, #26232a 0 2px, #1b191f 2px 4px);
		border: 1px solid var(--border);
		animation: spin 14s linear infinite;
	}
	.vinyl::after {
		content: '';
		position: absolute;
		inset: 38%;
		border-radius: 50%;
		background: var(--accent);
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	.cell-label {
		font-size: 9.5px;
		letter-spacing: 0.22em;
		text-transform: uppercase;
		color: var(--muted);
		margin-bottom: 4px;
	}
	.cell-label.hot {
		color: var(--accent);
	}
	.t {
		font-size: 14px;
		font-weight: 650;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.hdr-badge {
		display: inline-block;
		vertical-align: 1px;
		margin-left: 6px;
		padding: 0 5px;
		border: 1px solid color-mix(in srgb, var(--accent) 55%, transparent);
		border-radius: 4px;
		color: var(--accent);
		font-size: 9px;
		font-weight: 700;
		letter-spacing: 0.08em;
		line-height: 15px;
	}
	.s {
		font-size: 11.5px;
		color: var(--muted);
		margin-top: 2px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.paused-line {
		color: var(--accent);
		font-variant-numeric: tabular-nums;
	}
	.vote {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: 3px;
		flex: none;
		/* the note wraps under the pill instead of squeezing the title */
		max-width: 170px;
	}
	.pill {
		appearance: none;
		font: inherit;
		font-size: 12px;
		font-weight: 650;
		line-height: 1;
		padding: 8px 13px;
		border-radius: 999px;
		border: 1px solid color-mix(in srgb, var(--accent) 55%, transparent);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
		color: var(--fg, inherit);
		cursor: pointer;
		white-space: nowrap;
		display: inline-flex;
		align-items: center;
		gap: 7px;
		transition: background 0.15s, border-color 0.15s;
	}
	.pill:hover:not(:disabled) {
		background: color-mix(in srgb, var(--accent) 22%, transparent);
	}
	.pill.pressed {
		background: var(--accent);
		border-color: var(--accent);
		color: #fff;
	}
	.pill:disabled {
		cursor: default;
	}
	.pill.off {
		border-color: var(--border-soft);
		background: transparent;
		color: var(--muted);
		font-weight: 500;
	}
	.pill.host {
		border-color: var(--border);
		background: color-mix(in srgb, var(--surface) 60%, transparent);
		color: var(--muted);
	}
	.pill.pending {
		border-color: color-mix(in srgb, var(--accent) 40%, transparent);
		color: var(--muted);
	}
	.spin {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		border: 2px solid color-mix(in srgb, var(--accent) 35%, transparent);
		border-top-color: var(--accent);
		animation: spin 0.9s linear infinite;
	}
	.vote-note {
		font-size: 10px;
		line-height: 1.25;
		color: var(--muted);
		text-align: right;
		text-wrap: balance;
	}
	.ring {
		position: relative;
		width: 46px;
		height: 46px;
		flex: none;
	}
	.ring svg {
		transform: rotate(-90deg);
	}
	.ring .pct {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		font-size: 9px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}

	@media (max-width: 860px) {
		.now-tray {
			padding: 10px 14px;
		}
		/* one cell on phones: now playing carries the essentials */
		.tray-sep,
		.tray-cell:not(.now) {
			display: none;
		}
		.vote-note {
			display: none;
		}
		.pill {
			padding: 7px 10px;
			font-size: 11px;
		}
	}
</style>
