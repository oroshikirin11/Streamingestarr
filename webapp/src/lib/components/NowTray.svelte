<script>
	// The bottom tray. With sender metadata (status.nowPlaying/schedule) it
	// shows the real film, true progress, up next and tonight's showing;
	// without, it falls back to the stream title and time-live.
	let { status } = $props();

	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	const np = $derived(status?.nowPlaying ?? null);
	const title = $derived(np?.title || status?.streamTitle || 'Live');
	const subtitle = $derived(np?.subtitle ?? '');

	// True position: pushed position + time since the push.
	const position = $derived.by(() => {
		// Guard everything a sender could get wrong — the ring must never
		// show NaN.
		if (!np || !Number.isFinite(np.duration) || np.duration <= 0) return null;
		const base = Number.isFinite(np.position) ? np.position : 0;
		if (np.paused) return Math.min(base, np.duration);
		const received = new Date(np.receivedAt).getTime();
		const drift = Number.isFinite(received) ? (now - received) / 1000 : 0;
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
		const ms = now - new Date(status.lastConnectTime).getTime();
		return ms >= 0 ? fmt(ms / 1000) : null;
	});

	// Ring: real clip progress when known; otherwise an ambient hour-sweep.
	const CIRC = 119.4;
	const ringOffset = $derived.by(() => {
		if (position != null && np?.duration) return CIRC - (position / np.duration) * CIRC;
		if (!status?.lastConnectTime) return CIRC;
		const mins = ((now - new Date(status.lastConnectTime).getTime()) / 60000) % 60;
		return CIRC - (mins / 60) * CIRC;
	});
	const ringText = $derived(np?.paused ? '⏸' : position != null ? fmt(position) : (elapsedLive ?? ''));

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
			<div class="t">{title}</div>
			{#if subtitle}<div class="s">{subtitle}</div>{/if}
		</div>
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
				{#if np.upNext.subtitle}<div class="s">{np.upNext.subtitle}</div>{/if}
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
		border: 1px solid var(--border);
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
		background: var(--border);
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
	.s {
		font-size: 11.5px;
		color: var(--muted);
		margin-top: 2px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
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
	}
</style>
