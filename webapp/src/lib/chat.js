// Chat client: anonymous registration (token kept in localStorage — the
// same device token that carries the name reservation, design.md §6),
// websocket with reconnect, and a small message store.
import { writable } from 'svelte/store';
import { registerChatUser, getChatHistory, currentChannel } from './api.js';

const TOKEN_KEY = 'sgr_chat_token';
const MAX_MESSAGES = 300;

export const messages = writable([]);
export const me = writable(null);
export const connected = writable(false);
// The room's pause-vote tally (PAUSE_VOTE_STATE): sent on connect and on
// every change. null until the first frame arrives.
export const pauseVote = writable(null);
// The room's mode as the server last announced it over the socket —
// null until a ROOM_MODE event arrives. The page refreshes status on it.
export const roomModeEvent = writable(null);

let socket = null;
let reconnectDelay = 1000;
let closedByUs = false;

function pushMessage(m) {
	messages.update((list) => {
		const next = [...list, m];
		return next.length > MAX_MESSAGES ? next.slice(-MAX_MESSAGES) : next;
	});
}

async function ensureRegistered(forceNew = false) {
	let token = forceNew ? null : localStorage.getItem(TOKEN_KEY);
	if (!token) {
		// The name chosen at the login door is the proposed chat name; if
		// it is taken (someone else's reservation) fall back to a
		// generated one — renaming later is one click.
		const preferred = localStorage.getItem('sgr_preferred_name');
		let reg;
		try {
			reg = await registerChatUser(preferred || null);
		} catch {
			reg = await registerChatUser(null);
		}
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
	} catch (e) {
		if (e?.status === 401) {
			// Stale device token (e.g. from another instance): register
			// fresh and start over.
			localStorage.removeItem(TOKEN_KEY);
			me.set(null);
			try {
				token = await ensureRegistered(true);
			} catch {
				return;
			}
		}
		/* otherwise: history is a nicety, not a requirement */
	}

	const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
	socket = new WebSocket(`${proto}//${location.host}/ws?accessToken=${encodeURIComponent(token)}&channel=${currentChannel()}`);

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
		// Without the socket there is no vote button to press.
		pauseVote.update((pv) => (pv ? { ...pv, available: false, reason: 'not connected to the room' } : pv));
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
		case 'PAUSE_VOTE_STATE':
			pauseVote.set(ev);
			break;
		case 'ROOM_MODE':
			roomModeEvent.set({ mode: ev.mode, at: Date.now() });
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

// action: 'pause' | 'resume' | 'withdraw'
export function sendPauseVote(action) {
	if (!socket || socket.readyState !== WebSocket.OPEN) return;
	socket.send(JSON.stringify({ type: 'PAUSE_VOTE', action }));
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
