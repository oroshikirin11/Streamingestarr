<script>
	// The open stage: when the admin shares a room's ingest (Rooms →
	// Ingest → Open stage), everyone inside sees the address and stream
	// keys and may broadcast here. Renders nothing while the room keeps
	// its stage closed — the server answers {enabled:false}.
	import { getRoomBroadcast } from '../api.js';

	let { locked = false } = $props();

	let stage = $state(null);
	$effect(() => {
		if (locked) {
			stage = null;
			return;
		}
		getRoomBroadcast()
			.then((d) => (stage = d?.enabled ? d : null))
			.catch(() => (stage = null));
	});

	const host = typeof window === 'undefined' ? '' : window.location.hostname;
	const addresses = $derived.by(() => {
		if (!stage) return [];
		const out = [];
		if (stage.rtmpPort) out.push({ key: 'rtmp', label: 'RTMP', value: `rtmp://${host}:${stage.rtmpPort}` });
		if (stage.srt && stage.srtPort) out.push({ key: 'srt', label: 'SRT', value: `srt://${host}:${stage.srtPort}` });
		if (stage.tcp && stage.tcpPort) out.push({ key: 'tcp', label: 'TCP', value: `tcp://${host}:${stage.tcpPort}` });
		return out;
	});

	// '' | 'copied' | 'selected', keyed by row — selected is the fallback
	// when the clipboard is out of reach: the field is selected for a
	// manual Ctrl+C.
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
	function copyLabel(key) {
		if (copiedKey !== key || !copied) return 'Copy';
		return copied === 'copied' ? 'Copied' : 'Selected — Ctrl+C';
	}
</script>

{#if stage}
	<div class="stage">
		<div class="stage-head">
			<span class="stage-kicker">open stage</span>
			<span class="stage-sub">anyone here may broadcast — point your encoder at an address below with one of the keys</span>
		</div>
		{#each addresses as a (a.key)}
			<div class="credrow">
				<span class="credlabel">{a.label}</span>
				<input class="cred" readonly value={a.value} spellcheck="false" aria-label={a.label + ' ingest address'} onfocus={(e) => e.target.select()} />
				<button class="copy" class:done={copiedKey === a.key && Boolean(copied)} onclick={(e) => copy(a.value, a.key, e.currentTarget.previousElementSibling)}>{copyLabel(a.key)}</button>
			</div>
		{/each}
		{#each stage.keys ?? [] as k, i}
			<div class="credrow">
				<span class="credlabel">Key{#if k.comment}&nbsp;· {k.comment}{/if}</span>
				<input class="cred" readonly value={k.key} spellcheck="false" aria-label="Stream key" onfocus={(e) => e.target.select()} />
				<button class="copy" class:done={copiedKey === 'key' + i && Boolean(copied)} onclick={(e) => copy(k.key, 'key' + i, e.currentTarget.previousElementSibling)}>{copyLabel('key' + i)}</button>
			</div>
		{/each}
		{#if !(stage.keys ?? []).length}
			<p class="empty">the stage is open but this room has no stream keys yet — ask the admin to add one</p>
		{/if}
		<p class="note">
			The key is the RTMP path (<span class="mono">rtmp://…/&lt;key&gt;</span>) and the SRT streamid.{#if stage.srtPassphraseRequired}
				SRT needs the room's passphrase — ask whoever gave you the key.{/if}
		</p>
	</div>
{/if}

<style>
	.stage {
		width: min(760px, 100%);
		margin: 0 auto;
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 16px 16px 14px;
		border: 1px solid var(--border-soft);
		border-radius: var(--radius);
		background: color-mix(in srgb, var(--surface) 60%, transparent);
		text-align: left;
	}
	.stage-head {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 10px;
		margin-bottom: 4px;
	}
	.stage-kicker {
		font-size: 10.5px;
		letter-spacing: 0.26em;
		text-transform: uppercase;
		color: var(--accent);
	}
	.stage-sub {
		font-size: 12px;
		color: var(--muted);
	}
	.credrow {
		display: grid;
		grid-template-columns: minmax(70px, 180px) 1fr auto;
		align-items: stretch;
		gap: 8px;
	}
	.credlabel {
		align-self: center;
		font-size: 11px;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.cred {
		flex: 1;
		min-width: 0;
		appearance: none;
		font-family: ui-monospace, monospace;
		font-size: 12.5px;
		color: var(--text);
		text-align: left;
		padding: 9px 12px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface-2);
		line-height: 1.4;
	}
	.cred:focus {
		outline: none;
		border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
	}
	.copy {
		appearance: none;
		font: inherit;
		font-size: 12px;
		font-weight: 650;
		padding: 0 14px;
		border-radius: 10px;
		border: 0;
		background: var(--accent);
		color: #101216;
		cursor: pointer;
		white-space: nowrap;
		min-width: 78px;
		transition: background 0.15s;
	}
	.copy.done {
		background: color-mix(in srgb, var(--accent) 45%, var(--surface-2));
		color: var(--text);
	}
	.empty,
	.note {
		color: var(--muted);
		font-size: 12.5px;
		line-height: 1.5;
	}
	.note {
		margin-top: 2px;
	}
	.mono {
		font-family: ui-monospace, monospace;
	}
	@media (max-width: 700px) {
		.credrow {
			grid-template-columns: 1fr auto;
		}
		.credlabel {
			grid-column: 1 / -1;
		}
	}
</style>
