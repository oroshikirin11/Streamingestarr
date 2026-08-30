<script>
	// The offline state — the reason to come back (docs/design.md §7).
	// Schedule/countdown light up once the metadata channel provides them;
	// until then the room rests with the last-watched note.
	let { status } = $props();

	const lastWatched = $derived.by(() => {
		const t = status?.lastDisconnectTime;
		if (!t) return null;
		const d = new Date(t);
		const days = Math.floor((Date.now() - d.getTime()) / 86400000);
		if (days === 0) return 'today';
		if (days === 1) return 'yesterday';
		return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
	});
</script>

<div class="lobby">
	<div class="ember-room">
		<div class="ember"></div>
		<div class="ember"></div>
		<div class="ember"></div>
		<div class="ember"></div>
		<div class="lobby-card">
			<div class="z">The room is resting</div>
			{#if status?.schedule}
				<h2>{status.schedule.title}</h2>
				<div class="when">{status.schedule.when}</div>
			{:else}
				<h2>See you at the next showing</h2>
				<div class="when">the doors open when the projector spins up</div>
			{/if}
			{#if lastWatched}
				<div class="foot">
					last watched together — <b>{status.streamTitle || lastWatched}</b>
					{#if status.streamTitle}<span class="dim"> · {lastWatched}</span>{/if}
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	.lobby {
		display: flex;
		flex: 1;
		min-height: 0;
	}
	.ember-room {
		flex: 1;
		border-radius: var(--radius);
		position: relative;
		overflow: hidden;
		display: grid;
		place-items: center;
		background:
			radial-gradient(ellipse 90% 70% at 50% 115%, #2a1c18 0%, transparent 55%),
			linear-gradient(180deg, #17161a, #121114);
		box-shadow:
			inset 0 0 0 1px #ffffff0d,
			0 0 120px -30px color-mix(in srgb, var(--accent) 18%, transparent);
	}
	.ember {
		position: absolute;
		bottom: -4px;
		width: 3px;
		height: 3px;
		border-radius: 50%;
		background: var(--accent);
		opacity: 0;
		animation: rise 7s infinite;
	}
	.ember:nth-child(1) { left: 24%; animation-delay: 0s; }
	.ember:nth-child(2) { left: 46%; animation-delay: 2.2s; }
	.ember:nth-child(3) { left: 62%; animation-delay: 4.1s; }
	.ember:nth-child(4) { left: 78%; animation-delay: 1.3s; }
	@keyframes rise {
		0% { transform: translateY(0); opacity: 0; }
		12% { opacity: 0.7; }
		100% { transform: translateY(-46vh); opacity: 0; }
	}
	.lobby-card {
		text-align: center;
		padding: 20px;
	}
	.z {
		font-size: 12px;
		letter-spacing: 0.3em;
		text-transform: uppercase;
		color: var(--muted);
	}
	h2 {
		font-size: 34px;
		font-weight: 300;
		margin: 18px 0 4px;
	}
	.when {
		font-size: 13.5px;
		color: var(--muted);
	}
	.foot {
		margin-top: 30px;
		font-size: 12px;
		color: #6b6660;
	}
	.foot b {
		color: var(--muted);
		font-weight: 500;
	}
	.dim {
		color: #6b6660;
	}
</style>
