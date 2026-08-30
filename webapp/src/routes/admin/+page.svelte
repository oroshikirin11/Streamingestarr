<script>
	import '../../app.css';
	import { onMount } from 'svelte';
	import * as api from '$lib/admin/api.js';

	let section = $state('status');
	let cfg = $state(null);
	let status = $state(null);
	let feedback = $state({}); // per-card save feedback

	// editable copies
	let serverName = $state('');
	let streamTitle = $state('');
	let welcomeMessage = $state('');
	let keys = $state([]);
	let variants = $state([]);
	let latency = $state(2);
	let segmentFormat = $state('ts');
	let srtEnabled = $state(true);
	let srtPort = $state(9710);
	let reservationDays = $state(30);
	let chatDisabled = $state(false);
	let joinMessages = $state(true);
	let forbiddenNames = $state('');
	let newAdminPw = $state('');
	let newRoomPw = $state('');
	let messages = $state([]);
	let disabledUsers = $state([]);

	async function loadConfig() {
		cfg = await api.getServerConfig();
		serverName = cfg.instanceDetails?.name ?? '';
		streamTitle = cfg.instanceDetails?.streamTitle ?? '';
		welcomeMessage = cfg.instanceDetails?.welcomeMessage ?? '';
		keys = (cfg.streamKeys ?? []).map((k) => ({ key: k.key ?? '', comment: k.comment ?? '' }));
		variants = structuredClone($state.snapshot(cfg.videoSettings?.videoQualityVariants ?? []));
		latency = cfg.videoSettings?.latencyLevel ?? 2;
		segmentFormat = cfg.videoSegmentFormat ?? 'ts';
		srtEnabled = cfg.srtServerEnabled ?? true;
		srtPort = cfg.srtServerPort ?? 9710;
		reservationDays = cfg.chatNameReservationDays ?? 30;
		chatDisabled = cfg.chatDisabled ?? false;
		joinMessages = cfg.chatJoinMessagesEnabled ?? true;
		forbiddenNames = (cfg.forbiddenUsernames ?? []).join(', ');
	}

	async function refreshStatus() {
		try {
			status = await api.getAdminStatus();
		} catch {}
	}

	onMount(() => {
		loadConfig().catch(() => {});
		refreshStatus();
		const t = setInterval(refreshStatus, 5000);
		return () => clearInterval(t);
	});

	async function save(card, fn) {
		feedback[card] = '…';
		try {
			const res = await fn();
			feedback[card] = res?.success === false ? (res.message || 'failed') : (res?.message || 'saved ✓');
		} catch (e) {
			feedback[card] = 'failed';
		}
		setTimeout(() => (feedback[card] = ''), 4000);
	}

	function randomKey() {
		const bytes = crypto.getRandomValues(new Uint8Array(12));
		return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
	}

	async function loadModeration() {
		try {
			messages = ((await api.getChatMessages()) ?? []).slice(-60).reverse();
			disabledUsers = (await api.getDisabledUsers()) ?? [];
		} catch {}
	}

	function pick(s) {
		section = s;
		if (s === 'chat') loadModeration();
	}

	const host = typeof location !== 'undefined' ? location.hostname : 'localhost';
	const uptime = $derived.by(() => {
		const t = status?.broadcaster?.time;
		if (!t || !status?.broadcastActive) return null;
		const mins = Math.floor((Date.now() - new Date(t).getTime()) / 60000);
		return mins < 60 ? `${mins}m` : `${Math.floor(mins / 60)}h ${mins % 60}m`;
	});

	const SECTIONS = [
		['status', 'Status'],
		['stream', 'Stream'],
		['video', 'Video'],
		['chat', 'Chat'],
		['settings', 'Settings']
	];
</script>

<svelte:head>
	<title>Streamingestarr — Admin</title>
</svelte:head>

