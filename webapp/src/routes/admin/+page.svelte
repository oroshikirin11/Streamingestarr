<script>
	import '../../app.css';
	import { onMount } from 'svelte';
	import * as api from '$lib/admin/api.js';
	import Spark from '$lib/admin/Spark.svelte';

	let section = $state('status');
	let cfg = $state(null);
	let status = $state(null);
	let toasts = $state([]);

	// editable copies
	let serverName = $state('');
	let keys = $state([]);
	let latency = $state(2);
	let srtEnabled = $state(true);
	let srtPort = $state(9710);
	let srtPassphrase = $state('');
	let srtPassphraseSet = $state(false);
	let tcpIngestEnabled = $state(false);
	let tcpIngestPort = $state(9711);
	let tcpTlsMode = $state('off');
	let tcpTlsCert = $state('');
	let tcpTlsKey = $state('');
	let tcpTls = $state({ certOk: false, certError: '', certSubject: '', certNotAfter: '' });
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
	let viewersSeries = $state(null);
	let bitrateSeries = $state(null);
	let ingestStats = $state(null);
	let avSyncLatest = $state(null);
	let avSyncWorst = $state(0);
	let cadence = $state(null);
	let incidents = $state([]);
	let ingestEvents = $state([]);
	let tokens = $state([]);
	let newTokenName = $state('');
	let rooms = $state([]);
	let newRoomName = $state('');
	let roomSel = $state('main');
	let relayRTSPPort = $state(8554);
	let relayTranscodeFallback = $state(true);
	// The room whose page is open in the Rooms section; null is the list.
	let openRoomId = $state(null);
	const roomOpen = $derived(openRoomId ? roomsEdit.find((r) => r.id === openRoomId) ?? null : null);

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
		keys = (cfg.streamKeys ?? []).map((k) => ({ key: k.key ?? '', comment: k.comment ?? '' }));
		latency = cfg.videoSettings?.latencyLevel ?? 2;
		srtEnabled = cfg.srtServerEnabled ?? true;
		srtPort = cfg.srtServerPort ?? 9710;
		srtPassphraseSet = cfg.srtPassphraseSet ?? false;
		tcpIngestEnabled = cfg.tcpIngestEnabled ?? false;
		tcpIngestPort = cfg.tcpIngestPort ?? 9711;
		relayRTSPPort = cfg.relayRTSPPort ?? 8554;
		relayTranscodeFallback = cfg.relayTranscodeFallback !== false;
		tcpTls = cfg.tcpTls ?? { mode: 'off', certFile: '', keyFile: '', certOk: false, certError: '', certSubject: '', certNotAfter: '' };
		tcpTlsMode = tcpTls.mode ?? 'off';
		tcpTlsCert = tcpTls.certFile ?? '';
		tcpTlsKey = tcpTls.keyFile ?? '';
		reservationDays = cfg.chatNameReservationDays ?? 30;
		chatEnabled = !(cfg.chatDisabled ?? false);
		joinMessages = cfg.chatJoinMessagesEnabled ?? true;
		forbiddenNames = (cfg.forbiddenUsernames ?? []).join(', ');
	}

	async function refreshStatus() {
		try {
			status = await api.getAdminStatus(roomSel);
			if (section === 'status') {
				await loadRooms();
			}
			if (section === 'status') {
				hw = await api.getHardwareStats();
				// Last 4 hours of viewer counts — sampled every 2 minutes
				// server-side, so this stays a tiny payload.
				viewersSeries = await api.getViewersOverTime(Math.floor(Date.now() / 1000) - 4 * 3600);
				bitrateSeries = status?.broadcaster ? await api.getIngestBitrate(roomSel) : null;
				ingestStats = status?.broadcaster ? await api.getIngestStats(roomSel) : null;
				const av = status?.broadcaster ? await api.getAVSync(roomSel) : null;
				avSyncLatest = av?.length ? av[av.length - 1] : null;
				avSyncWorst = av?.length ? av.reduce((w, m) => (Math.abs(m.deltaMs) > Math.abs(w) ? m.deltaMs : w), 0) : 0;
				cadence = summarizeCadence(av);
				incidents = ((await api.getPlayerIncidents(roomSel).catch(() => [])) ?? []);
				ingestEvents = ((await api.getIngestEvents(roomSel).catch(() => [])) ?? []);
			}
		} catch {}
	}

	async function loadRooms() {
		try {
			const d = await api.getRooms();
			rooms = d.rooms ?? [];
			if (!rooms.some((r) => r.id === roomSel)) roomSel = 'main';
		} catch {}
	}

	// The editable copy the Rooms section works on — refreshed on entry and
	// after saves, NEVER by the 5s poll, so typing does not get clobbered.
	let roomsEdit = $state([]);
	async function loadRoomsEdit() {
		await loadRooms();
		roomsEdit = $state.snapshot(rooms).map((r) => ({
			...r,
			keys: (r.keys ?? []).map((k) => ({ key: k.key ?? '', comment: k.comment ?? '' })),
			title: r.title ?? '',
			welcomeMessage: r.welcomeMessage ?? '',
			latencyLevel: r.latencyLevel ?? -1,
			// A ladder or forced container stored by an earlier version:
			// shown as a notice with a reset, never edited.
			legacyOutput: (r.outputVariants ?? []).length > 0 || Boolean(r.segmentFormat),
			passphraseSet: Boolean(r.passphraseSet),
			passphrase: '',
			pauseVoteEnabled: r.pauseVoteEnabled !== false,
			shareIngest: Boolean(r.shareIngest),
			mode: r.mode ?? 'theater',
			relayProtocols: r.relayProtocols ?? ['rtsp'],
			relayLinks: r.relayLinks ?? {},
			relayPlayers: r.relayPlayers ?? 0,
			relayEncoding: r.relayEncoding ?? '',
			roomPasswordSet: Boolean(r.roomPasswordSet),
			roomPassword: '',
			lockTheater: Boolean(r.lockTheater),
			lockRelay: Boolean(r.lockRelay)
		}));
	}

	function saveRoom(r) {
		return run(async () => {
			const rn = await api.renameRoom(r.id, r.name.trim());
			if (rn?.success === false) return rn;
			// Container and ladder are never sent: the save resets a room to
			// the server's passthrough and Auto container.
			const cf = await api.setRoomConfig(r.id, {
				title: r.title,
				welcomeMessage: r.welcomeMessage,
				latencyLevel: Number(r.latencyLevel),
				shareIngest: r.shareIngest
			});
			if (cf?.success === false) return cf;
			r.legacyOutput = false;
			if (!r.isDefault) return api.setRoomKeys(r.id, r.keys.filter((k) => k.key));
			return cf;
		}, 'Room saved');
	}

	// The open-stage switch saves on its own; the config endpoint replaces
	// the whole config, so it rides with the card's current fields.
	function saveShareIngest(r) {
		run(async () => {
			const res = await api.setRoomConfig(r.id, {
				title: r.title,
				welcomeMessage: r.welcomeMessage,
				latencyLevel: Number(r.latencyLevel),
				shareIngest: r.shareIngest
			});
			if (res?.success === false) r.shareIngest = !r.shareIngest;
			return res;
		}, r.shareIngest ? 'Stage opened — the room page now shows the ingest address and keys' : 'Stage closed — credentials are admin-only again');
	}

	// Mode and relay links save on their own — the switch is the action.
	const MODES = [['theater', 'Theater'], ['relay', 'Relay'], ['both', 'Both']];
	const PROTOCOLS = [
		['rtsp', 'RTSP', 'PC — lowest latency, over TCP'],
		['ts', 'MPEG-TS over HTTP', 'Quest'],
		['hls', 'HLS', 'browsers and everything else']
	];
	async function saveMode(r, mode, protocols) {
		const before = { mode: r.mode, relayProtocols: r.relayProtocols };
		r.mode = mode;
		r.relayProtocols = protocols;
		const res = await run(() => api.setRoomMode(r.id, mode, protocols));
		if (res?.success === false) { r.mode = before.mode; r.relayProtocols = before.relayProtocols; return; }
		await refreshRoomLinks(r);
	}
	function toggleProtocol(r, p) {
		const has = r.relayProtocols.includes(p);
		const next = has ? r.relayProtocols.filter((x) => x !== p) : [...r.relayProtocols, p];
		if (!next.length) { toast('Keep at least one link kind', false); return; }
		saveMode(r, r.mode, next);
	}
	async function refreshRoomLinks(r) {
		try {
			const d = await api.getRooms();
			const fresh = (d.rooms ?? []).find((x) => x.id === r.id);
			if (fresh) { r.relayLinks = fresh.relayLinks ?? {}; r.relayProtocols = fresh.relayProtocols ?? r.relayProtocols; r.relayPlayers = fresh.relayPlayers ?? 0; r.relayEncoding = fresh.relayEncoding ?? ''; }
		} catch {}
	}
	const linkLabel = { rtsp: 'PC · RTSP', ts: 'Quest · MPEG-TS', hls: 'Other · HLS' };
	const dimToken = (url) => {
		const m = String(url).match(/^(.*\/)([0-9a-f]{16,})(\.[a-z0-9]+)?$/i);
		return m ? { head: m[1], token: m[2], tail: m[3] ?? '' } : { head: url, token: '', tail: '' };
	};

	// The server-wide ladder and container from an earlier version — the
	// Video tab is gone, but a stored transcode must never hide.
	const legacyLadder = $derived.by(() => {
		const v = cfg?.videoSettings?.videoQualityVariants ?? [];
		return v.length > 1 || v.some((x) => !x.videoPassthrough || !x.audioPassthrough) ? v.length : 0;
	});
	const segmentForced = $derived((cfg?.videoSegmentFormat ?? 'auto') !== 'auto' ? cfg.videoSegmentFormat : '');
	const PASSTHROUGH = [{ name: 'passthrough', videoPassthrough: true, audioPassthrough: true, cpuUsageLevel: 2 }];
	const roomStatus = (id) => rooms.find((r) => r.id === id);

	// The rubberband verdict, from the segment ledger. Each measured step
	// spans the SAME segments in two clocks: how far the MEDIA advanced
	// (first-PTS delta) and how much WALL time passed while those segments
	// were written. Healthy live: the two match, whatever the segment
	// sizes. Wall ahead of media = the feed arrived late (sender starving
	// or the uplink). Media ahead of wall = a burst/catch-up. Negative
	// media = the source timeline stepped backwards (a seam).
	function summarizeCadence(av) {
		const per = (av ?? []).filter((m) => m.stepSegments > 0);
		if (per.length < 4) return null;
		const med = (xs) => {
			const a = [...xs].sort((x, y) => x - y);
			return a[Math.floor(a.length / 2)];
		};
		// Pairing subtlety: a segment's wall timestamp marks its END (when
		// it was written), its PTS marks its START. So the wall step of
		// segment N spans the same real span as the PTS step of segment
		// N+1 — pairing them makes the lag exact even when segment
		// durations alternate (keyframe-aligned cutting does that).
		let late = 0, bursts = 0, worstLateMs = 0;
		for (let i = 0; i < per.length - 1; i++) {
			if (per[i].stepSegments !== 1 || per[i + 1].stepSegments !== 1) continue;
			const lag = per[i].wallStepMs - per[i + 1].ptsStepMs;
			if (lag > 1500) { late++; worstLateMs = Math.max(worstLateMs, lag); }
			else if (lag < -1500) bursts++;
		}
		const seams = per.filter((s) => s.ptsStepMs < 0).length;
		return {
			medPts: Math.round(med(per.map((s) => s.ptsStepMs / s.stepSegments))),
			worstLateMs: Math.round(worstLateMs),
			late,
			seams,
			bursts
		};
	}

	// The feed's own testimony: gaps in the last hour, and whether it is
	// silent at this very moment.
	const feedSummary = $derived.by(() => {
		const cutoff = Date.now() - 60 * 60 * 1000;
		const gaps = (ingestEvents ?? []).filter(
			(e) => e.type === 'feed-gap' && new Date(e.time).getTime() > cutoff
		);
		const drops = (ingestEvents ?? []).filter(
			(e) => e.type === 'queue-drop' && new Date(e.time).getTime() > cutoff
		);
		if (!gaps.length && !drops.length) return null;
		const worst = gaps.reduce((w, g) => Math.max(w, g.durMs ?? 0), 0);
		const last = gaps[gaps.length - 1] ?? drops[drops.length - 1];
		return { gaps: gaps.length, worstMs: worst, drops: drops.length, last };
	});

	const incidentSummary = $derived.by(() => {
		const cutoff = Date.now() - 10 * 60 * 1000;
		const recent = (incidents ?? []).filter((i) => new Date(i.time).getTime() > cutoff);
		if (!recent.length) return null;
		const counts = {};
		const clients = new Set();
		for (const i of recent) {
			counts[i.type] = (counts[i.type] ?? 0) + 1;
			clients.add(i.client);
		}
		return { counts, clients: clients.size, last: recent[recent.length - 1], total: recent.length };
	});

	const latest = (series) => {
		const v = series?.[series.length - 1]?.value;
		return typeof v === 'number' ? Math.round(v) : null;
	};
	const kbps = (n) => (n ? `${n} kbps` : null);

	onMount(() => {
		loadConfig().catch(() => {});
		refreshStatus();
		const t = setInterval(refreshStatus, 5000);
		// #section or #rooms/<id>: a room page survives a reload and the
		// back button walks between list and page.
		const fromHash = () => {
			const [s, id] = location.hash.slice(1).split('/');
			if (s && SECTIONS.some(([k]) => k === s)) {
				if (section !== s) pick(s, false);
				openRoomId = s === 'rooms' && id ? id : null;
			}
		};
		fromHash();
		window.addEventListener('hashchange', fromHash);
		return () => { clearInterval(t); window.removeEventListener('hashchange', fromHash); };
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

	function pick(s, setHash = true) {
		section = s;
		if (s !== 'rooms') openRoomId = null;
		if (setHash && location.hash !== '#' + s) history.replaceState(null, '', '#' + s);
		if (s === 'chat') loadModeration();
		if (s === 'logs') loadLogs();
		if (s === 'stream') loadTokens();
		if (s === 'rooms') loadRoomsEdit();
		if (s === 'status') loadRooms();
	}
	function openRoomPage(id) {
		openRoomId = id;
		history.pushState(null, '', '#rooms/' + id);
	}
	function closeRoomPage() {
		openRoomId = null;
		history.pushState(null, '', '#rooms');
	}

	$effect(() => {
		if (section !== 'logs') return;
		logFilter;
		loadLogs();
		const t = setInterval(loadLogs, 10000);
		return () => clearInterval(t);
	});

	const host = typeof location !== 'undefined' ? location.hostname : 'localhost';
	// Bare addresses — the stream key lives in its own card with its own
	// Copy button; senders take address and key as separate fields anyway.
	const rtmpURL = $derived(`rtmp://${host}:${cfg?.rtmpServerPort ?? 1935}`);
	const srtURL = $derived(`srt://${host}:${srtPort}`);
	const tcpURL = $derived(`tcp://${host}:${tcpIngestPort}`);
	const tcpTlsExpired = $derived(!!tcpTls.certNotAfter && new Date(tcpTls.certNotAfter) < new Date());
	const tcpTlsDate = $derived(tcpTls.certNotAfter ? new Date(tcpTls.certNotAfter).toLocaleDateString() : '');
	// The certificate browser: what the container sees under a path, so
	// the admin picks the files instead of guessing what a mount produced.
	let browse = $state(null); // { target: 'cert' | 'key', path, parent, entries, error, loading }
	const dirOf = (p) => { const i = p.lastIndexOf('/'); return i > 0 ? p.slice(0, i) : ''; };
	async function openBrowse(target) {
		if (browse?.target === target) { browse = null; return; }
		browse = { target, path: '', parent: '', entries: [], error: '', loading: true };
		await browseTo(dirOf((target === 'cert' ? tcpTlsCert : tcpTlsKey).trim()));
	}
	async function browseTo(path) {
		if (!browse) return;
		browse.loading = true;
		try {
			const r = await api.browseTCPIngestTLS(path);
			browse = { ...browse, path: r.path, parent: r.parent, entries: r.entries ?? [], error: r.error ?? '', loading: false };
		} catch {
			browse = { ...browse, error: 'The receiver did not answer.', loading: false };
		}
	}
	function pickFile(e) {
		if (!browse) return;
		if (browse.target === 'cert') {
			tcpTlsCert = e.path;
			// A sibling key with the same stem fills an empty key field.
			const stem = e.name.replace(/\.(crt|pem|cer)$/i, '');
			const key = browse.entries.find((x) => !x.dir && x.name !== e.name && /\.(key|pem)$/i.test(x.name) && x.name.replace(/\.(key|pem)$/i, '') === stem);
			if (key && !tcpTlsKey.trim()) tcpTlsKey = key.path;
		} else {
			tcpTlsKey = e.path;
		}
		browse = null;
	}
	async function saveTcpTls() {
		const r = await run(() => api.setTCPIngestTLS(tcpTlsMode, tcpTlsCert.trim(), tcpTlsKey.trim()));
		if (r?.success !== false) await loadConfig();
	}

	// "passthrough (h264) · 3 players" for the status card and the room page.
	function relayLine(rl) {
		if (!rl) return '';
		const enc = rl.encoding === 'transcode' ? `re-encoding ${(rl.sourceVideo || 'the source').toUpperCase()} to H.264`
			: rl.encoding === 'passthrough-foreign' ? `${(rl.sourceVideo || 'source').toUpperCase()} passed through — PC players that take only H.264 will not play it`
			: rl.encoding === 'passthrough' ? `passthrough (${(rl.sourceVideo || 'h264').toUpperCase()})`
			: 'waiting for a stream';
		const players = rl.players === 1 ? '1 player' : `${rl.players ?? 0} players`;
		return `${enc} · ${players}${rl.rtspListening === false ? ' · RTSP port not bound' : ''}`;
	}

	// "Paused by viewer vote since 21:14" for the status card.
	const pausedSince = $derived.by(() => {
		const pv = status?.pauseVote;
		if (!pv?.paused) return null;
		const who = pv.pausedBy === 'viewers' ? 'Paused by viewer vote' : pv.pausedBy === 'host' ? 'Paused by the host' : 'Paused';
		if (!pv.pausedAt) return who;
		return `${who} since ${new Date(pv.pausedAt * 1000).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}`;
	});

	const uptime = $derived.by(() => {
		const t = status?.broadcaster?.time;
		if (!t) return null;
		const mins = Math.floor((Date.now() - new Date(t).getTime()) / 60000);
		return mins < 60 ? `${mins}m` : `${Math.floor(mins / 60)}h ${mins % 60}m`;
	});

	const SECTIONS = [
		['status', 'Status'],
		['rooms', 'Rooms'],
		['stream', 'Stream'],
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

			{#if rooms.length > 1}
				<div class="roompick">
					{#each rooms as r (r.id)}
						<button class="ghost tiny" class:on={roomSel === r.id} onclick={() => { roomSel = r.id; refreshStatus(); }}>
							<span class="dot" class:live={r.online}></span>{r.name}
						</button>
					{/each}
				</div>
			{/if}

			<div class="card">
				<div class="statusline">
					<span class="dot" class:live={status?.online}></span>
					{#if status?.online}
						<b>Live</b><span class="sep">·</span>{status.viewerCount ?? 0} watching{#if uptime}<span class="sep">·</span>ingest up {uptime}{/if}{#if pausedSince}<span class="sep">·</span><b>{pausedSince}</b>{/if}
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
						{#if status?.relay?.mode && status.relay.mode !== 'theater'}
							<div><dt>Relay</dt><dd>{relayLine(status.relay)}</dd></div>
						{/if}
						{#if pausedSince || status?.pauseVote?.advertised}
							<div><dt>Playback</dt><dd>{pausedSince ?? (status?.pauseVote?.pending ? (status.pauseVote.pending === 'pause' ? 'Pausing…' : 'Resuming…') : 'Playing')}<span class="sep">·</span>{status?.pauseVote?.controlConnected ? 'sender control connected' : 'sender control not connected'}{#if !status?.pauseVote?.enabled}<span class="sep">·</span>pause votes off for this room{/if}</dd></div>
						{/if}
						<div><dt>Encoder</dt><dd>{d.encoder || '—'}</dd></div>
						<div><dt>Video</dt><dd>{[d.videoCodec, d.width ? `${d.width}×${d.height}` : null, d.framerate ? `${d.framerate} fps` : null, kbps(d.videoBitrate)].filter(Boolean).join(' · ') || '—'}</dd></div>
						<div><dt>Audio</dt><dd>{d.videoOnly ? 'none — video only' : [d.audioCodec, kbps(d.audioBitrate)].filter(Boolean).join(' · ') || '—'}</dd></div>
					</dl>
					{#if bitrateSeries?.length > 1}
						<p class="spark-label">inbound bitrate — live, 5s samples</p>
						<Spark data={bitrateSeries} unit=" kbps" height={64} />
					{/if}
					{#if ingestStats?.connected}
						<p class="hint" class:danger-text={ingestStats.srtPktRecvDrop > 0 || ingestStats.bufferDroppedBytes > 0}>
							Receive health — SRT drops: {ingestStats.srtPktRecvDrop}
							· link loss: {ingestStats.srtPktRecvLoss} (recovered {ingestStats.srtPktRecvRetrans})
							· buffer drops: {ingestStats.bufferDroppedBytes > 0 ? `${Math.round(ingestStats.bufferDroppedBytes / 1024)} KB` : '0'}
						</p>
					{/if}
					{#if ingestStats?.silentMs > 1000}
						<p class="hint danger-text">
							FEED SILENT for {(ingestStats.silentMs / 1000).toFixed(1)}s — nothing is arriving on the socket right now (uplink or sender).
						</p>
					{/if}
					{#if feedSummary}
						<p class="hint danger-text">
							Feed events (1 h) — {feedSummary.gaps} gap{feedSummary.gaps === 1 ? '' : 's'}, worst {(feedSummary.worstMs / 1000).toFixed(1)}s
							{#if feedSummary.drops}&nbsp;· {feedSummary.drops} buffer-drop marks{/if}
							· last at {new Date(feedSummary.last.time).toLocaleTimeString()}
							— the feed itself starved; check the sender's pacing and the uplink.
						</p>
					{:else if status?.online}
						<p class="hint">Feed events (1 h) — none: every byte arrived on time.</p>
					{/if}
					{#if cadence}
						<p class="hint" class:danger-text={cadence.seams > 0 || cadence.late > 0}>
							Segment cadence — pacing ~{cadence.medPts} ms/segment
							{#if cadence.late > 0}&nbsp;· {cadence.late} late arrivals, worst {(cadence.worstLateMs / 1000).toFixed(1)}s (the feed fell behind — sender starving or uplink){/if}
							{#if cadence.bursts > 0}&nbsp;· {cadence.bursts} catch-up bursts{/if}
							{#if cadence.seams > 0}&nbsp;· {cadence.seams} timeline seams (source timestamps stepped backwards){/if}
							{#if !cadence.late && !cadence.seams && !cadence.bursts}&nbsp;· steady — arrival matches the media clock{/if}
						</p>
					{/if}
					{#if incidentSummary}
						<p class="hint danger-text">
							Viewer incidents (10 min) — {Object.entries(incidentSummary.counts).map(([k, v]) => `${k}: ${v}`).join(' · ')}
							· {incidentSummary.clients} viewer{incidentSummary.clients === 1 ? '' : 's'}
							· last: {incidentSummary.last.type}{incidentSummary.last.magMs ? ` ${Math.round(incidentSummary.last.magMs)}ms` : ''} at {new Date(incidentSummary.last.time).toLocaleTimeString()}
						</p>
					{:else if status?.online}
						<p class="hint">Viewer incidents (10 min) — none reported. Rubberbanding without incidents here means the viewer never told us; with incidents but a clean cadence line, blame that viewer's network.</p>
					{/if}
					{#if avSyncLatest}
						<p class="hint" class:danger-text={Math.abs(avSyncWorst) > 45}>
							A/V offset per segment — now: {avSyncLatest.deltaMs > 0 ? '+' : ''}{Math.round(avSyncLatest.deltaMs)} ms
							· worst recent: {avSyncWorst > 0 ? '+' : ''}{Math.round(avSyncWorst)} ms
							(audio {avSyncLatest.deltaMs >= 0 ? 'behind' : 'ahead of'} video; skew in the SEGMENTS means the sender, clean segments with drifting playback means the player)
						</p>
					{/if}
					<footer>
						<button class="danger" onclick={() => run(() => api.disconnectStream(roomSel), 'Stream disconnected')}>Disconnect the stream</button>
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
				{#if viewersSeries?.length > 1}
					<p class="spark-label">last 4 hours · all rooms</p>
					<Spark data={viewersSeries} height={72} />
				{/if}
			</div>

			{#if hw}
				<div class="card">
					<header><h2>Server health</h2></header>
					<div class="tiles">
						<div class="tile">
							<span class="num">{latest(hw.cpu) ?? '—'}<small>%</small></span><span class="lab">cpu</span>
							<Spark data={hw.cpu} unit="%" max={100} height={44} />
						</div>
						<div class="tile">
							<span class="num">{latest(hw.memory) ?? '—'}<small>%</small></span><span class="lab">memory</span>
							<Spark data={hw.memory} unit="%" max={100} height={44} />
						</div>
						<div class="tile">
							<span class="num">{latest(hw.disk) ?? '—'}<small>%</small></span><span class="lab">disk</span>
							<Spark data={hw.disk} unit="%" max={100} height={44} />
						</div>
					</div>
					{#if status?.health?.message}
						<p class="hint">{status.health.message}</p>
					{/if}
				</div>
			{/if}

		{:else if section === 'rooms'}
			{#if !roomOpen}
				<hgroup><h1>Rooms</h1><p>Independent theaters on the same ingest ports — the stream key decides which room a broadcast lands in. Open a room to set it up.</p></hgroup>

				<div class="roomlist">
					{#each roomsEdit as r (r.id)}
						<button class="roomrow" onclick={() => openRoomPage(r.id)}>
							<span class="dot" class:live={roomStatus(r.id)?.online}></span>
							<span class="rname">{r.name}</span>
							<span class="rpath mono-text">/t/{r.id}</span>
							{#if r.mode !== 'theater'}<span class="badge">{r.mode === 'relay' ? 'relay' : 'theater + relay'}</span>{/if}
							<span class="rstate">{#if roomStatus(r.id)?.online}live · {roomStatus(r.id)?.viewerCount} watching{:else}resting{/if}</span>
							<span class="chev" aria-hidden="true">›</span>
						</button>
					{/each}
				</div>

				<div class="card">
					<header><h2>New room</h2><p>Each room gets its own theater page at /t/&lt;id&gt;, its own chat, its own keys, and its own configuration.</p></header>
					<div class="field-row">
						<div class="field"><label for="roomname">Room name</label><input id="roomname" bind:value={newRoomName} placeholder="Second Screen" /></div>
					</div>
					<footer>
						<button disabled={!newRoomName.trim()} onclick={() => run(async () => { const res = await api.createRoom(newRoomName.trim()); newRoomName = ''; await loadRoomsEdit(); return res; }, 'Room created — copy its key')}>Create room</button>
					</footer>
					<p class="hint">Point a sender at the SAME ingest address as always and use one of the room's keys — RTMP, SRT and TCP all route by it. Viewers pick a room at /t/&lt;id&gt;; the front page is the main room.</p>
				</div>
			{:else}
				{#each roomsEdit.filter((x) => x.id === openRoomId) as r (r.id)}
					<a class="backlink" href="#rooms" onclick={(e) => { e.preventDefault(); closeRoomPage(); }}>← Rooms</a>
					<hgroup>
						<h1><span class="dot big" class:live={roomStatus(r.id)?.online}></span> {r.name}</h1>
						<p>/t/{r.id}
							{#if roomStatus(r.id)?.online}&nbsp;· live · {roomStatus(r.id)?.viewerCount} watching{:else}&nbsp;· resting{/if}
							{#if r.isDefault}&nbsp;· the main room, the front page{/if}
						</p>
					</hgroup>

					<div class="card">
						<header><h2>Room</h2></header>
						<div class="field-row">
							<div class="field">
								<label for={'roomname-' + r.id}>Name</label>
								<input id={'roomname-' + r.id} bind:value={r.name} />
							</div>
							<div class="field">
								<label for={'roomtitle-' + r.id}>Stream title — empty inherits the default</label>
								<input id={'roomtitle-' + r.id} bind:value={r.title} placeholder="what this theater shows" />
							</div>
						</div>
						<div class="field">
							<label for={'roomwelcome-' + r.id}>Chat welcome — empty inherits the default</label>
							<input id={'roomwelcome-' + r.id} bind:value={r.welcomeMessage} placeholder="welcome to this room" />
						</div>
						<footer><button onclick={() => saveRoom(r)}>Save</button></footer>
					</div>

					<div class="card">
						<header><h2>Mode</h2><p>What this room is. A theater is the player page. A relay hands out links for an external player — VRChat's video players, VLC — and seats nobody. Both keeps the theater open with a relay chip in its header.</p></header>
						<div class="seg" role="radiogroup" aria-label="Room mode">
							{#each MODES as [id, label]}
								<button role="radio" aria-checked={r.mode === id} class:on={r.mode === id} onclick={() => r.mode !== id && saveMode(r, id, r.relayProtocols)}>{label}</button>
							{/each}
						</div>
						{#if r.mode !== 'theater'}
							<div class="protocols">
								{#each PROTOCOLS as [id, label, where]}
									<label class="switch">
										<input type="checkbox" checked={r.relayProtocols.includes(id)} onchange={() => toggleProtocol(r, id)} />
										<span class="track"></span> {label} <span class="where">— {where}</span>
									</label>
								{/each}
							</div>
							<div class="links">
								{#each r.relayProtocols as p (p)}
									{#if r.relayLinks[p]}
										{@const parts = dimToken(r.relayLinks[p])}
										<div class="linkrow">
											<span class="lk">{linkLabel[p]}</span>
											<code class="mono-text" title={r.relayLinks[p]}>{parts.head}<span class="tok">{parts.token}</span>{parts.tail}</code>
											<button class="ghost tiny" onclick={() => copy(r.relayLinks[p])}>Copy</button>
										</div>
									{/if}
								{/each}
							</div>
							<footer>
								<span class="players">{r.relayPlayers === 1 ? '1 player connected' : `${r.relayPlayers} players connected`}{#if r.relayEncoding}&nbsp;· {r.relayEncoding === 'transcode' ? 're-encoding to H.264 here' : r.relayEncoding === 'passthrough-foreign' ? 'source is not H.264, passed through' : 'passthrough'}{/if}</span>
								<button class="ghost danger-text" onclick={() => { if (confirm('New links? Every old link stops working and connected players are dropped.')) run(async () => { const res = await api.newRelayToken(r.id); await refreshRoomLinks(r); return res; }); }}>New links</button>
							</footer>
							<p class="hint">Links are shown to viewers on the room's page — behind the room password when <b>Access</b> says so. The RTSP port is set under <b>Stream</b>.</p>
						{/if}
					</div>

					<div class="card">
						<header><h2>Access</h2><p>The site password is the door for everyone. This room can ask for a second one — for its theater, its relay links, either or neither.</p></header>
						<div class="field-row">
							<div class="field">
								<label for={'roomrpw-' + r.id}>Room password</label>
								<input id={'roomrpw-' + r.id} type="password" bind:value={r.roomPassword} autocomplete="new-password"
									placeholder={r.roomPasswordSet ? 'set — type a new one to replace it' : 'none'} />
							</div>
							<div class="field compact">
								<label>&nbsp;</label>
								<div style="display:flex; gap:6px">
									<button class="ghost" disabled={!r.roomPassword.trim()} onclick={() => run(async () => { const res = await api.setRoomPassword(r.id, r.roomPassword.trim()); if (res?.success !== false) { r.roomPassword = ''; r.roomPasswordSet = true; } return res; })}>Set</button>
									{#if r.roomPasswordSet}<button class="ghost danger-text" onclick={() => run(async () => { const res = await api.setRoomPassword(r.id, ''); if (res?.success !== false) { r.roomPasswordSet = false; r.lockTheater = false; r.lockRelay = false; } return res; })}>Remove</button>{/if}
								</div>
							</div>
						</div>
						<div class="locks" class:off={!r.roomPasswordSet}>
							<label class="switch">
								<input type="checkbox" bind:checked={r.lockTheater} disabled={!r.roomPasswordSet} onchange={() => run(async () => { const res = await api.setRoomLocks(r.id, r.lockTheater, r.lockRelay); if (res?.success === false) r.lockTheater = !r.lockTheater; return res; }, r.lockTheater ? 'The theater asks for the room password' : 'The theater is open to everyone at the door')} />
								<span class="track"></span> Required to enter the theater
							</label>
							<label class="switch">
								<input type="checkbox" bind:checked={r.lockRelay} disabled={!r.roomPasswordSet} onchange={() => run(async () => { const res = await api.setRoomLocks(r.id, r.lockTheater, r.lockRelay); if (res?.success === false) r.lockRelay = !r.lockRelay; return res; }, r.lockRelay ? 'The relay links ask for the room password' : 'The relay links are open to everyone at the door')} />
								<span class="track"></span> Required to see the relay links
							</label>
							{#if !r.roomPasswordSet}<p class="hint">Set a room password first.</p>{/if}
						</div>
						<p class="hint">Changing the password asks everyone again. Admins are never asked.</p>
						<div class="field-row">
							<div class="field">
								<label for={'roompass-' + r.id}>SRT passphrase</label>
								<input id={'roompass-' + r.id} type="password" bind:value={r.passphrase} autocomplete="new-password"
									placeholder={r.passphraseSet ? 'set — type a new one to replace it' : 'none — the global SRT passphrase applies'} />
							</div>
							<div class="field compact">
								<label>&nbsp;</label>
								<div style="display:flex; gap:6px">
									<button class="ghost" disabled={!r.passphrase.trim()} onclick={() => run(async () => { const res = await api.setRoomPassphrase(r.id, r.passphrase.trim()); if (res?.success !== false) { r.passphrase = ''; r.passphraseSet = true; } return res; }, 'Passphrase set')}>Set</button>
									{#if r.passphraseSet}<button class="ghost danger-text" onclick={() => run(async () => { const res = await api.setRoomPassphrase(r.id, ''); if (res?.success !== false) r.passphraseSet = false; return res; }, 'Passphrase cleared')}>Clear</button>{/if}
								</div>
							</div>
						</div>
						<p class="hint">Senders that open this room over SRT must use the passphrase as the encryption key; it replaces the global SRT passphrase for this room. 10 to 79 characters, no spaces. Applies to the next connection.</p>
					</div>

					<div class="card">
						<header><h2>Playback</h2><p>The stream is relayed as it arrives. Latency is the cushion viewers keep; it applies to the room's next broadcast.</p></header>
						<div class="field compact">
							<label for={'roomlat-' + r.id}>Latency</label>
							<select id={'roomlat-' + r.id} bind:value={r.latencyLevel}>
								<option value={-1}>Server default</option>
								<option value={0}>Lowest</option><option value={1}>Low</option>
								<option value={2}>Default</option><option value={3}>High</option>
								<option value={4}>Highest buffer</option>
							</select>
						</div>
						{#if r.mode !== 'theater' && Number(r.latencyLevel) === 0}
							<p class="hint danger-text">Lowest latency starves the relay: the feed that serves the relay links comes from the same process, and at this level external players stutter. Default is the right setting for a relay room.</p>
						{:else if r.mode !== 'theater'}
							<p class="hint">Relay rooms do best at Default. Lowest starves the relay feed and external players stutter.</p>
						{/if}
						{#if r.legacyOutput}
							<p class="hint danger-text">This room still carries a transcode ladder or a forced segment container from an earlier version. Saving resets it to passthrough and the Auto container.</p>
						{/if}
						<label class="switch">
							<input type="checkbox" bind:checked={r.pauseVoteEnabled} onchange={() => run(async () => { const res = await api.setRoomPauseVote(r.id, r.pauseVoteEnabled); if (res?.success === false) r.pauseVoteEnabled = !r.pauseVoteEnabled; return res; }, r.pauseVoteEnabled ? 'Viewers may vote to pause' : 'Pause votes off')} />
							<span class="track"></span> Viewers may vote to pause — half the room pauses or resumes the broadcast, when the sender allows it
						</label>
						<footer><button onclick={() => saveRoom(r)}>Save</button></footer>
					</div>

					<div class="card">
						<header><h2>Ingest</h2><p>{#if r.isDefault}The main room's stream keys are the list in <b>Stream</b>.{:else}A sender opens this room with one of these keys on the shared ingest address — RTMP, SRT and TCP all route by it.{/if}</p></header>
						{#if !r.isDefault}
							{#each r.keys as k, i}
								<div class="keyrow">
									<input class="mono" bind:value={k.key} placeholder="key" />
									<input class="comment" bind:value={k.comment} placeholder="comment" />
									<button class="ghost tiny" onclick={() => copy(k.key)}>Copy</button>
									<button class="ghost tiny" onclick={() => (k.key = randomKey())}>Randomize</button>
									<button class="ghost tiny danger-text" onclick={() => (r.keys = r.keys.filter((_, j) => j !== i))}>Remove</button>
								</div>
							{/each}
						{/if}
						<label class="switch">
							<input type="checkbox" bind:checked={r.shareIngest} onchange={() => saveShareIngest(r)} />
							<span class="track"></span> Open stage — the room's page shows the ingest address and stream keys to everyone in it (behind the room's password, if set), so anyone may broadcast here
						</label>
						{#if r.shareIngest}
							<p class="hint">To revoke access later: {#if r.isDefault}rotate the keys in <b>Stream</b>{:else}randomize or remove the keys above{/if}, or switch the stage off.</p>
						{/if}
						{#if !r.isDefault}
							<footer>
								<button class="ghost" onclick={() => (r.keys = [...r.keys, { key: randomKey(), comment: '' }])}>Add a key</button>
								<button onclick={() => saveRoom(r)}>Save</button>
							</footer>
						{/if}
					</div>

					{#if !r.isDefault}
						<div class="card">
							<header><h2>Delete</h2><p>Removes the room, its keys and its chat. A live broadcast in it ends immediately.</p></header>
							<footer>
								<button class="ghost danger-text" onclick={() => { if (confirm(`Delete the room “${r.name}”? A live broadcast in it ends immediately.`)) run(async () => { const res = await api.deleteRoom(r.id); closeRoomPage(); await loadRoomsEdit(); return res; }, 'Room deleted'); }}>Delete room</button>
							</footer>
						</div>
					{/if}
				{/each}
			{/if}

		{:else if section === 'stream'}
			<hgroup><h1>Stream</h1><p>Where your source connects, and the keys that open the door.</p></hgroup>

			<div class="card">
				<header><h2>Ingest addresses</h2><p>Point your source at either. The stream key doubles as the SRT streamid.</p></header>
				<dl class="mono">
					<div><dt>RTMP</dt><dd>{rtmpURL}</dd><button class="ghost tiny" onclick={() => copy(rtmpURL)}>Copy</button></div>
					<div><dt>SRT</dt><dd>{srtURL}</dd><button class="ghost tiny" onclick={() => copy(srtURL)}>Copy</button></div>
					{#if tcpIngestEnabled}
						<div><dt>TCP</dt><dd>{tcpURL}</dd><button class="ghost tiny" onclick={() => copy(tcpURL)}>Copy</button></div>
					{/if}
				</dl>
			</div>

			<div class="card">
				<header><h2>SRT ingest</h2><p>The preferred path — carries AV1 and HEVC. Port and toggle take effect after a restart.</p></header>
				<div class="field-row">
					<label class="switch">
						<input type="checkbox" bind:checked={srtEnabled} onchange={() => run(() => api.setSRTEnabled(srtEnabled))} />
						<span class="track"></span> Enabled
					</label>
					<div class="field compact">
						<label for="srtport">UDP port</label>
						<input id="srtport" type="number" bind:value={srtPort} />
					</div>
					<div class="field">
						<label for="srtpass">Passphrase — optional ({srtPassphraseSet ? 'set' : 'not set'})</label>
						<div class="pw-row">
							<input id="srtpass" type="password" bind:value={srtPassphrase} placeholder={srtPassphraseSet ? 'unchanged — type to replace' : '10–79 chars; empty = no encryption'} autocomplete="new-password" />
							{#if srtPassphraseSet}
								<button class="ghost tiny danger-text" onclick={() => run(async () => { const r = await api.setSRTPassphrase(''); if (r?.success !== false) { srtPassphraseSet = false; srtPassphrase = ''; } return r; }, 'SRT passphrase removed — the ingest accepts unencrypted callers again')}>Remove</button>
							{/if}
						</div>
					</div>
				</div>
				<p class="hint">With a passphrase the ingest only accepts encrypted callers carrying it — worth having when the port faces the internet. Give the sender the same passphrase. Applies to the next connection, no restart.</p>
				<div class="field-row">
					<label class="switch">
						<input type="checkbox" bind:checked={tcpIngestEnabled} onchange={() => run(() => api.setTCPIngestEnabled(tcpIngestEnabled))} />
						<span class="track"></span> TCP ingest
					</label>
					<div class="field compact">
						<label for="tcpport">TCP port</label>
						<input id="tcpport" type="number" bind:value={tcpIngestPort} onchange={() => run(() => api.setTCPIngestPort(Number(tcpIngestPort)))} />
					</div>
				</div>
				<p class="hint">Raw container over TCP: retransmits forever, so uplink loss becomes delay instead of artifacts — carries HEVC/AV1 like SRT, survives lossy links like RTMP. The sender authenticates with the stream key (Jellystreamerr: protocol TCP + this host + port). Enabled flag and port apply on restart.</p>
				<div class="field-row">
					<div class="field compact">
						<label for="tcptlsmode">TLS</label>
						<select id="tcptlsmode" bind:value={tcpTlsMode}>
							<option value="off">Off</option>
							<option value="allow">Allow TLS</option>
							<option value="require">Require TLS</option>
						</select>
					</div>
					<div class="field">
						<label for="tcptlscert">Certificate (PEM path in the container)</label>
						<div class="with-browse">
							<input id="tcptlscert" class="mono" bind:value={tcpTlsCert} placeholder="/certs/<domain>/<domain>.crt" autocomplete="off" spellcheck="false" />
							<button class="ghost small" class:active={browse?.target === 'cert'} onclick={() => openBrowse('cert')} title="Pick the file as the container sees it">Browse…</button>
						</div>
					</div>
					<div class="field">
						<label for="tcptlskey">Key (PEM path in the container)</label>
						<div class="with-browse">
							<input id="tcptlskey" class="mono" bind:value={tcpTlsKey} placeholder="/certs/<domain>/<domain>.key" autocomplete="off" spellcheck="false" />
							<button class="ghost small" class:active={browse?.target === 'key'} onclick={() => openBrowse('key')} title="Pick the file as the container sees it">Browse…</button>
						</div>
					</div>
					<div class="field compact">
						<label>&nbsp;</label>
						<button class="ghost" onclick={saveTcpTls}>Save TLS</button>
					</div>
				</div>
				{#if browse}
					<div class="browse" aria-live="polite">
						<div class="browse-head">
							<span class="mono-text">{browse.path || '…'}</span>
							<span class="browse-for">picking the {browse.target === 'cert' ? 'certificate' : 'key'} — as the container sees it</span>
							<button class="ghost small" onclick={() => (browse = null)}>Close</button>
						</div>
						{#if browse.error}
							<p class="hint danger-text">{browse.error}</p>
						{/if}
						<ul class="browse-list">
							{#if browse.path && browse.path !== '/'}
								<li><button class="browse-row dir" onclick={() => browseTo(browse.parent)}>..</button></li>
							{/if}
							{#each browse.entries as e (e.path)}
								<li>
									{#if e.dir}
										<button class="browse-row dir" class:unreadable={!e.readable} onclick={() => browseTo(e.path)}>
											{e.name}/{#if !e.readable}<span class="note">no read access</span>{/if}
										</button>
									{:else}
										<button class="browse-row" class:pem={e.pem} class:unreadable={!e.readable} onclick={() => pickFile(e)} title={e.readable ? 'Use this file' : 'The container cannot read this file — see the setfacl commands in docs/deploy-vps.md'}>
											{e.name}<span class="note">{!e.readable ? 'no read access' : e.pem ? 'PEM' : ''}</span>
										</button>
									{/if}
								</li>
							{/each}
							{#if !browse.loading && !browse.error && browse.entries.length === 0}
								<li class="hint">Empty directory.</li>
							{/if}
						</ul>
					</div>
				{/if}
				{#if tcpTls.certOk}
					<p class="hint" class:danger-text={tcpTlsExpired}>Certificate: {tcpTls.certSubject}, {tcpTlsExpired ? 'EXPIRED on' : 'valid until'} {tcpTlsDate}{tcpTls.mode === 'off' ? ' (TLS is off)' : ''}</p>
				{:else if tcpTls.certError}
					<p class="hint danger-text">Certificate: {tcpTls.certError}</p>
				{:else}
					<p class="hint">Certificate: none configured.</p>
				{/if}
				<p class="hint">Same port, no passphrase: with TLS <b>required</b> the stream key and the media are encrypted on the wire and a plaintext sender is closed at its first byte — safe to face the internet. <b>Allow</b> takes both, for switching the sender over. Off means plaintext only: keep the port tailnet-bound then. The certificate is re-read when its files change, so renewals apply on their own; borrow Caddy's, see <span class="mono">docs/deploy-vps.md</span>. Mode and paths apply to the next connection.</p>
				<footer>
					<button onclick={() => run(async () => {
						const r = await api.setSRTPort(Number(srtPort));
						if (srtPassphrase !== '' || srtPassphraseSet) {
							const p = await api.setSRTPassphrase(srtPassphrase);
							srtPassphraseSet = srtPassphrase !== '';
							srtPassphrase = '';
							return p;
						}
						return r;
					})}>Save</button>
				</footer>
			</div>

			<div class="card">
				<header><h2>Relay</h2><p>The RTSP outlet for rooms in relay mode — rtspt:// links carry this port. TCP only, one port to open. The HTTP links (MPEG-TS, HLS) ride the web port.</p></header>
				<div class="field-row">
					<div class="field compact">
						<label for="rtspport">RTSP port</label>
						<input id="rtspport" type="number" bind:value={relayRTSPPort} onchange={() => run(() => api.setRelayRTSPPort(Number(relayRTSPPort)))} />
					</div>
				</div>
				<p class="hint">Applies on restart. Open it in the firewall (ufw allow {relayRTSPPort}/tcp) when players come from the internet — the token in the link is the lock, the RTSP itself is plaintext.</p>
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

		{:else if section === 'chat'}
			<hgroup><h1>Chat</h1><p>The room's rules, and the tools to keep it kind.</p></hgroup>

			<div class="card">
				<header><h2>Room</h2></header>
				<div class="field-row">
					<label class="switch"><input type="checkbox" bind:checked={chatEnabled} onchange={() => run(() => api.setConfigValue('chat/disable', !chatEnabled))} /><span class="track"></span> Chat enabled</label>
					<label class="switch"><input type="checkbox" bind:checked={joinMessages} onchange={() => run(() => api.setConfigValue('chat/joinmessagesenabled', joinMessages))} /><span class="track"></span> Join messages</label>
				</div>
				<p class="hint">Each room's welcome message lives on its card in <b>Rooms</b>.</p>
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
				<footer>
					<button onclick={() => run(() => api.setConfigValue('name', serverName))}>Save</button>
				</footer>
				<p class="hint">Each room's stream title lives on its page in <b>Rooms</b>.</p>
			</div>

			<div class="card">
				<header><h2>Playback</h2><p>The stream is relayed as it arrives — no re-encode. Latency is the cushion viewers keep; a room can override it on its page.</p></header>
				<div class="field compact">
					<label for="latency">Latency</label>
					<select id="latency" bind:value={latency} onchange={() => run(() => api.setConfigValue('video/streamlatencylevel', Number(latency)))}>
						<option value={0}>Lowest</option><option value={1}>Low</option>
						<option value={2}>Default</option><option value={3}>High</option>
						<option value={4}>Highest buffer</option>
					</select>
				</div>
				<label class="switch">
					<input type="checkbox" bind:checked={relayTranscodeFallback} onchange={() => run(() => api.setRelayTranscodeFallback(relayTranscodeFallback))} />
					<span class="track"></span> Re-encode to H.264 on this server when a relay room's source is not H.264
				</label>
				<p class="hint">Relay players (VRChat on PC) take H.264 only. The sender is asked to send H.264 first; this is the fallback when it does not, at the cost of CPU here and a generation of quality. Off, the source passes through as it is.</p>
				{#if legacyLadder}
					<p class="hint danger-text">A transcode ladder from an earlier version is still stored ({legacyLadder} variants) — every broadcast is re-encoded on this server.
						<button class="ghost tiny" onclick={() => run(async () => { const r = await api.setOutputVariants(PASSTHROUGH); await loadConfig(); return r; }, 'Passthrough restored')}>Reset to passthrough</button></p>
				{/if}
				{#if segmentForced}
					<p class="hint">The HLS segment container is forced to {segmentForced}. Auto picks fMP4 only when the codec needs it.
						<button class="ghost tiny" onclick={() => run(async () => { const r = await api.setSegmentFormat('auto'); await loadConfig(); return r; }, 'Container set to Auto')}>Reset to Auto</button></p>
				{/if}
			</div>

			<div class="card">
				<header><h2>Site password</h2><p>The door: shared by everyone who watches, any room. Changing it ends every session except yours — everyone re-enters with the new key.</p></header>
				<div class="field">
					<label for="roompw">New site password</label>
					<input id="roompw" type="password" bind:value={newRoomPw} autocomplete="new-password" />
				</div>
				<footer>
					<button disabled={!newRoomPw} onclick={() => run(async () => { const r = await api.setSitePassword(newRoomPw); newRoomPw = ''; return r; }, 'Site password changed — everyone else was signed out')}>Change</button>
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
	.pw-row { display: flex; gap: 8px; align-items: center; }
	.pw-row input { flex: 1; }
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
	/* The real checkbox stays inside its label (position: relative) — an
	   absolutely placed input with no positioned ancestor lands at the top
	   of the document, and focusing it on click scrolled the page there. */
	.switch { position: relative; display: flex; align-items: center; gap: 9px; color: var(--text); font-size: 13px; cursor: pointer; padding: 4px 0; }
	.card > .switch { margin: 12px 0 10px; }
	.switch input { position: absolute; left: 0; top: 0; width: 1px; height: 1px; opacity: 0; pointer-events: none; }
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
	button.small { padding: 6px 10px; font-size: 12px; }
	button.ghost.active { color: var(--accent); border-color: var(--accent); }
	.with-browse { display: flex; gap: 8px; align-items: center; }
	.with-browse input { flex: 1; min-width: 0; }
	.browse { border: 1px solid var(--border); border-radius: 10px; padding: 10px 12px; margin: 0 0 12px; background: var(--surface-2); }
	.browse-head { display: flex; align-items: center; gap: 12px; margin-bottom: 6px; flex-wrap: wrap; }
	.browse-head .browse-for { color: var(--muted); font-size: 12px; flex: 1; }
	.browse-list { list-style: none; margin: 0; padding: 0; max-height: 280px; overflow: auto; }
	.browse-row {
		display: flex; align-items: center; gap: 10px; width: 100%; text-align: left;
		background: transparent; border: 0; border-radius: 6px; padding: 5px 6px;
		font-family: ui-monospace, monospace; font-size: 12.5px; font-weight: 500; color: inherit;
	}
	.browse-row:hover { background: color-mix(in srgb, var(--accent) 12%, transparent); }
	.browse-row.dir { color: var(--accent); }
	.browse-row.pem { font-weight: 600; }
	.browse-row .note { margin-left: auto; color: var(--muted); font-size: 11px; font-family: inherit; }
	.browse-row.unreadable .note { color: var(--danger); }
	button.tiny { padding: 5px 11px; font-size: 12px; }
	button.danger { background: var(--danger); color: #16090a; }
	.danger-text:hover { color: var(--danger) !important; border-color: var(--danger) !important; }

	/* ---------- status ---------- */
	.statusline { display: flex; align-items: center; gap: 9px; font-size: 15px; }
	.statusline .sep, dd .sep { color: var(--muted); margin: 0 6px; }
	.dot { width: 9px; height: 9px; border-radius: 50%; background: var(--muted); flex: none; }
	.dot.live { background: #5fc493; box-shadow: 0 0 10px #5fc493; }
	.roompick { display: flex; gap: 8px; flex-wrap: wrap; margin: 0 0 14px; }
	.roompick button { display: inline-flex; align-items: center; gap: 7px; }
	.roompick button.on { border-color: var(--accent, #dd6a4d); color: var(--text); }
	.roompick .dot { width: 7px; height: 7px; }
	.msgrow .who .dot { display: inline-block; margin-right: 4px; }

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
	/* Wraps on narrow cards: the inputs shrink first (min-width keeps them
	   usable), and the buttons drop to their own line before ever leaving
	   the card. Four buttons used to push Remove past the edge. */
	.keyrow { display: flex; gap: 8px; align-items: center; margin: 8px 0; flex-wrap: wrap; }
	.keyrow input.mono { font-family: ui-monospace, monospace; font-size: 12.5px; flex: 1.4 1 140px; min-width: 140px; }
	.field input.mono, .hint .mono { font-family: ui-monospace, monospace; font-size: 12.5px; }
	.keyrow input.comment { flex: 1 1 90px; min-width: 90px; }
	.keyrow button { flex: 0 0 auto; }

	/* ---------- rooms ---------- */
	.roomlist { display: flex; flex-direction: column; gap: 8px; max-width: 640px; margin-bottom: 22px; }
	.roomrow {
		display: flex; align-items: center; gap: 12px; width: 100%; text-align: left;
		background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
		padding: 14px 16px; color: var(--text); font: inherit; cursor: pointer;
	}
	.roomrow:hover { border-color: var(--accent); }
	.roomrow .rname { font-weight: 650; font-size: 14px; }
	.roomrow .rpath { color: var(--muted); }
	.roomrow .rstate { margin-left: auto; color: var(--muted); font-size: 12.5px; font-variant-numeric: tabular-nums; }
	.roomrow .chev { color: var(--muted); font-size: 18px; line-height: 1; }
	.roomrow .badge, .seg + .protocols { }
	.badge { font-size: 10px; letter-spacing: 0.14em; text-transform: uppercase; color: var(--accent); border: 1px solid color-mix(in srgb, var(--accent) 50%, transparent); border-radius: 999px; padding: 2px 8px; }
	.seg { display: inline-flex; border: 1px solid var(--border); border-radius: 999px; padding: 3px; gap: 2px; background: var(--surface-2); margin: 4px 0 12px; }
	.seg button { background: transparent; color: var(--muted); border: 0; border-radius: 999px; padding: 6px 16px; font-size: 13px; font-weight: 600; }
	.seg button.on { background: var(--accent); color: #101216; }
	.protocols { margin: 4px 0 10px; }
	.protocols .where { color: var(--muted); font-weight: 400; }
	.links { display: flex; flex-direction: column; gap: 6px; margin: 8px 0 4px; }
	.linkrow { display: flex; align-items: center; gap: 10px; }
	.linkrow .lk { flex: none; width: 118px; font-size: 11.5px; color: var(--muted); }
	.linkrow code { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; padding: 7px 10px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface-2); }
	.linkrow code .tok { color: var(--muted); }
	.players { margin-right: auto; color: var(--muted); font-size: 12.5px; font-variant-numeric: tabular-nums; }
	.locks.off { opacity: 0.55; }
	.backlink { display: inline-block; color: var(--muted); font-size: 12.5px; text-decoration: none; margin-bottom: 12px; }
	.backlink:hover { color: var(--accent); }
	hgroup h1 .dot.big { display: inline-block; width: 11px; height: 11px; vertical-align: 1px; margin-right: 4px; }

	/* ---------- moderation ---------- */
	.empty { color: var(--muted); font-size: 13px; }
	.hint { color: var(--muted); font-size: 12.5px; line-height: 1.5; margin: 12px 0 2px; }
	.card footer + .hint { margin-top: 14px; }
	.hint + .field-row, .hint + .keyrow { margin-top: 14px; }
	p.danger-text { color: var(--danger); }
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
	.card header h2 .dot { display: inline-block; margin-right: 6px; }
	.msgrow.hidden-msg .body { opacity: 0.35; text-decoration: line-through; }
	.msgrow.hidden-msg .actions { opacity: 1; }

	/* ---------- stat tiles ---------- */
	.tiles { display: flex; gap: 12px; }
	.spark-label { margin: 0.6rem 0 0.15rem; font-size: 0.72rem; opacity: 0.5; }
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
