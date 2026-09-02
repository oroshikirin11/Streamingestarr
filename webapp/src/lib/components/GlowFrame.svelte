<script>
	import { onMount, onDestroy } from 'svelte';
	import Hls from 'hls.js';
	import { sound, setSoundVolume, setSoundMuted, setForcedMute } from '../sound.js';

	let { channelId = 'main', clockSkewMs = 0, paused = false } = $props();

	let videoEl;
	let frameEl;
	let ambiEl;
	let hls = null;
	// Sound-first: viewers arrive through the login click, which counts as
	// the user gesture browsers want before unmuted autoplay. startPlayback
	// tries with sound and falls back to muted only when the browser
	// actually refuses (e.g. a restored tab with no interaction yet).
	// Sound preference lives above this component (lib/sound.js) so it
	// survives the remounts an offline->online cycle causes — a new
	// episode with a different frame shape is exactly that, and rebuilding
	// from defaults there reset the slider and re-muted the viewer.
	let muted = $state(sound.muted);
	let volume = $state(sound.volume);

	const src = () => `/hls/${channelId}/stream.m3u8`;

	// A fatal hls.js error (e.g. the server restarting under us — often
	// too fast for the status poll to even flip the page to the lobby)
	// leaves the instance wedged; the only reliable recovery is a full
	// teardown and rebuild.
	let dead = false;
	let retryTimer;

	// ---- playback incident watchdog ----
	// Records the moments playback misbehaves — audio replays (backward
	// jumps), forward discontinuities, stalls, and the sync controller's
	// own snaps — and ships them to the server every 10s so the admin can
	// read them NEXT TO the segment ledger: incidents while the ledger
	// shows stretched wall-steps = the feed arrived late; incidents over a
	// clean ledger = this viewer's network or player. Also mirrored on
	// window.__sgrSync.incidents for console forensics.
	let incidentRing = [];
	let incidentPending = [];
	let lastDrift = null;
	let lastControllerSeek = 0;
	function bufferedAhead() {
		try {
			const b = videoEl.buffered;
			for (let i = 0; i < b.length; i++) {
				if (videoEl.currentTime >= b.start(i) - 0.5 && videoEl.currentTime <= b.end(i)) {
					return b.end(i) - videoEl.currentTime;
				}
			}
		} catch {}
		return 0;
	}
	function noteIncident(type, magMs, detail = '') {
		if (!videoEl) return;
		const ev = {
			t: Date.now(),
			type,
			magMs: Math.round(magMs),
			posS: +videoEl.currentTime.toFixed(1),
			bufferS: +bufferedAhead().toFixed(1),
			driftS: lastDrift == null ? 0 : +lastDrift.toFixed(2),
			rate: videoEl.playbackRate,
			detail
		};
		incidentRing = [...incidentRing.slice(-59), ev];
		incidentPending.push(ev);
		window.__sgrSync = { ...(window.__sgrSync ?? {}), incidents: incidentRing };
	}

	// Touch devices have no hover: a tap on the frame reveals the
	// controls, which then fade after a few seconds of stillness.
	let uiVisible = $state(false);
	let uiTimer;
	function pokeUI() {
		uiVisible = true;
		clearTimeout(uiTimer);
		uiTimer = setTimeout(() => (uiVisible = false), 3000);
	}

	// Try to start with sound at the remembered volume; if the browser's
	// autoplay policy refuses (a refresh wipes the page's gesture state, so
	// this is the NORMAL path on reload), restart muted rather than not
	// playing at all — and arm a one-shot listener so the very first
	// click/tap anywhere brings the sound in, instead of demanding the
	// viewer find the speaker button.
	let unmuteHandler = null;
	function disarmUnmute() {
		if (!unmuteHandler) return;
		window.removeEventListener('pointerdown', unmuteHandler, true);
		unmuteHandler = null;
	}
	function armUnmuteOnGesture() {
		if (unmuteHandler) return;
		unmuteHandler = (e) => {
			// A tap on the sound controls speaks for itself — let it decide.
			if (e.target?.closest?.('.snd-btn, .vol')) return;
			disarmUnmute();
			muted = false;
			setForcedMute(false);
			videoEl.muted = false;
			videoEl.volume = volume;
			videoEl.play().catch(() => {});
		};
		window.addEventListener('pointerdown', unmuteHandler, true);
	}
	function startPlayback() {
		videoEl.muted = muted;
		videoEl.volume = volume;
		videoEl.play().catch(() => {
			// The browser's constraint, not the viewer's wish — see
			// setForcedMute. Remembering it would mute every later visit.
			muted = true;
			setForcedMute(true);
			videoEl.muted = true;
			armUnmuteOnGesture();
			videoEl.play().catch(() => {});
		});
	}

	function initPlayer() {
		if (dead) return;
		hls?.destroy();
		if (Hls.isSupported()) {
			hls = new Hls({
				// The gate rides on the session cookie; same-origin fetches
				// carry it automatically, but be explicit for safety.
				xhrSetup: (xhr) => {
					xhr.withCredentials = true;
				},
				liveDurationInfinity: true,
				// Three segments behind the live edge (hls.js's own default).
				// This sat at 5 while sender-side splices were rough seams
				// the buffer had to absorb; the sender now splices on exact
				// sent-frontier pts (overlay applies, track changes,
				// pause/resume all measured seam-free), so the extra two
				// segments bought nothing but ~6s of glass-to-glass delay.
				// Revert to 5 if a sender older than that ever feeds this.
				liveSyncDurationCount: 3,
				liveMaxLatencyDurationCount: 12,
				// Rate steering is OWNED by the room-sync controller below —
				// hls.js's own latency controller stays off (default rate 1)
				// so two steering loops never fight over playbackRate.
				maxBufferLength: 45,
				maxMaxBufferLength: 90,
				backBufferLength: 30,
				nudgeMaxRetry: 10
			});
			hls.loadSource(src());
			hls.attachMedia(videoEl);
			hls.on(Hls.Events.MANIFEST_PARSED, startPlayback);
			hls.on(Hls.Events.ERROR, (_e, data) => {
				if (!data.fatal) {
					// Buffer stalls and nudges are exactly the rubberband
					// moments — worth a line in the incident feed.
					if (/stall|nudge|gap|hole/i.test(String(data.details ?? ''))) {
						noteIncident('hls-error', 0, String(data.details ?? ''));
					}
					return;
				}
				clearTimeout(retryTimer);
				retryTimer = setTimeout(initPlayer, 3000);
			});
		} else if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
			videoEl.src = src();
			startPlayback();
			videoEl.onerror = () => {
				clearTimeout(retryTimer);
				retryTimer = setTimeout(initPlayer, 3000);
			};
		}
	}

	onMount(initPlayer);

	// Belt and braces: should any navigation path ever swap the channel
	// prop on a LIVE instance instead of remounting, rebuild the player —
	// an hls session is bound to one room's playlist forever.
	let bootedChannel = channelId;
	$effect(() => {
		if (channelId !== bootedChannel) {
			bootedChannel = channelId;
			initPlayer();
		}
	});

	// Room sync: every client steers to the same ABSOLUTE moment — the
	// server's wall clock (EXT-X-PROGRAM-DATE-TIME in the playlists,
	// local-clock skew corrected via the status poll's serverTime) minus
	// one shared target latency. "My playlist edge minus five segments"
	// differed between viewers by fetch timing; the wall clock does not.
	//
	// Steering is gentle and bounded: ±2%/s of drift within [0.97, 1.05]
	// (browsers pitch-correct — inaudible), a dead band so rate sits at
	// exactly 1 when in sync, and the hard snap only for gross drift after
	// a real stall. When PDT or the skew is unavailable the controller
	// falls back to hls.js's edge-based latency — the exact behaviour that
	// shipped before wall-clock sync existed.
	const SYNC = {
		deadband: 0.35, // s — inside this, rate is exactly 1
		snapAt: 8, // s behind target — jump instead of crawling
		rateMax: 1.05,
		rateMin: 0.97,
		gain: 0.02 // rate delta per second of drift
	};
	// The shared wall-clock anchor. PDT is stamped when a segment is
	// WRITTEN, so it runs ~1.5 SEGMENT DURATIONS newer than the media —
	// an absolute offset, not a fraction of the target. The old 0.7
	// factor encoded exactly that at five segments (1 - 1.5/5) and was
	// verified live (10.8s wall vs edge target 15); expressing it per
	// segment keeps the same physics at any sync depth, so a fresh join
	// still starts inside the dead band instead of sinking for minutes.
	// The exact anchor only shifts the room's shared depth, never its
	// uniformity — every client computes the same number.
	const wallAnchor = (edgeTarget) => {
		const count = hls?.config?.liveSyncDurationCount || 3;
		return edgeTarget * (1 - 1.5 / count);
	};
	let driftTimer;
	let lastSnapEdge = -1;
	onMount(() => {
		driftTimer = setInterval(() => {
			if (!hls || !videoEl || dead) return;
			// The streamer pressed pause: the live edge is about to freeze
			// DELIBERATELY. Steering against a frozen edge is what looped
			// the last seconds forever — so the controller stands down, the
			// buffered tail plays out, and the picture rests on its final
			// frame until the broadcast moves again (then one clean snap
			// re-joins the room at the live edge).
			if (paused) {
				if (videoEl.playbackRate !== 1) videoEl.playbackRate = 1;
				return;
			}
			if (videoEl.paused || videoEl.seeking) {
				if (videoEl.playbackRate !== 1) videoEl.playbackRate = 1;
				return;
			}
			if (videoEl.readyState < 3) {
				// Starved at the buffer's end. A browser never crawls out of
				// a deep hole on its own — when fresh segments exist well
				// ahead of the stall point (the feed came back after a
				// freeze), jump to them.
				if (
					Number.isFinite(hls.liveSyncPosition) &&
					hls.liveSyncPosition > videoEl.currentTime + 3 &&
					Math.abs(hls.liveSyncPosition - lastSnapEdge) >= 1
				) {
					lastSnapEdge = hls.liveSyncPosition;
					lastControllerSeek = performance.now();
					noteIncident('sync-snap', (hls.liveSyncPosition - videoEl.currentTime) * 1000);
					videoEl.currentTime = hls.liveSyncPosition;
				}
				if (videoEl.playbackRate !== 1) videoEl.playbackRate = 1;
				return;
			}
			// The fallback mirrors the configured depth (3 x 3s segments).
			const edgeTarget = Number.isFinite(hls.targetLatency) ? hls.targetLatency : 9;
			let drift = null;
			let mode = 'wall';
			const pd = hls.playingDate;
			if (pd instanceof Date && Number.isFinite(pd.getTime())) {
				const wallLatency = (Date.now() + clockSkewMs - pd.getTime()) / 1000;
				drift = wallLatency - wallAnchor(edgeTarget);
			} else if (Number.isFinite(hls.latency)) {
				mode = 'edge';
				drift = hls.latency - edgeTarget;
			}
			// Debug handle: read window.__sgrSync in a console to see what
			// the controller sees. Costs nothing, settles arguments.
			lastDrift = drift;
			window.__sgrSync = { ...(window.__sgrSync ?? {}), channel: channelId, drift, mode, rate: videoEl.playbackRate, skewMs: clockSkewMs };
			if (drift == null || !Number.isFinite(drift)) return;
			if (Math.abs(drift) > SYNC.snapAt) {
				// One clean seek instead of minutes of crawling — both
				// directions. drift > 0 = too latent, seek forward;
				// drift < 0 = ahead of the room (an early join near the
				// edge of a young session), seek back into the playlist
				// window. Guarded by the seekable range so it can never
				// jump outside what actually exists; when the range can't
				// take it, fall through to bounded rate steering.
				const want = videoEl.currentTime + drift;
				const s = videoEl.seekable;
				if (s.length && want > s.start(0) + 1 && want < s.end(s.length - 1) - 0.5) {
					lastControllerSeek = performance.now();
					noteIncident('sync-snap', drift * 1000);
					videoEl.currentTime = want;
					videoEl.playbackRate = 1;
					return;
				}
				if (drift > 0 && Number.isFinite(hls.liveSyncPosition)) {
					// A frozen playlist means a frozen liveSyncPosition —
					// snapping to it again just replays the same seconds in
					// a loop. Only snap when the edge has actually moved;
					// otherwise hold and let the stream come back to us.
					if (Math.abs(hls.liveSyncPosition - lastSnapEdge) < 1) {
						videoEl.playbackRate = 1;
						return;
					}
					lastSnapEdge = hls.liveSyncPosition;
					lastControllerSeek = performance.now();
					noteIncident('sync-snap', drift * 1000);
					videoEl.currentTime = hls.liveSyncPosition;
					videoEl.playbackRate = 1;
					return;
				}
			}
			if (Math.abs(drift) <= SYNC.deadband) {
				if (videoEl.playbackRate !== 1) videoEl.playbackRate = 1;
				return;
			}
			videoEl.playbackRate = Math.max(SYNC.rateMin, Math.min(SYNC.rateMax, 1 + drift * SYNC.gain));
		}, 1000);
		return () => clearInterval(driftTimer);
	});

	// The watchdog itself: four times a second, compare how far the MEDIA
	// moved against how much WALL time passed. Backward movement the sync
	// controller did not order = the browser replayed (repeated words);
	// forward movement beyond real time = a gap jump; no movement while
	// playing = a stall (reported with its duration when it ends).
	let wdTimer;
	let shipTimer;
	onMount(() => {
		let wd = { wall: 0, media: 0, rate: 1 };
		let stallStart = 0;
		wdTimer = setInterval(() => {
			if (!videoEl || dead) return;
			const now = performance.now();
			const media = videoEl.currentTime;
			const playing = !videoEl.paused && !videoEl.seeking && !paused;
			if (wd.wall && playing) {
				const wallDt = (now - wd.wall) / 1000;
				const mediaDt = media - wd.media;
				const ordered = now - lastControllerSeek < 1500;
				if (wallDt < 2) {
					if (mediaDt < -0.25 && !ordered) {
						noteIncident('backjump', -mediaDt * 1000);
					} else if (mediaDt - wallDt * wd.rate > 1.0 && !ordered) {
						noteIncident('forwardjump', (mediaDt - wallDt * wd.rate) * 1000);
					}
					if (mediaDt < 0.02 && wallDt > 0.2) {
						if (!stallStart) stallStart = now;
					} else if (stallStart) {
						const dur = now - stallStart;
						stallStart = 0;
						if (dur > 500) noteIncident('stall', dur);
					}
				}
			} else {
				stallStart = 0;
			}
			wd = { wall: now, media, rate: videoEl.playbackRate };
		}, 250);

		shipTimer = setInterval(() => {
			if (!incidentPending.length) return;
			const batch = incidentPending.splice(0, 30);
			fetch('/api/metrics/player', {
				method: 'POST',
				credentials: 'include',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ channel: channelId, incidents: batch })
			}).catch(() => {
				// Lost reports rejoin the queue; the ring stays bounded.
				incidentPending = batch.concat(incidentPending).slice(-60);
			});
		}, 10000);

		return () => {
			clearInterval(wdTimer);
			clearInterval(shipTimer);
		};
	});

	// Ambilight: the video is drawn tiny (32×18) into a canvas that sits
	// behind the frame, scaled up and heavily blurred by CSS. Drawing with
	// low alpha accumulates frames, so colour changes ease in like a TV
	// backlight instead of flickering per cut.
	let ambiTimer;
	onMount(() => {
		const ctx = ambiEl.getContext('2d', { alpha: false });
		ctx.fillStyle = '#141416';
		ctx.fillRect(0, 0, 32, 18);
		ambiTimer = setInterval(() => {
			if (document.hidden || !videoEl || videoEl.readyState < 2 || videoEl.paused) return;
			ctx.globalAlpha = 0.18;
			try {
				ctx.drawImage(videoEl, 0, 0, 32, 18);
			} catch {}
		}, 120);
		return () => clearInterval(ambiTimer);
	});

	onDestroy(() => {
		dead = true;
		clearTimeout(retryTimer);
		disarmUnmute();
		hls?.destroy();
		hls = null;
	});

	function toggleMute() {
		// An explicit choice either way retires the first-gesture unmute.
		disarmUnmute();
		muted = !muted;
		setSoundMuted(muted);
		videoEl.muted = muted;
		if (!muted) videoEl.volume = volume;
	}

	function setVolume(e) {
		volume = Number(e.target.value);
		setSoundVolume(volume);
		videoEl.volume = volume;
		if (volume > 0 && muted) {
			muted = false;
			setSoundMuted(false);
			videoEl.muted = false;
		}
	}

	export function goFullscreen() {
		if (document.fullscreenElement) {
			document.exitFullscreen();
			return;
		}
		(frameEl.requestFullscreen || frameEl.webkitRequestFullscreen)?.call(frameEl);
	}
