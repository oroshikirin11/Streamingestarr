<script>
	// The offline state — the reason to come back (docs/design.md §7).
	// With a pushed schedule the lobby shows the next showing + countdown;
	// without, the room simply rests with the last-watched note.
	import { dedupeSubtitle } from '../text.js';
	let { status, children } = $props();

	// The lobby's copy shuffles per visit — every slot independently, so
	// the room greets a regular differently almost every time.
	const pick = (list) => list[Math.floor(Math.random() * list.length)];
	const KICKERS = [
		'between showings',
		'the lights are low',
		'nothing on the marquee right now',
		'the projector sleeps',
		'intermission',
		'the room is resting'
	];
	const HEADLINES = [
		'The screen is dark, the seats are warm',
		'Nothing playing — yet',
		'The theater keeps your seat',
		'Come back when the marquee lights up',
		'Every good cinema is quiet most of the day',
		'See you at the next showing'
	];
	const SUBLINES = [
		'the moment something plays, this page comes alive on its own',
		'no need to refresh — the room wakes you',
		'popcorn optional, patience appreciated',
		'the projector warms up faster than you can find snacks',
		'the doors open when the projector spins up'
	];
	const IMMINENT = ['dimming the lights…', 'the projector is spinning up', 'find your seat', 'curtain in moments'];
	const FOOT_PREFIXES = ['last watched together', 'previously in this room', 'the last picture show', 'still echoing'];
	const mood = { k: pick(KICKERS), h: pick(HEADLINES), s: pick(SUBLINES) };
	const imminentLine = pick(IMMINENT);
	const footPrefix = pick(FOOT_PREFIXES);

	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	const nextShowing = $derived(status?.schedule?.[0] ?? null);

	const countdown = $derived.by(() => {
		if (!nextShowing) return null;
		let s = Math.floor((new Date(nextShowing.startsAt).getTime() - now) / 1000);
		if (s <= 0) return { h: '00', m: '00', s: '00', imminent: true };
		return {
			h: String(Math.floor(s / 3600)).padStart(2, '0'),
			m: String(Math.floor((s % 3600) / 60)).padStart(2, '0'),
			s: String(s % 60).padStart(2, '0'),
			imminent: false
		};
	});

	const whenLabel = $derived.by(() => {
		if (!nextShowing) return '';
		const d = new Date(nextShowing.startsAt);
		const sameDay = d.toDateString() === new Date(now).toDateString();
		const time = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
		return (sameDay ? 'Tonight at ' : d.toLocaleDateString(undefined, { weekday: 'long' }) + ' at ') + time +
			(nextShowing.subtitle ? ' · ' + nextShowing.subtitle : '');
	});

	const lastWatched = $derived.by(() => {
		const t = status?.lastDisconnectTime;
		if (!t) return null;
		const d = new Date(t);
		const days = Math.floor((Date.now() - d.getTime()) / 86400000);
		if (days === 0) return 'today';
		if (days === 1) return 'yesterday';
		return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
	});
</script>