<div class="admin">
	<nav>
		<div class="brand"><b>●</b> Streamingestarr <span>admin</span></div>
		{#each SECTIONS as [id, label]}
			<button class:on={section === id} onclick={() => pick(id)}>{label}</button>
		{/each}
		<div class="grow"></div>
		<a class="back" href="/">← Back to the theater</a>
	</nav>

	<main>
		{#if section === 'status'}
			<h1>Status</h1>
			<div class="card">
				<div class="statusline">
					<span class="dot" class:live={status?.online}></span>
					{#if status?.online}
						<b>Live</b> · {status.viewerCount ?? 0} watching{#if uptime}&nbsp;· ingest up {uptime}{/if}
					{:else if status?.broadcaster}
						<b>Ingest connected</b> — buffering to viewers
					{:else}
						<b>Offline</b> — waiting for a stream
					{/if}
				</div>
				{#if status?.broadcaster}
					<table class="kv">
						<tbody>
							<tr><td>Source</td><td>{status.broadcaster.remoteAddr}</td></tr>
							<tr><td>Encoder</td><td>{status.broadcaster.streamDetails?.encoder || '—'}</td></tr>
							<tr><td>Video</td><td>{status.broadcaster.streamDetails?.videoCodec || '—'} {status.broadcaster.streamDetails?.width || ''}{status.broadcaster.streamDetails?.width ? '×' + status.broadcaster.streamDetails?.height : ''}</td></tr>
							<tr><td>Audio</td><td>{status.broadcaster.streamDetails?.audioCodec || '—'}</td></tr>
						</tbody>
					</table>
					<button class="danger" onclick={() => save('disconnect', () => api.disconnectStream())}>Disconnect the stream</button>
					<span class="fb">{feedback['disconnect'] ?? ''}</span>
				{/if}
			</div>

		{:else if section === 'stream'}
			<h1>Stream</h1>
			<div class="card">
				<h2>Ingest</h2>
				<p class="hint">Point your source at either. The stream key doubles as the SRT streamid.</p>
				{#each keys.slice(0, 1) as k}
					<table class="kv mono">
						<tbody>
							<tr><td>RTMP</td><td>rtmp://{host}:{cfg?.rtmpServerPort ?? 1935}/live/{k.key}</td></tr>
							<tr><td>SRT</td><td>srt://{host}:{srtPort}?streamid={k.key}</td></tr>
						</tbody>
					</table>
				{/each}
				<div class="row">
					<label><input type="checkbox" bind:checked={srtEnabled} onchange={() => save('srt', () => api.setSRTEnabled(srtEnabled))} /> SRT ingest enabled</label>
					<label>UDP port <input class="short" type="number" bind:value={srtPort} /></label>
					<button onclick={() => save('srt', () => api.setSRTPort(Number(srtPort)))}>Save port</button>
					<span class="fb">{feedback['srt'] ?? ''}</span>
				</div>
			</div>
			<div class="card">
				<h2>Stream keys</h2>
				{#each keys as k, i}
					<div class="row">
						<input class="mono grow-input" bind:value={k.key} placeholder="key" />
						<input bind:value={k.comment} placeholder="comment" />
						<button class="ghost" onclick={() => (k.key = randomKey())}>Randomize</button>
						<button class="ghost danger-text" onclick={() => (keys = keys.filter((_, j) => j !== i))}>Remove</button>
					</div>
				{/each}
				<div class="row">
					<button class="ghost" onclick={() => (keys = [...keys, { key: randomKey(), comment: '' }])}>Add key</button>
					<button onclick={() => save('keys', () => api.setStreamKeys(keys.filter((k) => k.key)))}>Save keys</button>
					<span class="fb">{feedback['keys'] ?? ''}</span>
				</div>
			</div>

		{:else if section === 'video'}
			<h1>Video</h1>
			<div class="card">
				<h2>Output</h2>
				{#each variants as v, i}
					<div class="variant">
						<input bind:value={v.name} placeholder="name" />
						<label><input type="checkbox" bind:checked={v.videoPassthrough} /> video passthrough</label>
						<label><input type="checkbox" bind:checked={v.audioPassthrough} /> audio passthrough</label>
						{#if !v.videoPassthrough}
							<label>kbps <input class="short" type="number" bind:value={v.videoBitrate} /></label>
							<label>fps <input class="short" type="number" bind:value={v.framerate} /></label>
						{/if}
						<button class="ghost danger-text" onclick={() => (variants = variants.filter((_, j) => j !== i))}>Remove</button>
					</div>
				{/each}
				<div class="row">
					<button class="ghost" onclick={() => (variants = [...variants, { name: 'passthrough', videoPassthrough: true, audioPassthrough: true, cpuUsageLevel: 2 }])}>Add variant</button>
					<button onclick={() => save('variants', () => api.setOutputVariants(variants))}>Save output</button>
					<span class="fb">{feedback['variants'] ?? ''}</span>
				</div>
				<p class="hint">Passthrough relays the incoming stream untouched — the normal case when Jellystreamerr is the sender.</p>
			</div>
			<div class="card">
				<h2>Delivery</h2>
				<div class="row">
					<label>Segment container
						<select bind:value={segmentFormat} onchange={() => save('segfmt', () => api.setSegmentFormat(segmentFormat))}>
							<option value="ts">mpegts (H.264)</option>
							<option value="fmp4">fMP4 (AV1 / HEVC ready)</option>
						</select>
					</label>
					<label>Latency
						<select bind:value={latency} onchange={() => save('latency', () => api.setConfigValue('video/streamlatencylevel', Number(latency)))}>
							<option value={0}>Lowest</option><option value={1}>Low</option>
							<option value={2}>Default</option><option value={3}>High</option>
							<option value={4}>Highest buffer</option>
						</select>
					</label>
					<span class="fb">{feedback['segfmt'] || feedback['latency'] || ''}</span>
				</div>
			</div>

		{:else if section === 'chat'}
			<h1>Chat</h1>
			<div class="card">
				<div class="row">
					<label><input type="checkbox" checked={!chatDisabled} onchange={(e) => { chatDisabled = !e.target.checked; save('chaten', () => api.setConfigValue('chat/disable', chatDisabled)); }} /> Chat enabled</label>
					<label><input type="checkbox" bind:checked={joinMessages} onchange={() => save('chaten', () => api.setConfigValue('chat/joinmessagesenabled', joinMessages))} /> Join messages</label>
					<span class="fb">{feedback['chaten'] ?? ''}</span>
				</div>
				<div class="row">
					<label class="grow-input">Welcome message <input bind:value={welcomeMessage} /></label>
					<button onclick={() => save('welcome', () => api.setConfigValue('welcomemessage', welcomeMessage))}>Save</button>
					<span class="fb">{feedback['welcome'] ?? ''}</span>
				</div>
				<div class="row">
					<label class="grow-input">Forbidden names (comma-separated) <input bind:value={forbiddenNames} /></label>
					<button onclick={() => save('forbidden', () => api.setConfigValue('chat/forbiddenusernames', forbiddenNames.split(',').map((s) => s.trim()).filter(Boolean)))}>Save</button>
					<span class="fb">{feedback['forbidden'] ?? ''}</span>
				</div>
				<div class="row">
					<label>Name reservation (days, 0 = forever) <input class="short" type="number" min="0" max="3650" bind:value={reservationDays} /></label>
					<button onclick={() => save('resdays', () => api.setNameReservationDays(Number(reservationDays)))}>Save</button>
					<span class="fb">{feedback['resdays'] ?? ''}</span>
				</div>
			</div>
			<div class="card">
				<h2>Recent messages</h2>
				{#if messages.length === 0}<p class="hint">Nothing yet.</p>{/if}
				{#each messages as m (m.id)}
					<div class="msgrow" class:hidden-msg={m.hiddenAt}>
						<span class="who">{m.user?.displayName ?? '—'}</span>
						<span class="body">{@html m.body}</span>
						<button class="ghost" onclick={() => save('mod', async () => { const r = await api.setMessageVisibility([m.id], !!m.hiddenAt); m.hiddenAt = m.hiddenAt ? null : new Date().toISOString(); return r; })}>{m.hiddenAt ? 'Unhide' : 'Hide'}</button>
						{#if m.user?.id}
							<button class="ghost danger-text" onclick={() => save('mod', () => api.setUserEnabled(m.user.id, false))}>Ban user</button>
						{/if}
					</div>
				{/each}
				{#if disabledUsers.length}
					<h2>Banned</h2>
					{#each disabledUsers as u (u.id)}
						<div class="msgrow">
							<span class="who">{u.displayName}</span>
							<button class="ghost" onclick={() => save('mod', async () => { const r = await api.setUserEnabled(u.id, true); disabledUsers = disabledUsers.filter((x) => x.id !== u.id); return r; })}>Unban</button>
						</div>
					{/each}
				{/if}
				<span class="fb">{feedback['mod'] ?? ''}</span>
			</div>

		{:else if section === 'settings'}
			<h1>Settings</h1>
			<div class="card">
				<h2>Theater</h2>
				<div class="row">
					<label class="grow-input">Name <input bind:value={serverName} /></label>
					<button onclick={() => save('name', () => api.setConfigValue('name', serverName))}>Save</button>
					<span class="fb">{feedback['name'] ?? ''}</span>
				</div>
				<div class="row">
					<label class="grow-input">Stream title <input bind:value={streamTitle} /></label>
					<button onclick={() => save('title', () => api.setConfigValue('streamtitle', streamTitle))}>Save</button>
					<span class="fb">{feedback['title'] ?? ''}</span>
				</div>
			</div>
			<div class="card">
				<h2>Access</h2>
				<div class="row">
					<label class="grow-input">New room password <input type="password" bind:value={newRoomPw} /></label>
					<button onclick={() => save('roompw', async () => { const r = await api.setRoomPassword(newRoomPw); newRoomPw = ''; return r; })}>Change</button>
					<span class="fb">{feedback['roompw'] ?? ''}</span>
				</div>
				<p class="hint">Changing the room password ends every session except yours — the whole room re-enters with the new key.</p>
				<div class="row">
					<label class="grow-input">New admin password (min 8) <input type="password" bind:value={newAdminPw} /></label>
					<button onclick={() => save('adminpw', async () => { const r = await api.setAdminPassword(newAdminPw); newAdminPw = ''; return r; })}>Change</button>
					<span class="fb">{feedback['adminpw'] ?? ''}</span>
				</div>
			</div>
		{/if}
	</main>
</div>

<style>
	.admin {
		/* ops-blue accent: same family as the theater, different job */
		--accent: #6ba3f0;
		--radius: 10px;
		height: 100vh;
		display: flex;
		font-size: 13.5px;
	}
	nav {
		width: 210px;
		flex: none;
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 20px 14px;
		border-right: 1px solid var(--border);
		background: color-mix(in srgb, var(--surface) 60%, transparent);
	}
	.brand {
		font-size: 13px;
		font-weight: 700;
		margin: 0 8px 18px;
	}
	.brand b { color: var(--accent); }
	.brand span { color: var(--muted); font-weight: 400; }
	nav button {
		text-align: left;
		background: none;
		border: 0;
		color: var(--muted);
		padding: 8px 10px;
		border-radius: 8px;
		cursor: pointer;
		font-size: 13.5px;
	}
	nav button:hover { color: var(--text); background: var(--surface-2); }
	nav button.on { color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, transparent); font-weight: 600; }
	.grow { flex: 1; }
	.back { color: var(--muted); font-size: 12px; text-decoration: none; padding: 8px 10px; }
	.back:hover { color: var(--accent); }

	main { flex: 1; overflow-y: auto; padding: 26px 30px 60px; }
	h1 { font-size: 18px; font-weight: 650; margin-bottom: 16px; }
	h2 { font-size: 11px; letter-spacing: 0.16em; text-transform: uppercase; color: var(--muted); margin: 4px 0 12px; }
	.card {
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		padding: 16px 18px;
		margin-bottom: 14px;
		max-width: 860px;
	}
	.hint { color: var(--muted); font-size: 12px; margin: 8px 0 4px; }
	.row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin: 8px 0; }
	label { display: flex; align-items: center; gap: 7px; color: var(--muted); }
	input, select {
		background: var(--surface-2);
		border: 1px solid var(--border);
		color: var(--text);
		border-radius: 7px;
		padding: 7px 10px;
		font-size: 13px;
	}
	input:focus, select:focus { outline: 0; border-color: var(--accent); }
	input[type='checkbox'] { accent-color: var(--accent); width: 15px; height: 15px; padding: 0; }
	.short { width: 76px; }
	.grow-input { flex: 1; }
	.grow-input input { flex: 1; width: 100%; }
	button {
		background: var(--accent);
		color: #101216;
		border: 0;
		border-radius: 7px;
		padding: 7px 14px;
		font-weight: 600;
		cursor: pointer;
		font-size: 13px;
	}
	button.ghost {
		background: transparent;
		color: var(--muted);
		border: 1px solid var(--border);
		font-weight: 500;
	}
	button.ghost:hover { color: var(--accent); border-color: var(--accent); }
	button.danger { background: var(--danger); color: #16090a; }
	.danger-text:hover { color: var(--danger) !important; border-color: var(--danger) !important; }
	.fb { color: var(--accent); font-size: 12px; min-width: 60px; }

	.statusline { display: flex; align-items: center; gap: 10px; font-size: 15px; margin-bottom: 6px; }
	.dot { width: 9px; height: 9px; border-radius: 50%; background: var(--muted); }
	.dot.live { background: #5fc493; box-shadow: 0 0 10px #5fc493; }
	table.kv { border-collapse: collapse; margin: 10px 0; }
	table.kv td { padding: 4px 18px 4px 0; color: var(--text); }
	table.kv td:first-child { color: var(--muted); font-size: 12px; }
	.mono td:last-child, input.mono { font-family: ui-monospace, monospace; font-size: 12.5px; }

	.variant {
		display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
		border: 1px solid var(--border); border-radius: 8px; padding: 9px 12px; margin: 7px 0;
	}
	.msgrow {
		display: flex; align-items: center; gap: 10px; padding: 6px 0;
		border-bottom: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
		font-size: 13px;
	}
	.msgrow .who { color: var(--accent); font-weight: 600; flex: none; min-width: 110px; }
	.msgrow .body { flex: 1; min-width: 0; overflow-wrap: anywhere; }
	.msgrow.hidden-msg .body { opacity: 0.35; text-decoration: line-through; }
</style>