</script>

<div class="frame-wrap">
	<canvas class="ambilight" bind:this={ambiEl} width="32" height="18"></canvas>
	<div class="glow-frame" class:show-ui={uiVisible} bind:this={frameEl} onpointerup={pokeUI}>
		<!-- svelte-ignore a11y_media_has_caption -->
		<video bind:this={videoEl} playsinline muted={muted}></video>
		{#if paused}
			<div class="paused-veil">
				<span class="paused-chip">⏸&nbsp; paused by the streamer</span>
			</div>
		{/if}
	<div class="sound">
		<button class="snd-btn" title={muted ? 'Unmute' : 'Mute'} aria-label={muted ? 'Unmute' : 'Mute'} onclick={toggleMute}>
			{#if muted || volume === 0}
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/></svg>
			{:else}
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/></svg>
			{/if}
		</button>
		<!-- Show the real volume even while autoplay-muted, so the slider
		     opens where sound WILL be once unmuted instead of at zero; the
		     speaker icon still carries the muted state. -->
		<input class="vol" type="range" min="0" max="1" step="0.02" value={volume} oninput={setVolume} aria-label="Volume" />
	</div>
		<button class="frame-fs" title="Fullscreen" onclick={goFullscreen} aria-label="Fullscreen">
			<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"/></svg>
		</button>
	</div>
</div>

<style>
	.frame-wrap {
		flex: 1;
		position: relative;
		min-height: 0;
	}
	.ambilight {
		position: absolute;
		inset: -28px;
		width: calc(100% + 56px);
		height: calc(100% + 56px);
		border-radius: calc(var(--radius) * 2);
		filter: blur(46px) saturate(1.5) brightness(0.9);
		opacity: 0.55;
		pointer-events: none;
	}
	.glow-frame {
		position: absolute;
		inset: 0;
		border-radius: var(--radius);
		background: #101014;
		box-shadow: inset 0 0 0 1px #ffffff10;
		overflow: hidden;
	}
	video {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: contain;
		background: transparent;
	}
	.paused-veil {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		background: #00000038;
		pointer-events: none;
	}
	.paused-chip {
		padding: 10px 20px;
		border-radius: 99px;
		background: #000000b8;
		border: 1px solid #ffffff22;
		color: #eee;
		font-size: 14px;
		letter-spacing: 0.04em;
		backdrop-filter: blur(8px);
	}
	.sound {
		position: absolute;
		left: 16px;
		bottom: 16px;
		display: flex;
		align-items: center;
		gap: 4px;
		background: #000000a0;
		border: 1px solid #ffffff22;
		border-radius: 99px;
		padding: 4px 8px 4px 4px;
		backdrop-filter: blur(6px);
		opacity: 0;
		transition: 0.2s;
	}
	@media (hover: hover) {
		.glow-frame:hover .sound {
			opacity: 1;
		}
	}
	.glow-frame.show-ui .sound {
		opacity: 1;
	}
	.snd-btn {
		width: 28px;
		height: 28px;
		border: 0;
		border-radius: 50%;
		background: transparent;
		color: #ddd;
		display: grid;
		place-items: center;
		cursor: pointer;
	}
	.snd-btn:hover {
		color: var(--accent);
	}
	.snd-btn svg {
		width: 15px;
		height: 15px;
	}
	.vol {
		appearance: none;
		-webkit-appearance: none;
		width: 90px;
		height: 4px;
		border-radius: 99px;
		background: #ffffff2c;
		outline: none;
		cursor: pointer;
	}
	.vol::-webkit-slider-thumb {
		-webkit-appearance: none;
		width: 12px;
		height: 12px;
		border-radius: 50%;
		background: var(--accent);
		border: 0;
	}
	.vol::-moz-range-thumb {
		width: 12px;
		height: 12px;
		border-radius: 50%;
		background: var(--accent);
		border: 0;
	}
	.frame-fs {
		position: absolute;
		right: 16px;
		bottom: 16px;
		opacity: 0;
		transition: 0.2s;
		background: #000000a0;
		border: 1px solid #ffffff22;
		border-radius: 10px;
		width: 36px;
		height: 36px;
		display: grid;
		place-items: center;
		color: #ddd;
		cursor: pointer;
		backdrop-filter: blur(6px);
	}
	@media (hover: hover) {
		.glow-frame:hover .frame-fs {
			opacity: 1;
		}
	}
	.glow-frame.show-ui .frame-fs {
		opacity: 1;
	}
	.frame-fs:hover {
		color: var(--accent);
		border-color: var(--accent);
	}
	.frame-fs svg {
		width: 16px;
		height: 16px;
	}
</style>
