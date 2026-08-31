<script>
	import '../../app.css';
	import { onMount } from 'svelte';
	import * as api from '$lib/admin/api.js';

	let section = $state('status');
	let cfg = $state(null);
	let status = $state(null);
	let toasts = $state([]);

	// editable copies
	let serverName = $state('');
	let streamTitle = $state('');
	let welcomeMessage = $state('');
	let keys = $state([]);
	let variants = $state([]);
	let latency = $state(2);
	let segmentFormat = $state('auto');
	let srtEnabled = $state(true);
	let srtPort = $state(9710);
	let reservationDays = $state(30);
	let chatEnabled = $state(true);
	let joinMessages = $state(true);
	let forbiddenNames = $state('');
	let newAdminPw = $state('');
	let newRoomPw = $state('');
	let messages = $state([]);
	let disabledUsers = $state([]);
	let ipBans = $state([]);
	let newBanIP = $state('');
	let logs = $state([]);
	let logFilter = $state('warnings');
	let hw = $state(null);
	let tokens = $state([]);
	let newTokenName = $state('');

	function toast(text, ok = true) {
		const id = crypto.randomUUID();
		toasts = [...toasts, { id, text, ok }];
		setTimeout(() => (toasts = toasts.filter((t) => t.id !== id)), 2600);
	}

	async function run(fn, okText = 'Saved') {
		try {
			const res = await fn();
			if (res?.success === false) toast(res.message || 'That did not work', false);
			else toast(res?.message && res.message !== 'changed' ? res.message : okText);
			return res;
		} catch {
			toast('That did not work', false);
		}
	}

	async function loadConfig() {
		cfg = await api.getServerConfig();
		serverName = cfg.instanceDetails?.name ?? '';
		streamTitle = cfg.instanceDetails?.streamTitle ?? '';
		welcomeMessage = cfg.instanceDetails?.welcomeMessage ?? '';
		keys = (cfg.streamKeys ?? []).map((k) => ({ key: k.key ?? '', comment: k.comment ?? '' }));
		variants = structuredClone($state.snapshot(cfg.videoSettings?.videoQualityVariants ?? []));
		latency = cfg.videoSettings?.latencyLevel ?? 2;
		segmentFormat = cfg.videoSegmentFormat ?? 'auto';
		srtEnabled = cfg.srtServerEnabled ?? true;
		srtPort = cfg.srtServerPort ?? 9710;
		reservationDays = cfg.chatNameReservationDays ?? 30;
		chatEnabled = !(cfg.chatDisabled ?? false);
		joinMessages = cfg.chatJoinMessagesEnabled ?? true;
		forbiddenNames = (cfg.forbiddenUsernames ?? []).join(', ');
	}

	async function refreshStatus() {
		try {
			status = await api.getAdminStatus();
			if (section === 'status') hw = await api.getHardwareStats();
		} catch {}
	}

	const latest = (series) => {
		const v = series?.[series.length - 1]?.value;
		return typeof v === 'number' ? Math.round(v) : null;
	};
	const kbps = (n) => (n ? `${n} kbps` : null);

	onMount(() => {
		loadConfig().catch(() => {});
		refreshStatus();
		const t = setInterval(refreshStatus, 5000);
		return () => clearInterval(t);
	});

	function randomKey() {
		const bytes = crypto.getRandomValues(new Uint8Array(12));
		return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
	}

	function copy(text) {
		navigator.clipboard?.writeText(text).then(() => toast('Copied'));
	}

	async function loadModeration() {
		try {
			messages = ((await api.getChatMessages()) ?? []).slice(-60).reverse();
			disabledUsers = (await api.getDisabledUsers()) ?? [];
			ipBans = (await api.getIPBans()) ?? [];
		} catch {}
	}

	async function loadLogs() {
		try {
			const list = logFilter === 'all' ? await api.getLogs() : await api.getWarnings();
			logs = (list ?? []).slice(0, 200);
		} catch {}
	}

	async function loadTokens() {
		try {
			tokens = (await api.getAccessTokens()) ?? [];
		} catch {}
	}

	function pick(s) {
		section = s;
		if (s === 'chat') loadModeration();
		if (s === 'logs') loadLogs();
		if (s === 'stream') loadTokens();
	}

	$effect(() => {
		if (section !== 'logs') return;
		logFilter;
		loadLogs();
		const t = setInterval(loadLogs, 10000);
		return () => clearInterval(t);
	});

	const host = typeof location !== 'undefined' ? location.hostname : 'localhost';
	const rtmpURL = $derived(`rtmp://${host}:${cfg?.rtmpServerPort ?? 1935}/live/${keys[0]?.key ?? ''}`);
	const srtURL = $derived(`srt://${host}:${srtPort}?streamid=${keys[0]?.key ?? ''}`);

	const uptime = $derived.by(() => {
		const t = status?.broadcaster?.time;
		if (!t) return null;
		const mins = Math.floor((Date.now() - new Date(t).getTime()) / 60000);
		return mins < 60 ? `${mins}m` : `${Math.floor(mins / 60)}h ${mins % 60}m`;
	});

	const SECTIONS = [
		['status', 'Status'],
		['stream', 'Stream'],
		['video', 'Video'],
		['chat', 'Chat'],
		['logs', 'Logs'],
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
			<hgroup><h1>Status</h1><p>What the ingest and the room are doing right now.</p></hgroup>

			<div class="card">
				<div class="statusline">
					<span class="dot" class:live={status?.online}></span>
					{#if status?.online}
						<b>Live</b><span class="sep">·</span>{status.viewerCount ?? 0} watching{#if uptime}<span class="sep">·</span>ingest up {uptime}{/if}
					{:else if status?.broadcaster}
						<b>Ingest connected</b><span class="sep">—</span>buffering to viewers
					{:else}
						<b>Offline</b><span class="sep">—</span>waiting for a stream
					{/if}
				</div>
			</div>

			{#if status?.broadcaster}
				{@const d = status.broadcaster.streamDetails ?? {}}
				<div class="card">
					<header><h2>Broadcaster</h2></header>
					<dl>
						<div><dt>Source</dt><dd>{status.broadcaster.remoteAddr}</dd></div>
						<div><dt>Connected</dt><dd>{uptime ?? '—'}{uptime ? ' ago' : ''}</dd></div>
						<div><dt>Encoder</dt><dd>{d.encoder || '—'}</dd></div>
						<div><dt>Video</dt><dd>{[d.videoCodec, d.width ? `${d.width}×${d.height}` : null, d.framerate ? `${d.framerate} fps` : null, kbps(d.videoBitrate)].filter(Boolean).join(' · ') || '—'}</dd></div>
						<div><dt>Audio</dt><dd>{d.videoOnly ? 'none — video only' : [d.audioCodec, kbps(d.audioBitrate)].filter(Boolean).join(' · ') || '—'}</dd></div>
					</dl>
					<footer>
						<button class="danger" onclick={() => run(api.disconnectStream, 'Stream disconnected')}>Disconnect the stream</button>
					</footer>
				</div>
			{/if}

			<div class="card">
				<header><h2>Viewers</h2></header>
				<div class="tiles">
					<div class="tile"><span class="num">{status?.viewerCount ?? 0}</span><span class="lab">right now</span></div>
					<div class="tile"><span class="num">{status?.sessionPeakViewerCount ?? 0}</span><span class="lab">session peak</span></div>
					<div class="tile"><span class="num">{status?.overallPeakViewerCount ?? 0}</span><span class="lab">all-time peak</span></div>
				</div>
			</div>

			{#if hw}
				<div class="card">
					<header><h2>Server health</h2></header>
					<div class="tiles">
						<div class="tile"><span class="num">{latest(hw.cpu) ?? '—'}<small>%</small></span><span class="lab">cpu</span></div>
						<div class="tile"><span class="num">{latest(hw.memory) ?? '—'}<small>%</small></span><span class="lab">memory</span></div>
						<div class="tile"><span class="num">{latest(hw.disk) ?? '—'}<small>%</small></span><span class="lab">disk</span></div>
					</div>
					{#if status?.health?.message}
						<p class="hint">{status.health.message}</p>
					{/if}
				</div>
			{/if}

		{:else if section === 'stream'}
			<hgroup><h1>Stream</h1><p>Where your source connects, and the keys that open the door.</p></hgroup>

			<div class="card">
				<header><h2>Ingest addresses</h2><p>Point your source at either. The stream key doubles as the SRT streamid.</p></header>
				<dl class="mono">
					<div><dt>RTMP</dt><dd>{rtmpURL}</dd><button class="ghost tiny" onclick={() => copy(rtmpURL)}>Copy</button></div>
					<div><dt>SRT</dt><dd>{srtURL}</dd><button class="ghost tiny" onclick={() => copy(srtURL)}>Copy</button></div>
				</dl>
			</div>

			<div class="card">
				<header><h2>SRT ingest</h2><p>The preferred path — carries AV1 and HEVC. Changes take effect after a restart.</p></header>
				<div class="field-row">
					<label class="switch">
						<input type="checkbox" bind:checked={srtEnabled} onchange={() => run(() => api.setSRTEnabled(srtEnabled))} />
						<span class="track"></span> Enabled
					</label>
					<div class="field compact">
						<label for="srtport">UDP port</label>
						<input id="srtport" type="number" bind:value={srtPort} />
					</div>
				</div>
				<footer><button onclick={() => run(() => api.setSRTPort(Number(srtPort)))}>Save</button></footer>
			</div>

			<div class="card">
				<header><h2>Stream keys</h2><p>Anyone holding a key can broadcast to the theater.</p></header>
				{#each keys as k, i}
					<div class="keyrow">
						<input class="mono" bind:value={k.key} placeholder="key" />
						<input class="comment" bind:value={k.comment} placeholder="comment" />
						<button class="ghost tiny" onclick={() => copy(k.key)}>Copy</button>
						<button class="ghost tiny" onclick={() => (k.key = randomKey())}>Randomize</button>
						<button class="ghost tiny danger-text" onclick={() => (keys = keys.filter((_, j) => j !== i))}>Remove</button>
					</div>
				{/each}
				<footer>
					<button class="ghost" onclick={() => (keys = [...keys, { key: randomKey(), comment: '' }])}>Add a key</button>
					<button onclick={() => run(() => api.setStreamKeys(keys.filter((k) => k.key)))}>Save</button>
				</footer>
			</div>

			<div class="card">
				<header><h2>Access tokens</h2><p>For integrations that push metadata or chat lines — Jellystreamerr's Streamingestarr mode uses one. Not the same as a stream key.</p></header>
				{#if tokens.length === 0}<p class="empty">No tokens yet.</p>{/if}
				{#each tokens as t (t.accessToken)}
					<div class="msgrow">
						<span class="who">{t.displayName}</span>
						<span class="body mono-text dim">{t.accessToken}</span>
						<span class="actions">
							<button class="ghost tiny" onclick={() => copy(t.accessToken)}>Copy</button>
							<button class="ghost tiny danger-text" onclick={() => run(async () => { const r = await api.deleteAccessToken(t.accessToken); tokens = tokens.filter((x) => x.accessToken !== t.accessToken); return r; }, 'Token revoked')}>Revoke</button>
						</span>
					</div>
				{/each}
				<div class="field-row">
					<div class="field compact"><label for="tokname">Name</label><input id="tokname" bind:value={newTokenName} placeholder="jellystreamerr" /></div>
				</div>
				<footer>
					<button disabled={!newTokenName.trim()} onclick={() => run(async () => { const r = await api.createAccessToken(newTokenName.trim()); newTokenName = ''; await loadTokens(); return r; }, 'Token created — copy it from the list')}>Create token</button>
				</footer>
			</div>

		{:else if section === 'video'}
			<hgroup><h1>Video</h1><p>What happens between the ingest and the viewers.</p></hgroup>

			<div class="card">
				<header><h2>Output</h2><p>Passthrough relays the incoming stream untouched — the normal case when the sender controls its own encode.</p></header>
				{#each variants as v, i}
					<div class="variant">
						<div class="field"><label for={'vn' + i}>Name</label><input id={'vn' + i} bind:value={v.name} /></div>
						<label class="switch"><input type="checkbox" bind:checked={v.videoPassthrough} /><span class="track"></span> Video passthrough</label>
						<label class="switch"><input type="checkbox" bind:checked={v.audioPassthrough} /><span class="track"></span> Audio passthrough</label>
						{#if !v.videoPassthrough}
							<div class="field compact"><label for={'vb' + i}>kbps</label><input id={'vb' + i} type="number" bind:value={v.videoBitrate} /></div>
							<div class="field compact"><label for={'vf' + i}>fps</label><input id={'vf' + i} type="number" bind:value={v.framerate} /></div>
						{/if}
						<button class="ghost tiny danger-text remove" onclick={() => (variants = variants.filter((_, j) => j !== i))}>Remove</button>
					</div>
				{/each}
				<footer>
					<button class="ghost" onclick={() => (variants = [...variants, { name: 'passthrough', videoPassthrough: true, audioPassthrough: true, cpuUsageLevel: 2 }])}>Add a variant</button>
					<button onclick={() => run(() => api.setOutputVariants(variants))}>Save</button>
				</footer>
			</div>

			<div class="card">
				<header><h2>Delivery</h2></header>
				<div class="field-row">
					<div class="field">
						<label for="segfmt">Segment container</label>
						<select id="segfmt" bind:value={segmentFormat} onchange={() => run(() => api.setSegmentFormat(segmentFormat))}>
							<option value="auto">Auto — fMP4 when the codec needs it</option>
							<option value="ts">mpegts — always</option>
							<option value="fmp4">fMP4 — always</option>
						</select>
					</div>
					<div class="field">
						<label for="latency">Latency</label>
						<select id="latency" bind:value={latency} onchange={() => run(() => api.setConfigValue('video/streamlatencylevel', Number(latency)))}>
							<option value={0}>Lowest</option><option value={1}>Low</option>
							<option value={2}>Default</option><option value={3}>High</option>
							<option value={4}>Highest buffer</option>
						</select>
					</div>
				</div>
			</div>

		{:else if section === 'chat'}
			<hgroup><h1>Chat</h1><p>The room's rules, and the tools to keep it kind.</p></hgroup>

			<div class="card">
				<header><h2>Room</h2></header>
				<div class="field-row">
					<label class="switch"><input type="checkbox" bind:checked={chatEnabled} onchange={() => run(() => api.setConfigValue('chat/disable', !chatEnabled))} /><span class="track"></span> Chat enabled</label>
					<label class="switch"><input type="checkbox" bind:checked={joinMessages} onchange={() => run(() => api.setConfigValue('chat/joinmessagesenabled', joinMessages))} /><span class="track"></span> Join messages</label>
				</div>
				<div class="field">
					<label for="welcome">Welcome message</label>
					<input id="welcome" bind:value={welcomeMessage} placeholder="Shown to everyone who joins" />
				</div>
				<footer><button onclick={() => run(() => api.setConfigValue('welcomemessage', welcomeMessage))}>Save</button></footer>
			</div>

			<div class="card">
				<header><h2>Names</h2><p>Chat names are first-come, unique, and reserved while their owner keeps visiting.</p></header>
				<div class="field">
					<label for="forbidden">Forbidden names</label>
					<input id="forbidden" bind:value={forbiddenNames} placeholder="comma, separated" />
				</div>
				<div class="field compact">
					<label for="resdays">Reservation window (days · 0 = forever)</label>
					<input id="resdays" type="number" min="0" max="3650" bind:value={reservationDays} />
				</div>
				<footer>
					<button onclick={() => run(async () => { await api.setConfigValue('chat/forbiddenusernames', forbiddenNames.split(',').map((s) => s.trim()).filter(Boolean)); return api.setNameReservationDays(Number(reservationDays)); })}>Save</button>
				</footer>
			</div>

			<div class="card">
				<header><h2>Moderation</h2><p>The last hour of the room. Hidden messages stay hidden for everyone.</p></header>
				{#if messages.length === 0}<p class="empty">Nothing said yet.</p>{/if}
				<div class="modlist">
					{#each messages as m (m.id)}
						<div class="msgrow" class:hidden-msg={m.hiddenAt}>
							<span class="who">{m.user?.displayName ?? '—'}</span>
							<span class="body">{@html m.body}</span>
							<span class="actions">
								<button class="ghost tiny" onclick={() => run(async () => { const r = await api.setMessageVisibility([m.id], !!m.hiddenAt); m.hiddenAt = m.hiddenAt ? null : new Date().toISOString(); return r; }, m.hiddenAt ? 'Message restored' : 'Message hidden')}>{m.hiddenAt ? 'Unhide' : 'Hide'}</button>
								{#if m.user?.id}
									<button class="ghost tiny danger-text" onclick={() => run(() => api.setUserEnabled(m.user.id, false), 'User banned')}>Ban</button>
								{/if}
							</span>
						</div>
					{/each}
				</div>
				{#if disabledUsers.length}
					<header class="mt"><h2>Banned</h2></header>
					{#each disabledUsers as u (u.id)}
						<div class="msgrow">
							<span class="who">{u.displayName}</span>
							<span class="body"></span>
							<span class="actions"><button class="ghost tiny" onclick={() => run(async () => { const r = await api.setUserEnabled(u.id, true); disabledUsers = disabledUsers.filter((x) => x.id !== u.id); return r; }, 'User unbanned')}>Unban</button></span>
						</div>
					{/each}
				{/if}
			</div>

			<div class="card">
				<header><h2>IP bans</h2><p>Blocked addresses cannot connect to chat at all.</p></header>
				{#if ipBans.length === 0}<p class="empty">No addresses banned.</p>{/if}
				{#each ipBans as b (b.ipAddress)}
					<div class="msgrow">
						<span class="who mono-text">{b.ipAddress}</span>
						<span class="body dim">{b.createdAt ? new Date(b.createdAt).toLocaleDateString() : ''}</span>
						<span class="actions"><button class="ghost tiny" onclick={() => run(async () => { const r = await api.unbanIP(b.ipAddress); ipBans = ipBans.filter((x) => x.ipAddress !== b.ipAddress); return r; }, 'Address unbanned')}>Unban</button></span>
					</div>
				{/each}
				<div class="field-row">
					<div class="field compact"><label for="banip">Address</label><input id="banip" bind:value={newBanIP} placeholder="203.0.113.7" /></div>
				</div>
				<footer>
					<button disabled={!newBanIP.trim()} onclick={() => run(async () => { const r = await api.banIP(newBanIP.trim()); newBanIP = ''; loadModeration(); return r; }, 'Address banned')}>Ban address</button>
				</footer>
			</div>

		{:else if section === 'logs'}
			<hgroup><h1>Logs</h1><p>What the server has been saying. Refreshes while you watch.</p></hgroup>

			<div class="card wide">
				<header class="logbar">
					<div class="seg">
						<button class:on={logFilter === 'warnings'} onclick={() => (logFilter = 'warnings')}>Warnings</button>
						<button class:on={logFilter === 'all'} onclick={() => (logFilter = 'all')}>Everything</button>
					</div>
					<button class="ghost tiny" onclick={loadLogs}>Refresh</button>
				</header>
				{#if logs.length === 0}<p class="empty">Nothing logged{logFilter === 'warnings' ? ' — no warnings is good news' : ''}.</p>{/if}
				<div class="loglist">
					{#each logs as l, i (i)}
						<div class="logrow">
							<span class="ltime">{new Date(l.time).toLocaleTimeString()}</span>
							<span class="lvl {l.level}">{l.level}</span>
							<span class="lmsg">{l.message}</span>
						</div>
					{/each}
				</div>
			</div>

		{:else if section === 'settings'}
			<hgroup><h1>Settings</h1><p>The theater's identity, and the keys to its doors.</p></hgroup>

			<div class="card">
				<header><h2>Theater</h2></header>
				<div class="field">
					<label for="sname">Name</label>
					<input id="sname" bind:value={serverName} />
				</div>
				<div class="field">
					<label for="stitle">Stream title</label>
					<input id="stitle" bind:value={streamTitle} placeholder="Shown as “now playing” while live" />
				</div>
				<footer>
					<button onclick={() => run(async () => { await api.setConfigValue('name', serverName); return api.setConfigValue('streamtitle', streamTitle); })}>Save</button>
				</footer>
			</div>

			<div class="card">
				<header><h2>Room password</h2><p>Shared by everyone who watches. Changing it ends every session except yours — the whole room re-enters with the new key.</p></header>
				<div class="field">
					<label for="roompw">New room password</label>
					<input id="roompw" type="password" bind:value={newRoomPw} autocomplete="new-password" />
				</div>
				<footer>
					<button disabled={!newRoomPw} onclick={() => run(async () => { const r = await api.setRoomPassword(newRoomPw); newRoomPw = ''; return r; }, 'Room password changed — everyone else was signed out')}>Change</button>
				</footer>
			</div>

			<div class="card">
				<header><h2>Admin password</h2><p>Yours alone. At least 8 characters.</p></header>
				<div class="field">
					<label for="adminpw">New admin password</label>
					<input id="adminpw" type="password" bind:value={newAdminPw} autocomplete="new-password" />
				</div>
				<footer>
					<button disabled={newAdminPw.length < 8} onclick={() => run(async () => { const r = await api.setAdminPassword(newAdminPw); newAdminPw = ''; return r; }, 'Admin password changed')}>Change</button>
				</footer>
			</div>
		{/if}
	</main>

	<div class="toasts">
		{#each toasts as t (t.id)}
			<div class="toast" class:err={!t.ok}>{t.text}</div>
		{/each}
	</div>
</div>

<style>
	.admin {
		/* ops-blue accent: same family as the theater, different job */
		--accent: #6ba3f0;
		--radius: 12px;
		height: 100vh;
		display: flex;
		font-size: 13.5px;
	}

	/* ---------- nav ---------- */
	nav {
		width: 216px;
		flex: none;
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 20px 14px;
		border-right: 1px solid var(--border);
		background: color-mix(in srgb, var(--surface) 60%, transparent);
	}
	.brand { font-size: 13px; font-weight: 700; margin: 0 8px 18px; }
	.brand b { color: var(--accent); }
	.brand span { color: var(--muted); font-weight: 400; }
	nav button {
		text-align: left; background: none; border: 0; color: var(--muted);
		padding: 8px 12px; border-radius: 8px; cursor: pointer; font-size: 13.5px;
	}
	nav button:hover { color: var(--text); background: var(--surface-2); }
	nav button.on { color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, transparent); font-weight: 600; }
	.grow { flex: 1; }
	.back { color: var(--muted); font-size: 12px; text-decoration: none; padding: 8px 12px; }
	.back:hover { color: var(--accent); }

	/* ---------- main column ---------- */
	main { flex: 1; overflow-y: auto; padding: 30px 34px 80px; }
	hgroup { margin-bottom: 20px; }
	hgroup h1 { font-size: 19px; font-weight: 650; }
	hgroup p { color: var(--muted); font-size: 13px; margin-top: 3px; }

	/* ---------- cards ---------- */
	.card {
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		padding: 18px 20px 14px;
		margin-bottom: 16px;
		max-width: 640px;
	}
	.card header { margin-bottom: 14px; }
	.card header.mt { margin-top: 18px; }
	.card h2 { font-size: 11px; letter-spacing: 0.16em; text-transform: uppercase; color: var(--muted); }
	.card header p { color: var(--muted); font-size: 12.5px; margin-top: 5px; line-height: 1.45; }
	.card footer {
		display: flex; justify-content: flex-end; gap: 10px; align-items: center;
		margin-top: 14px; padding-top: 12px;
		border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
	}

	/* ---------- fields ---------- */
	.field { display: flex; flex-direction: column; gap: 6px; margin: 12px 0; }
	.field label { font-size: 12px; color: var(--muted); }
	.field input, .field select { width: 100%; }
	.field.compact { max-width: 240px; }
	.field-row { display: flex; gap: 26px; align-items: flex-end; flex-wrap: wrap; margin: 12px 0; }
	.field-row .field { flex: 1; min-width: 180px; margin: 0; }

	input, select {
		background: var(--surface-2);
		border: 1px solid var(--border);
		color: var(--text);
		border-radius: 8px;
		padding: 8px 11px;
		font-size: 13px;
	}
	input:focus, select:focus { outline: 0; border-color: var(--accent); }

	/* ---------- switches ---------- */
	.switch { display: flex; align-items: center; gap: 9px; color: var(--text); font-size: 13px; cursor: pointer; padding: 4px 0; }
	.switch input { position: absolute; opacity: 0; pointer-events: none; }
	.switch .track {
		width: 32px; height: 18px; border-radius: 99px; flex: none;
		background: var(--surface-2); border: 1px solid var(--border);
		position: relative; transition: background 0.15s, border-color 0.15s;
	}
	.switch .track::after {
		content: ''; position: absolute; top: 2px; left: 2px;
		width: 12px; height: 12px; border-radius: 50%;
		background: var(--muted); transition: transform 0.15s, background 0.15s;
	}
	.switch input:checked + .track { background: color-mix(in srgb, var(--accent) 30%, var(--surface-2)); border-color: var(--accent); }
	.switch input:checked + .track::after { transform: translateX(14px); background: var(--accent); }

	/* ---------- buttons ---------- */
	button {
		background: var(--accent); color: #101216; border: 0; border-radius: 8px;
		padding: 8px 16px; font-weight: 600; cursor: pointer; font-size: 13px;
	}
	button:disabled { opacity: 0.4; cursor: default; }
	button.ghost { background: transparent; color: var(--muted); border: 1px solid var(--border); font-weight: 500; }
	button.ghost:hover { color: var(--accent); border-color: var(--accent); }
	button.tiny { padding: 5px 11px; font-size: 12px; }
	button.danger { background: var(--danger); color: #16090a; }
	.danger-text:hover { color: var(--danger) !important; border-color: var(--danger) !important; }

	/* ---------- status ---------- */
	.statusline { display: flex; align-items: center; gap: 9px; font-size: 15px; }
	.statusline .sep { color: var(--muted); }
	.dot { width: 9px; height: 9px; border-radius: 50%; background: var(--muted); flex: none; }
	.dot.live { background: #5fc493; box-shadow: 0 0 10px #5fc493; }

	/* ---------- definition rows ---------- */
	dl { display: flex; flex-direction: column; gap: 2px; }
	dl div {
		display: flex; align-items: center; gap: 14px; padding: 7px 0;
		border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
	}
	dl div:last-child { border-bottom: 0; }
	dt { color: var(--muted); font-size: 12px; width: 74px; flex: none; }
	dd { flex: 1; min-width: 0; overflow-wrap: anywhere; }
	dl.mono dd { font-family: ui-monospace, monospace; font-size: 12.5px; }

	/* ---------- stream keys ---------- */
	.keyrow { display: flex; gap: 8px; align-items: center; margin: 8px 0; }
	.keyrow input.mono { font-family: ui-monospace, monospace; font-size: 12.5px; flex: 1.4; }
	.keyrow input.comment { flex: 1; }

	/* ---------- variants ---------- */
	.variant {
		display: flex; align-items: flex-end; gap: 18px; flex-wrap: wrap;
		border: 1px solid var(--border); border-radius: 10px;
		padding: 12px 14px; margin: 10px 0;
		background: color-mix(in srgb, var(--surface-2) 45%, transparent);
	}
	.variant .field { margin: 0; max-width: 170px; }
	.variant .switch { padding-bottom: 8px; }
	.variant .remove { margin-left: auto; margin-bottom: 6px; }

	/* ---------- moderation ---------- */
	.empty { color: var(--muted); font-size: 13px; }
	.modlist { max-height: 380px; overflow-y: auto; }
	.msgrow {
		display: flex; align-items: center; gap: 12px; padding: 8px 2px;
		border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
		font-size: 13px;
	}
	.msgrow:last-child { border-bottom: 0; }
	.msgrow .who { color: var(--accent); font-weight: 600; flex: none; width: 118px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.msgrow .body { flex: 1; min-width: 0; overflow-wrap: anywhere; }
	.msgrow .actions { flex: none; display: flex; gap: 6px; opacity: 0; transition: opacity 0.15s; }
	.msgrow:hover .actions { opacity: 1; }
	.msgrow.hidden-msg .body { opacity: 0.35; text-decoration: line-through; }
	.msgrow.hidden-msg .actions { opacity: 1; }

	/* ---------- stat tiles ---------- */
	.tiles { display: flex; gap: 12px; }
	.tile {
		flex: 1; display: flex; flex-direction: column; gap: 3px;
		background: color-mix(in srgb, var(--surface-2) 55%, transparent);
		border: 1px solid var(--border); border-radius: 10px;
		padding: 12px 16px;
	}
	.tile .num { font-size: 22px; font-weight: 650; font-variant-numeric: tabular-nums; }
	.tile .num small { font-size: 13px; color: var(--muted); font-weight: 400; }
	.tile .lab { font-size: 11px; letter-spacing: 0.1em; text-transform: uppercase; color: var(--muted); }

	/* ---------- logs ---------- */
	.card.wide { max-width: 980px; }
	.logbar { display: flex; align-items: center; justify-content: space-between; }
	.seg { display: flex; gap: 2px; background: var(--surface-2); border: 1px solid var(--border); border-radius: 8px; padding: 3px; }
	.seg button {
		background: transparent; color: var(--muted); font-weight: 500;
		padding: 5px 14px; border-radius: 6px; font-size: 12.5px;
	}
	.seg button.on { background: color-mix(in srgb, var(--accent) 16%, transparent); color: var(--accent); font-weight: 600; }
	.loglist { max-height: 64vh; overflow-y: auto; font-size: 12.5px; }
	.logrow {
		display: flex; gap: 12px; padding: 6px 2px; align-items: baseline;
		border-bottom: 1px solid color-mix(in srgb, var(--border) 40%, transparent);
	}
	.logrow:last-child { border-bottom: 0; }
	.ltime { color: var(--muted); font-variant-numeric: tabular-nums; flex: none; width: 74px; }
	.lvl {
		flex: none; width: 62px; text-align: center; font-size: 10.5px; font-weight: 700;
		text-transform: uppercase; letter-spacing: 0.06em; border-radius: 5px; padding: 2px 0;
		background: var(--surface-2); color: var(--muted);
	}
	.lvl.warning { background: color-mix(in srgb, #d9b06a 18%, transparent); color: #d9b06a; }
	.lvl.error, .lvl.fatal { background: color-mix(in srgb, var(--danger) 18%, transparent); color: var(--danger); }
	.lmsg { flex: 1; min-width: 0; overflow-wrap: anywhere; }
	.mono-text { font-family: ui-monospace, monospace; font-size: 12.5px; }
	.dim { color: var(--muted); font-size: 12px; }

	/* ---------- toasts ---------- */
	.toasts {
		position: fixed; bottom: 22px; right: 22px; z-index: 50;
		display: flex; flex-direction: column; gap: 8px; align-items: flex-end;
	}
	.toast {
		background: var(--surface-2); border: 1px solid var(--accent);
		color: var(--text); font-size: 12.5px;
		padding: 9px 16px; border-radius: 10px;
		box-shadow: 0 6px 24px -8px #000;
		animation: pop 0.18s ease;
	}
	.toast.err { border-color: var(--danger); color: var(--danger); }
	@keyframes pop { from { transform: translateY(6px); opacity: 0; } }

	/* ---------- mobile ---------- */
	@media (max-width: 760px) {
		.admin { flex-direction: column; }
		nav {
			width: 100%;
			flex-direction: row;
			align-items: center;
			overflow-x: auto;
			padding: 10px 12px;
			border-right: 0;
			border-bottom: 1px solid var(--border);
			gap: 2px;
		}
		.brand { margin: 0 10px 0 2px; white-space: nowrap; }
		nav button { white-space: nowrap; padding: 7px 10px; }
		.back { display: none; }
		.grow { display: none; }
		main { padding: 16px 14px 60px; }
		.tiles { flex-direction: column; }
	}
</style>
