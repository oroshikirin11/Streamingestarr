<script>
	import { onMount, onDestroy } from 'svelte';
	import Hls from 'hls.js';

	let { channelId = 'main' } = $props();

	let videoEl;
	let frameEl;
	let hls = null;
	let muted = $state(true); // autoplay needs muted; one click un-mutes

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
		videoEl.play().catch(() => {});
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
