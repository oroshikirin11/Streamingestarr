// Thin API layer. Everything is same-origin and cookie-authenticated by
// the viewer gate; a 401 anywhere means the session is gone — go to /login.

async function request(path, options = {}) {
	const res = await fetch(path, options);
	if (res.status === 401) {
		// Only the session gate's 401 means "log in again". Other 401s
		// (e.g. a stale chat access token) are the caller's to handle —
		// redirecting on those caused a reload loop.
		const body = await res.text();
		if (body.includes('not authenticated')) {
			window.location.href = '/login';
		}
		const err = new Error('unauthorized');
		err.status = 401;
		throw err;
	}
	if (!res.ok) throw new Error(`${path}: ${res.status}`);
	return res.json();
}

// The room this page views: /t/<room> is a scoped theater, everything else
// is the default room. The whole app keys its status, HLS, and chat on it.
export function currentChannel() {
	const m = window.location.pathname.match(/^\/t\/([a-z][a-z0-9-]*)/);
	return m ? m[1] : 'main';
}

export const getStatus = () => request(`/api/status?channel=${currentChannel()}`);
export const getConfig = () => request('/api/config');
export const getRooms = () => request('/api/rooms');

// A room's own password. The answer is {success, message}; a wrong
// password is a 401 with the sentence to show, never a door redirect.
export async function unlockRoom(password) {
	const res = await fetch('/api/rooms/unlock', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ channel: currentChannel(), password })
	});
	const body = await res.json().catch(() => ({}));
	if (res.status === 401 && String(body.message ?? '').includes('not authenticated')) {
		window.location.href = '/login';
	}
	return body;
}

export const registerChatUser = (displayName) =>
	request('/api/chat/register', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(displayName ? { displayName } : {})
	});

export const getChatHistory = (accessToken) =>
	request(`/api/chat?accessToken=${encodeURIComponent(accessToken)}&channel=${currentChannel()}`);

export async function logout() {
	try {
		await fetch('/api/auth/logout', { method: 'POST' });
	} finally {
		window.location.href = '/login';
	}
}
