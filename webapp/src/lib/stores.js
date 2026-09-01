// Server state: status polled every 5s, config fetched once.
import { readable, writable } from 'svelte/store';
import { getStatus, getConfig } from './api.js';

export const config = writable(null);
getConfig().then(config.set).catch(() => {});

// reachable flips false after two consecutive failed polls, so the page
// can say "connection lost" instead of showing a stale live view.
export const reachable = writable(true);

// Server-clock skew (serverTime minus local now, ms), smoothed across
// polls. The player's room-sync anchors every viewer to the SERVER's
// wall clock; local clocks being minutes off must not matter. Includes
// half the request time as noise — the EMA and the steering deadband
// both absorb that comfortably.
export const clockSkewMs = writable(0);
let skewSeeded = false;
function noteServerTime(s) {
	const t = new Date(s?.serverTime ?? NaN).getTime();
	if (!Number.isFinite(t)) return;
	const sample = t - Date.now();
	clockSkewMs.update((prev) => (skewSeeded ? prev * 0.8 + sample * 0.2 : ((skewSeeded = true), sample)));
}

export const status = readable(null, (set) => {
	let stopped = false;
	let failures = 0;
	async function poll() {
		try {
			const s = await getStatus();
			failures = 0;
			reachable.set(true);
			noteServerTime(s);
			if (!stopped) set(s);
		} catch {
			failures += 1;
			if (failures >= 2) reachable.set(false);
		}
		if (!stopped) timer = setTimeout(poll, failures > 0 ? 3000 : 5000);
	}
	let timer;
	poll();
	return () => {
		stopped = true;
		clearTimeout(timer);
	};
});

// Session role — decides whether admin affordances render.
export const role = writable('');
fetch('/api/auth/status')
	.then((r) => r.json())
	.then((d) => role.set(d.role || ''))
	.catch(() => {});
