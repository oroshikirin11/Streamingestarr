// Chat client: anonymous registration (token kept in localStorage — the
// same device token that carries the name reservation, design.md §6),
// websocket with reconnect, and a small message store.
import { writable } from 'svelte/store';
import { registerChatUser, getChatHistory } from './api.js';

const TOKEN_KEY = 'sgr_chat_token';
const MAX_MESSAGES = 300;

export const messages = writable([]);
export const me = writable(null);
export const connected = writable(false);

let socket = null;
let reconnectDelay = 1000;
let closedByUs = false;

function pushMessage(m) {
	messages.update((list) => {
		const next = [...list, m];
		return next.length > MAX_MESSAGES ? next.slice(-MAX_MESSAGES) : next;
	});
}

async function ensureRegistered(forceNew = false, proposedName = null) {
	let token = forceNew ? null : localStorage.getItem(TOKEN_KEY);
	if (!token) {
		const reg = await registerChatUser(proposedName);
		token = reg.accessToken;
		localStorage.setItem(TOKEN_KEY, token);
		me.set({ id: reg.id, displayName: reg.displayName });
	}
	return token;
}

function normalize(raw) {
	// History rows and live events share shape: {type, id, timestamp, body,
	// user?, oldName?...}
	return {
		type: raw.type,
		id: raw.id ?? crypto.randomUUID(),
		timestamp: raw.timestamp,
		body: raw.body ?? '',
		user: raw.user ?? null,
		oldName: raw.oldName ?? null
	};
}

export async function connectChat() {
	closedByUs = false;
	let token;
	try {
		token = await ensureRegistered();
	} catch {
		return; // gate redirect already happened on 401
	}

	try {
		const history = await getChatHistory(token);
		if (Array.isArray(history)) {
			messages.set(history.map(normalize));
		}
	} catch {
		/* history is a nicety, not a requirement */
	}

	const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
	socket = new WebSocket(`${proto}//${location.host}/ws?accessToken=${encodeURIComponent(token)}`);

	socket.onopen = () => {
		connected.set(true);
		reconnectDelay = 1000;
	};

	socket.onmessage = (e) => {
		// The server may batch several newline-delimited JSON payloads.
		for (const line of String(e.data).split('\n')) {
			if (!line.trim()) continue;
			let ev;
			try {
				ev = JSON.parse(line);
			} catch {
				continue;
			}
			handleEvent(ev);
		}
	};

	socket.onclose = async () => {
		connected.set(false);
		if (closedByUs) return;
		setTimeout(connectChat, reconnectDelay);
		reconnectDelay = Math.min(reconnectDelay * 2, 15000);
	};
}

function handleEvent(ev) {
	switch (ev.type) {
		case 'CHAT':
		case 'SYSTEM':
		case 'CHAT_ACTION':
			pushMessage(normalize(ev));
			break;
		case 'NAME_CHANGE':
			pushMessage(normalize(ev));
			me.update((u) => (u && ev.user?.id === u.id ? { ...u, displayName: ev.user.displayName } : u));
			break;
		case 'CONNECTED_USER_INFO':
			if (ev.user) me.set({ id: ev.user.id, displayName: ev.user.displayName });
			break;
		case 'ERROR_NEEDS_REGISTRATION':
			localStorage.removeItem(TOKEN_KEY);
			socket?.close();
			ensureRegistered(true).then(connectChat);
			break;
		case 'PING':
			socket?.send(JSON.stringify({ type: 'PONG' }));
			break;
		case 'VISIBILITY-UPDATE':
			if (ev.ids && ev.visible === false) {
				messages.update((list) => list.filter((m) => !ev.ids.includes(m.id)));
			}
			break;
	}
}

export function sendChatMessage(body) {
	if (!socket || socket.readyState !== WebSocket.OPEN) return;
	socket.send(JSON.stringify({ type: 'CHAT', body }));
}

export function requestNameChange(newName) {
	if (!socket || socket.readyState !== WebSocket.OPEN) return;
	socket.send(JSON.stringify({ type: 'NAME_CHANGE', newName }));
}

export function disconnectChat() {
	closedByUs = true;
	socket?.close();
	socket = null;
}
