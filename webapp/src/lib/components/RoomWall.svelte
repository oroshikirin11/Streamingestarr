<script>
	// The room overview — mockup 03 "screen wall" made real. One tile per
	// room: live tiles show the room's actual preview (/preview.gif, which
	// the server regenerates every ~20s per room) glowing with its own
	// colours; resting tiles wait in the dark. The grid flows with however
	// many rooms exist.
	import { dedupeSubtitle } from '../text.js';

	let { rooms = [] } = $props();

	const REST_LINES = [
		'the projector sleeps',
		'between showings',
		'the lights are low',
		'the room is resting'
	];

	// Refresh previews on the generator's own cadence. A room that just
	// went live has no gif for its first seconds — the error fallback shows
	// a soft gradient until the next tick finds one.
	let tick = $state(Date.now());
	let broken = $state({});
	$effect(() => {
		const t = setInterval(() => {
			tick = Date.now();
			broken = {};
		}, 20000);
		return () => clearInterval(t);
	});
	const previewSrc = (id) => `/preview.gif?channel=${encodeURIComponent(id)}&t=${tick}`;

	const nowLine = (np) => {
		if (!np?.title) return '';
		const sub = dedupeSubtitle(np.title, np.subtitle);
		return sub ? `${np.title} — ${sub}` : np.title;
	};
</script>

<div class="wall-scroll">
	<div class="wall">
		<header class="top">
			<div class="kicker">pick a screen</div>
		</header>

		<div class="grid">
			{#each rooms as r, i (r.id)}
				<a class="screen-card" class:live={r.online} class:empty={!r.online} href={'/t/' + r.id}>
					<div class="halo-wrap">
						{#if r.online && !broken[r.id]}
							<img class="halo" src={previewSrc(r.id)} alt="" aria-hidden="true" />
						{:else if r.online}
							<div class="halo fallback"></div>
						{/if}
						<div class="screen">
							{#if r.online}
								{#if !broken[r.id]}
									<img
										class="preview"
										src={previewSrc(r.id)}
										alt=""
										onerror={() => (broken = { ...broken, [r.id]: true })}
									/>
								{:else}
									<div class="preview fallback"></div>
								{/if}
								<span class="live-chip">Live</span>
								{#if r.viewerCount}
									<span class="viewers-chip">{r.viewerCount} watching</span>
								{/if}
								{#if nowLine(r.nowPlaying)}
									<div class="nowbar"><small>Now playing</small>{nowLine(r.nowPlaying)}</div>
								{/if}
							{:else}
								<div class="off">{REST_LINES[i % REST_LINES.length]}</div>
							{/if}
						</div>
					</div>
					<div class="meta">
						<span class="name">{r.name}</span>
						<span class="enter">{r.online ? 'Enter →' : 'Wait inside →'}</span>
					</div>
				</a>
			{/each}
		</div>

		<footer class="hint">rooms light up here the moment their stream starts</footer>
	</div>
</div>

<style>
	.wall-scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
	}
	.wall {
		max-width: 980px;
		margin: 0 auto;
		padding: 26px 16px 60px;
	}

	header.top {
		text-align: center;
		margin-bottom: 30px;
	}
	.kicker {
		font-size: 11px;
		letter-spacing: 0.28em;
		text-transform: uppercase;
		color: var(--muted);
	}

	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(270px, 1fr));
		gap: 26px;
	}
	/* One or two rooms shouldn't stretch into cinema-width banners. */
	@media (min-width: 900px) {
		.grid {
			grid-template-columns: repeat(auto-fit, minmax(270px, 440px));
			justify-content: center;
		}
	}

	.screen-card {
		text-decoration: none;
		color: inherit;
		display: block;
	}

	.halo-wrap {
		position: relative;
		z-index: 0;
	}
	.screen {
		position: relative;
		aspect-ratio: 16 / 9;
		border-radius: 14px;
		background: #0d0d10;
		border: 1px solid var(--border);
		overflow: hidden;
		transition: transform 0.18s ease;
	}
	.screen-card:hover .screen {
		transform: translateY(-3px) scale(1.01);
	}
	.live .screen {
		border-color: color-mix(in srgb, var(--accent) 30%, var(--border));
	}

	.preview {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.preview.fallback,
	.halo.fallback {
		background: linear-gradient(120deg, #3a2f4a, #22384a 55%, #1c3a30);
	}

	/* The ambilight halo: the same preview, blurred and bled outward. */
	.halo {
		position: absolute;
		inset: -16px;
		width: calc(100% + 32px);
		height: calc(100% + 32px);
		z-index: -1;
		border-radius: 24px;
		object-fit: cover;
		filter: blur(28px) saturate(1.4);
		opacity: 0.55;
	}

	.empty .screen {
		opacity: 0.85;
	}
	.off {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		color: #514d57;
		font-size: 13px;
		font-style: italic;
	}

	.live-chip {
		position: absolute;
		top: 10px;
		left: 10px;
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 5px 11px;
		border-radius: 99px;
		background: #000a;
		backdrop-filter: blur(8px);
		font-size: 10px;
		letter-spacing: 0.14em;
		text-transform: uppercase;
		color: #5fc493;
	}
	.live-chip::before {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: #5fc493;
		box-shadow: 0 0 8px #5fc493;
	}
	.viewers-chip {
		position: absolute;
		top: 10px;
		right: 10px;
		padding: 5px 11px;
		border-radius: 99px;
		background: #000a;
		backdrop-filter: blur(8px);
		font-size: 11px;
		color: #cfcbc7;
	}

	.nowbar {
		position: absolute;
		inset: auto 0 0 0;
		padding: 26px 14px 12px;
		background: linear-gradient(transparent, #000c);
		font-size: 13px;
		color: var(--text);
		overflow: hidden;
		white-space: nowrap;
		text-overflow: ellipsis;
	}
	.nowbar small {
		display: block;
		font-size: 10px;
		letter-spacing: 0.18em;
		text-transform: uppercase;
		color: #b5aca7;
		margin-bottom: 3px;
	}

	.meta {
		display: flex;
		align-items: baseline;
		gap: 10px;
		padding: 13px 4px 0;
	}
	.meta .name {
		font-size: 15.5px;
		font-weight: 650;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.empty .meta .name {
		color: #8d8892;
	}
	.meta .enter {
		margin-left: auto;
		flex: none;
		font-size: 11px;
		letter-spacing: 0.1em;
		text-transform: uppercase;
		color: var(--accent);
		opacity: 0;
		transition: opacity 0.15s;
	}
	.screen-card:hover .enter {
		opacity: 1;
	}

	footer.hint {
		text-align: center;
		margin-top: 40px;
		color: #6d6871;
		font-size: 12.5px;
	}
</style>
