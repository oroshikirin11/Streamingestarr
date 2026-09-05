// Server state: status polled every 5s, config fetched once.
import { readable, writable } from 'svelte/store';
import { getStatus, getConfig, currentChannel } from './api.js';

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

// refreshStatus polls right now — after an unlock or a mode event the
// page must not wait out the interval.
let pollNow = null;
export function refreshStatus() {
	pollNow?.();
}

export const status = readable(null, (set) => {
	let stopped = false;
	let failures = 0;
	// A remount for another room starts here again: the last room's answer
	// must not be the first thing the new room renders.
	set(null);
	pollNow = () => {
		clearTimeout(timer);
		poll();
	};
	async function poll() {
		try {
			const asked = currentChannel();
			const s = await getStatus();
			failures = 0;
			reachable.set(true);
			noteServerTime(s);
			// A poll that started for one room and lands after a switch to
			// another says nothing about the room on screen: drop it.
			if (!stopped && asked === currentChannel() && (!s?.channelId || s.channelId === asked)) set(s);
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
		pollNow = null;
		clearTimeout(timer);
	};
});

// Session role — decides whether admin affordances render.
export const role = writable('');
fetch('/api/auth/status')
	.then((r) => r.json())
	.then((d) => role.set(d.role || ''))
	.catch(() => {});
