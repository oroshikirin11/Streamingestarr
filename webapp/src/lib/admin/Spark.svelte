<script>
	/**
	 * A small time-series area chart for the Status page. One series per
	 * chart on purpose (the title names it — no legend), thin 2px line,
	 * quiet fill, and a crosshair + tooltip on hover. Data is the server's
	 * TimestampedValue shape: [{ time, value }].
	 */
	let { data = [], unit = '', max = null, color = '#dd6a4d', height = 56 } = $props();

	const W = 300; // internal coordinate space; the svg stretches to fit

	let points = $derived.by(() => {
		const rows = (data ?? []).filter((d) => typeof d?.value === 'number');
		if (rows.length < 2) return null;
		const t0 = new Date(rows[0].time).getTime();
		const t1 = new Date(rows[rows.length - 1].time).getTime();
		const span = Math.max(1, t1 - t0);
		const top = max ?? Math.max(1, ...rows.map((d) => d.value)) * 1.1;
		return rows.map((d) => ({
			x: ((new Date(d.time).getTime() - t0) / span) * W,
			y: height - 4 - (Math.min(d.value, top) / top) * (height - 10),
			value: d.value,
			time: new Date(d.time)
		}));
	});

	let linePath = $derived(points ? 'M' + points.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join('L') : '');
	let areaPath = $derived(points
		? `${linePath}L${W},${height}L0,${height}Z`
		: '');

	let hover = $state(null);
	function onMove(e) {
		if (!points) return;
		const rect = e.currentTarget.getBoundingClientRect();
		const x = ((e.clientX - rect.left) / rect.width) * W;
		let best = points[0];
		for (const p of points) if (Math.abs(p.x - x) < Math.abs(best.x - x)) best = p;
		hover = { ...best, leftPct: (best.x / W) * 100 };
	}
	const fmtTime = (t) => t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	const fmtVal = (v) => (Number.isInteger(v) ? v : Math.round(v * 10) / 10);
</script>

<div class="spark" style="height:{height}px">
	{#if points}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<svg viewBox="0 0 {W} {height}" preserveAspectRatio="none" onmousemove={onMove} onmouseleave={() => (hover = null)}>
			<path d={areaPath} fill={color} fill-opacity="0.14" />
			<path d={linePath} fill="none" stroke={color} stroke-width="2" vector-effect="non-scaling-stroke" />
			{#if hover}
				<line x1={hover.x} y1="0" x2={hover.x} y2={height} stroke="currentColor" stroke-opacity="0.25" vector-effect="non-scaling-stroke" />
			{/if}
		</svg>
		{#if hover}
			<div class="tip" style="left:{hover.leftPct}%">
				{fmtVal(hover.value)}{unit} · {fmtTime(hover.time)}
			</div>
		{/if}
	{:else}
		<p class="empty">collecting…</p>
	{/if}
</div>

<style>
	.spark { position: relative; width: 100%; }
	svg { width: 100%; height: 100%; display: block; }
	.tip {
		position: absolute; top: -1.6rem; transform: translateX(-50%);
		background: var(--panel, #1c1c1f); border: 1px solid var(--border, #322f36);
		color: var(--text, #e6e3e7); border-radius: 6px; padding: 0.1rem 0.45rem;
		font-size: 0.72rem; white-space: nowrap; pointer-events: none; z-index: 3;
	}
	.empty { margin: 0; font-size: 0.75rem; opacity: 0.4; line-height: 3; text-align: center; }
</style>
