<script>
	// The bottom tray: Now Playing (with elapsed ring), and — once the
	// structured metadata channel exists — Up Next and Tonight cells.
	// Until then those cells only render when data is present.
	let { status } = $props();

	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	const elapsed = $derived.by(() => {
		if (!status?.lastConnectTime) return null;
		const ms = now - new Date(status.lastConnectTime).getTime();
		if (ms < 0) return null;
		const h = Math.floor(ms / 3600000);
		const m = Math.floor((ms % 3600000) / 60000);
		const s = Math.floor((ms % 60000) / 1000);
		return h > 0
			? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
			: `${m}:${String(s).padStart(2, '0')}`;
	});

	// Ring sweeps once per hour — an ambient clock, not a progress claim.
	const ringOffset = $derived.by(() => {
		if (!status?.lastConnectTime) return 119.4;
		const mins = ((now - new Date(status.lastConnectTime).getTime()) / 60000) % 60;
		return 119.4 - (mins / 60) * 119.4;
	});

	const title = $derived(status?.streamTitle || 'Live');
</script>

<div class="now-tray">
	<div class="tray-cell now">
		<div class="vinyl"></div>
		<div class="txt">
			<div class="cell-label hot">Now Playing</div>
			<div class="t">{title}</div>
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
					stroke-dasharray="119.4"
					stroke-dashoffset={ringOffset}
				/>
			</svg>
			<div class="pct">{elapsed ?? ''}</div>
		</div>
	</div>
	{#if status?.upNext}
		<div class="tray-sep"></div>
		<div class="tray-cell">
			<div class="txt">
				<div class="cell-label">Up Next</div>
				<div class="t">{status.upNext}</div>
			</div>
		</div>
	{/if}
	{#if status?.schedule}
		<div class="tray-sep"></div>
		<div class="tray-cell">
			<div class="txt">
				<div class="cell-label">{status.schedule.when}</div>
				<div class="t">{status.schedule.title}</div>
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
</style>
