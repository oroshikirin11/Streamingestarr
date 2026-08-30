// Server state: status polled every 5s, config fetched once.
import { readable, writable } from 'svelte/store';
import { getStatus, getConfig } from './api.js';

export const config = writable(null);
getConfig().then(config.set).catch(() => {});

export const status = readable(null, (set) => {
	let stopped = false;
	async function poll() {
		try {
			const s = await getStatus();
			if (!stopped) set(s);
		} catch {
			/* keep last known status */
		}
		if (!stopped) timer = setTimeout(poll, 5000);
	}
	let timer;
	poll();
	return () => {
		stopped = true;
		clearTimeout(timer);
	};
});
