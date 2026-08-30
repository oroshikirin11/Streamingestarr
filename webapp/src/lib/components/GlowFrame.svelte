<script>
	import { onMount, onDestroy } from 'svelte';
	import Hls from 'hls.js';

	let { channelId = 'main' } = $props();

	let videoEl;
	let frameEl;
	let hls = null;
	let muted = $state(true); // autoplay needs muted; one click un-mutes
	let volume = $state(Number(localStorage.getItem('sgr_volume') ?? 1) || 1);

	const src = () => `/hls/${channelId}/stream.m3u8`;

	onMount(() => {
		if (Hls.isSupported()) {
			hls = new Hls({
				// The gate rides on the session cookie; same-origin fetches
				// carry it automatically, but be explicit for safety.
				xhrSetup: (xhr) => {
					xhr.withCredentials = true;
				},
				liveDurationInfinity: true
			});
			hls.loadSource(src());
			hls.attachMedia(videoEl);
			hls.on(Hls.Events.MANIFEST_PARSED, () => videoEl.play().catch(() => {}));
			hls.on(Hls.Events.ERROR, (_e, data) => {
				if (!data.fatal) return;
				if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
					// Stream restarts/splices: retry from the top.
					setTimeout(() => hls?.loadSource(src()), 3000);
				} else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
					hls.recoverMediaError();
				}
			});
		} else if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
			videoEl.src = src();
			videoEl.play().catch(() => {});
		}
	});

	onDestroy(() => {
		hls?.destroy();
		hls = null;
	});

	function unmute() {
		muted = false;
		videoEl.muted = false;
		videoEl.volume = volume;
		videoEl.play().catch(() => {});
	}

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

<div class="glow-frame" bind:this={frameEl}>
	<div class="lamp"></div>
	<!-- svelte-ignore a11y_media_has_caption -->
	<video bind:this={videoEl} playsinline muted={muted}></video>
	<div class="soft-badge"><span class="dot"></span> showing now</div>
	{#if muted}
		<button class="unmute" onclick={unmute}>tap for sound</button>
	{/if}
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

<style>
	.glow-frame {
		flex: 1;
		border-radius: var(--radius);
		position: relative;
		min-height: 0;
		background: linear-gradient(170deg, #1e1e26, #131318);
		box-shadow:
			0 0 90px -8px color-mix(in srgb, var(--accent) 22%, transparent),
			0 0 240px -30px color-mix(in srgb, var(--accent) 30%, transparent),
			inset 0 0 0 1px #ffffff10;
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
	.lamp {
		position: absolute;
		inset: 0;
		background: radial-gradient(
			ellipse 120% 90% at 50% 120%,
			color-mix(in srgb, var(--accent) 7%, transparent),
			transparent 55%
		);
	}
	.soft-badge {
		position: absolute;
		top: 18px;
		left: 20px;
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 11.5px;
		color: #e9c9bf;
		letter-spacing: 0.06em;
		background: #00000070;
		padding: 7px 14px;
		border-radius: 99px;
		backdrop-filter: blur(6px);
	}
	.soft-badge .dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--accent);
		box-shadow: 0 0 10px var(--accent);
		animation: breathe 2.4s infinite;
	}
	@keyframes breathe {
		50% {
			opacity: 0.4;
		}
	}
	.unmute {
		position: absolute;
		bottom: 18px;
		left: 50%;
		transform: translateX(-50%);
		background: #000000a0;
		border: 1px solid #ffffff22;
		color: #eee;
		border-radius: 99px;
		padding: 8px 18px;
		font-size: 12.5px;
		cursor: pointer;
		backdrop-filter: blur(6px);
	}
	.unmute:hover {
		border-color: var(--accent);
		color: var(--accent);
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
	.glow-frame:hover .sound {
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
	.glow-frame:hover .frame-fs {
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