<div class="lobby">
	<div class="ember-room">
		<div class="ember"></div>
		<div class="ember"></div>
		<div class="ember"></div>
		<div class="ember"></div>
		<div class="lobby-card">
			<div class="z">{countdown?.imminent ? imminentLine : mood.k}</div>
			{#if nextShowing}
				{#if nextShowing.artworkId}
					<img class="poster" src={'/artwork/' + nextShowing.artworkId} alt="" />
				{/if}
				<h2>{nextShowing.title}</h2>
				<div class="when">{whenLabel}</div>
				{#if countdown && !countdown.imminent}
					<div class="cd">
						<div class="cell"><div class="num">{countdown.h}</div><div class="lab">hrs</div></div>
						<div class="sep">:</div>
						<div class="cell"><div class="num">{countdown.m}</div><div class="lab">min</div></div>
						<div class="sep">:</div>
						<div class="cell"><div class="num">{countdown.s}</div><div class="lab">sec</div></div>
					</div>
				{/if}
			{:else}
				<h2>{mood.h}</h2>
				<div class="when">{mood.s}</div>
			{/if}
			{#if status?.lastPlayed?.title}
				<div class="foot">
					{footPrefix} — <b>{status.lastPlayed.title}{dedupeSubtitle(status.lastPlayed.title, status.lastPlayed.subtitle) ? ' · ' + dedupeSubtitle(status.lastPlayed.title, status.lastPlayed.subtitle) : ''}</b>
					{#if lastWatched}<span class="dim"> · {lastWatched}</span>{/if}
				</div>
			{:else if lastWatched}
				<div class="foot">{footPrefix} — <b>{lastWatched}</b></div>
			{/if}
		</div>
		{#if children}
			<div class="stage-slot">{@render children()}</div>
		{/if}
	</div>
</div>

<style>
	.lobby {
		display: flex;
		flex: 1;
		min-height: 0;
	}
	.ember-room {
		flex: 1;
		border-radius: var(--radius);
		position: relative;
		overflow: hidden;
		display: grid;
		grid-template-rows: 1fr auto;
		place-items: center;
		background:
			radial-gradient(ellipse 90% 70% at 50% 115%, #2a1c18 0%, transparent 55%),
			linear-gradient(180deg, #17161a, #121114);
		box-shadow:
			inset 0 0 0 1px #ffffff0d,
			0 0 120px -30px color-mix(in srgb, var(--accent) 18%, transparent);
	}
	.ember {
		position: absolute;
		bottom: -4px;
		width: 3px;
		height: 3px;
		border-radius: 50%;
		background: var(--accent);
		opacity: 0;
		animation: rise 7s infinite;
	}
	.ember:nth-child(1) { left: 24%; animation-delay: 0s; }
	.ember:nth-child(2) { left: 46%; animation-delay: 2.2s; }
	.ember:nth-child(3) { left: 62%; animation-delay: 4.1s; }
	.ember:nth-child(4) { left: 78%; animation-delay: 1.3s; }
	@keyframes rise {
		0% { transform: translateY(0); opacity: 0; }
		12% { opacity: 0.7; }
		100% { transform: translateY(-46vh); opacity: 0; }
	}
	.lobby-card {
		text-align: center;
		padding: 20px;
		grid-row: 1;
	}
	/* The open stage rides the lobby's own backdrop — same dead-stream
	   room, the credentials resting at its foot. */
	.stage-slot {
		grid-row: 2;
		justify-self: center;
		width: min(792px, 100%);
		padding: 0 16px 16px;
	}
	.z {
		font-size: 12px;
		letter-spacing: 0.3em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.poster {
		width: 108px;
		height: 156px;
		object-fit: cover;
		border-radius: 8px;
		border: 1px solid var(--border);
		margin: 18px auto 0;
		display: block;
		box-shadow: 0 14px 40px -12px #000;
	}
	h2 {
		font-size: 34px;
		font-weight: 300;
		margin: 18px 0 4px;
	}
	.when {
		font-size: 13.5px;
		color: var(--muted);
	}
	.cd {
		display: inline-flex;
		gap: 14px;
		margin-top: 26px;
		background: #00000055;
		border: 1px solid var(--border);
		border-radius: 16px;
		padding: 16px 26px;
	}
	.cd .cell {
		text-align: center;
	}
	.cd .num {
		font-size: 28px;
		font-weight: 250;
		font-variant-numeric: tabular-nums;
	}
	.cd .lab {
		font-size: 9.5px;
		letter-spacing: 0.2em;
		color: var(--muted);
		text-transform: uppercase;
		margin-top: 3px;
	}
	.cd .sep {
		font-size: 28px;
		font-weight: 200;
		color: var(--accent);
	}
	.foot {
		margin-top: 30px;
		font-size: 12px;
		color: #6b6660;
	}
	.foot b {
		color: var(--muted);
		font-weight: 500;
	}
	.dim {
		color: #6b6660;
	}
</style>
