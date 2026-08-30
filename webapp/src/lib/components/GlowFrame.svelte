<script>
	import { onMount, onDestroy } from 'svelte';
	import Hls from 'hls.js';

	let { channelId = 'main' } = $props();

	let videoEl;
	let frameEl;
	let ambiEl;
	let hls = null;
	let muted = $state(true); // autoplay needs muted; one click un-mutes
	let volume = $state(Number(localStorage.getItem('sgr_volume') ?? 1) || 1);

	const src = () => `/hls/${channelId}/stream.m3u8`;

	// A fatal hls.js error (e.g. the server restarting under us — often
	// too fast for the status poll to even flip the page to the lobby)
	// leaves the instance wedged; the only reliable recovery is a full
	// teardown and rebuild.
	let dead = false;
	let retryTimer;

	// Touch devices have no hover: a tap on the frame reveals the
	// controls, which then fade after a few seconds of stillness.
	let uiVisible = $state(false);
	let uiTimer;
	function pokeUI() {
		uiVisible = true;
		clearTimeout(uiTimer);
		uiTimer = setTimeout(() => (uiVisible = false), 3000);
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
				// Cinema trade-off: sit further behind the live edge with a
				// deep buffer so sender-side splices (overlay applies,
				// track changes) ride through invisibly. Delay is fine —
				// this is a broadcast, not a call.
				liveSyncDurationCount: 5,
				liveMaxLatencyDurationCount: 12,
				maxBufferLength: 45,
				maxMaxBufferLength: 90,
				backBufferLength: 30,
				nudgeMaxRetry: 10
			});
			hls.loadSource(src());
			hls.attachMedia(videoEl);
			hls.on(Hls.Events.MANIFEST_PARSED, () => videoEl.play().catch(() => {}));
			hls.on(Hls.Events.ERROR, (_e, data) => {
				if (!data.fatal) return;
				clearTimeout(retryTimer);
				retryTimer = setTimeout(initPlayer, 3000);
			});
		} else if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
			videoEl.src = src();
			videoEl.play().catch(() => {});
			videoEl.onerror = () => {
				clearTimeout(retryTimer);
				retryTimer = setTimeout(initPlayer, 3000);
			};
		}
	}

	onMount(initPlayer);

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
		hls?.destroy();
		hls = null;
	});

	function toggleMute() {
		muted = !muted;
		videoEl.muted = muted;
		if (!muted) videoEl.volume = volume;
	}

	function setVolume(e) {
		volume = Number(e.target.value);
		videoEl.volume = volume;
		localStorage.setItem('sgr_volume', String(volume));
		if (volume > 0 && muted) {
			muted = false;
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
	<div class="sound">
		<button class="snd-btn" title={muted ? 'Unmute' : 'Mute'} aria-label={muted ? 'Unmute' : 'Mute'} onclick={toggleMute}>
			{#if muted || volume === 0}
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/></svg>
			{:else}
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/></svg>
			{/if}
		</button>
		<input class="vol" type="range" min="0" max="1" step="0.02" value={muted ? 0 : volume} oninput={setVolume} aria-label="Volume" />
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
