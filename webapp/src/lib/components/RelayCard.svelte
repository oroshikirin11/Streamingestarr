<script>
	// The relay: the room's links for an external player (VRChat's video
	// players, VLC, anything that pulls a URL). Full-page in relay mode,
	// a panel under the tray in "both". One link shown at a time, picked
	// by where it goes, one button to copy it. When the room's stage is
	// open (admin: Rooms → Ingest → Open stage) the card also shows the
	// ingest address and stream keys, so anyone here may broadcast.
	import { dedupeSubtitle } from '../text.js';
	import { currentChannel } from '../api.js';
	import OpenStage from './OpenStage.svelte';
	import RoomLock from './RoomLock.svelte';

	let { status, roomName = 'this room', compact = false, onUnlocked } = $props();

	const relay = $derived(status?.relay ?? null);
	const locked = $derived(Boolean(status?.access?.relayLocked));
	const np = $derived(status?.nowPlaying ?? null);
	const nowLine = $derived.by(() => {
		if (!np?.title) return '';
		const sub = dedupeSubtitle(np.title, np.subtitle);
		return sub ? `${np.title} — ${sub}` : np.title;
	});

	// While the stream runs the card shows a small live preview — the same
	// still the room wall uses, refreshed on the thumbnail generator's own
	// ~20s rhythm. Kept behind the room lock like everything else here.
	let tick = $state(Date.now());
	let previewBroken = $state(false);
	$effect(() => {
		if (!status?.online) return;
		const t = setInterval(() => {
			previewBroken = false;
			tick = Date.now();
		}, 20000);
		return () => clearInterval(t);
	});
	const previewSrc = $derived(`/thumbnail.jpg?channel=${encodeURIComponent(currentChannel())}&t=${tick}`);

	// Where a link goes decides which one to show; the protocol is the
	// small print. Remembered per browser.
	const KINDS = {
		rtsp: { label: 'PC', note: 'RTSP over TCP — lowest latency, what AVPro on PC plays best' },
		ts: { label: 'Quest', note: 'MPEG-TS over HTTP — what AVPro on Quest plays best' },
		hls: { label: 'Other', note: 'HLS — browsers, VLC and everything else; the theater\'s own rendition' }
	};
	const kinds = $derived((relay?.protocols ?? []).filter((k) => KINDS[k]));
	let pick = $state('');
	$effect(() => {
		if (!kinds.length) return;
		let remembered = '';
		try {
			remembered = localStorage.getItem('sgr_relay_pick') ?? '';
		} catch {}
		if (!kinds.includes(pick)) pick = kinds.includes(remembered) ? remembered : kinds[0];
	});
	function choose(k) {
		pick = k;
		try {
			localStorage.setItem('sgr_relay_pick', k);
		} catch {}
	}
	const link = $derived(relay?.links?.[pick] ?? '');

	// One copy state for the whole card, keyed by what was copied, so a
	// second button does not inherit the first one's "Copied".
	// '' | 'copied' | 'selected' — selected is the fallback when the
	// clipboard is out of reach (plain http, an old browser): the field is
	// selected for a manual Ctrl+C.
	let copied = $state('');
	let copiedKey = $state('');
	let copyTimer;
	async function copy(value, key, inputEl) {
		if (!value) return;
		clearTimeout(copyTimer);
		copiedKey = key;
		try {
			await navigator.clipboard.writeText(value);
			copied = 'copied';
		} catch {
			inputEl?.focus();
			inputEl?.select();
			copied = 'selected';
		}
		copyTimer = setTimeout(() => ((copied = ''), (copiedKey = '')), 2200);
	}
	function copyLabel(key, idle = 'Copy') {
		if (copiedKey !== key || !copied) return idle;
		return copied === 'copied' ? 'Copied' : 'Selected — press Ctrl+C';
	}

	let relayLinkEl = $state(null);
</script>

