// Server state: status polled every 5s, config fetched once.
import { readable, writable } from 'svelte/store';
import { getStatus, getConfig } from './api.js';

export const config = writable(null);
getConfig().then(config.set).catch(() => {});

// reachable flips false after two consecutive failed polls, so the page
// can say "connection lost" instead of showing a stale live view.
export const reachable = writable(true);

export const status = readable(null, (set) => {
	let stopped = false;
	let failures = 0;
	async function poll() {
		try {
			const s = await getStatus();
			failures = 0;
			reachable.set(true);
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