<section class="relay" class:compact>
	{#if !compact}
		<div class="kicker">room in relay mode</div>
		<h1>{roomName}</h1>
	{/if}

	{#if status?.online && !locked && !compact}
		<div class="live">
			{#if !previewBroken}
				<img class="preview" src={previewSrc} alt="Live preview" onerror={() => (previewBroken = true)} />
			{/if}
			{#if nowLine}
				<div class="nowline"><span class="label">Now playing</span><span class="title">{nowLine}</span></div>
			{/if}
		</div>
	{:else if !compact}
		<p class="rest">nothing is playing yet — the link works the moment the stream starts</p>
	{/if}

	{#if locked}
		<div class="lockwrap"><RoomLock {roomName} what="relay" heading={compact} {onUnlocked} /></div>
	{:else if !relay || !kinds.length}
		<p class="rest">no relay links are enabled for this room</p>
	{:else}
		{#if kinds.length > 1}
			<div class="picker" role="tablist" aria-label="Where the link goes">
				{#each kinds as k}
					<button role="tab" aria-selected={pick === k} class:on={pick === k} onclick={() => choose(k)}>{KINDS[k].label}</button>
				{/each}
			</div>
		{/if}
		<div class="linkbox">
			<input
				class="link"
				readonly
				value={link}
				spellcheck="false"
				aria-label="Relay link"
				bind:this={relayLinkEl}
				onfocus={(e) => e.target.select()}
			/>
			<button class="copy" class:done={copiedKey === 'link' && Boolean(copied)} onclick={() => copy(link, 'link', relayLinkEl)} aria-live="polite">{copyLabel('link', 'Copy link')}</button>
		</div>
		<p class="note">{KINDS[pick]?.note}. Paste it into the world's video player — everyone in the instance needs untrusted URLs allowed.</p>
		<div class="players">{relay.players === 1 ? '1 player connected' : `${relay.players} players connected`}</div>
	{/if}

	{#if !locked}
		<OpenStage online={Boolean(status?.online)} />
	{/if}
</section>

<style>
	.relay {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 18px;
		padding: 30px 20px 40px;
		text-align: center;
		background: radial-gradient(ellipse at 50% 30%, color-mix(in srgb, var(--accent) 9%, transparent), transparent 60%);
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
	}
	.relay.compact {
		flex: none;
		padding: 18px 20px 20px;
		gap: 12px;
		background: color-mix(in srgb, var(--surface) 75%, transparent);
	}
	.kicker {
		font-size: 11px;
		letter-spacing: 0.28em;
		text-transform: uppercase;
		color: var(--accent);
	}
	h1 {
		font-size: 34px;
		font-weight: 400;
		letter-spacing: -0.01em;
		text-wrap: balance;
	}
	.live {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
	}
	.preview {
		width: min(420px, 100%);
		aspect-ratio: 16 / 9;
		object-fit: cover;
		border-radius: 12px;
		border: 1px solid var(--border);
		box-shadow: 0 18px 50px -18px #000;
		background: #0a0a0c;
	}
	.nowline {
		display: flex;
		align-items: baseline;
		gap: 10px;
	}
	.label {
		font-size: 9.5px;
		letter-spacing: 0.22em;
		text-transform: uppercase;
		color: var(--muted);
		margin-bottom: 3px;
	}
	.title {
		font-size: 15px;
		font-weight: 600;
	}
	.rest {
		color: var(--muted);
		font-size: 13px;
	}
	.lockwrap {
		width: 100%;
		padding-top: 6px;
	}
	.picker {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 999px;
		padding: 3px;
		gap: 2px;
		background: color-mix(in srgb, var(--surface) 70%, transparent);
	}
	.picker button {
		appearance: none;
		font: inherit;
		font-size: 12.5px;
		font-weight: 600;
		padding: 6px 16px;
		border-radius: 999px;
		border: 0;
		background: transparent;
		color: var(--muted);
		cursor: pointer;
	}
	.picker button.on {
		background: var(--accent);
		color: #101216;
	}
	.linkbox {
		display: flex;
		align-items: stretch;
		gap: 8px;
		width: min(760px, 100%);
	}
	.link {
		flex: 1;
		min-width: 0;
		appearance: none;
		font-family: ui-monospace, monospace;
		font-size: 13.5px;
		color: var(--text);
		text-align: left;
		padding: 12px 14px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface-2);
		line-height: 1.4;
	}
	.link:focus {
		outline: none;
		border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
	}
	.copy {
		appearance: none;
		font: inherit;
		font-size: 13.5px;
		font-weight: 650;
		padding: 0 20px;
		border-radius: 10px;
		border: 0;
		background: var(--accent);
		color: #101216;
		cursor: pointer;
		white-space: nowrap;
		min-width: 118px;
		transition: background 0.15s;
	}
	.copy.done {
		background: color-mix(in srgb, var(--accent) 45%, var(--surface-2));
		color: var(--text);
	}
	.note {
		color: var(--muted);
		font-size: 12.5px;
		max-width: 62ch;
		line-height: 1.5;
	}
	.players {
		font-size: 12px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	@media (max-width: 700px) {
		h1 {
			font-size: 26px;
		}
		.linkbox {
			flex-direction: column;
		}
		.copy {
			padding: 12px 20px;
		}
	}
</style>
