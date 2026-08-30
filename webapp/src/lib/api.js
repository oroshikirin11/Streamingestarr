// Thin API layer. Everything is same-origin and cookie-authenticated by
// the viewer gate; a 401 anywhere means the session is gone — go to /login.

async function request(path, options = {}) {
	const res = await fetch(path, options);
	if (res.status === 401) {
		window.location.href = '/login';
		throw new Error('not authenticated');
	}
	if (!res.ok) throw new Error(`${path}: ${res.status}`);
	return res.json();
}

export const getStatus = () => request('/api/status');
export const getConfig = () => request('/api/config');

export const registerChatUser = (displayName) =>
	request('/api/chat/register', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(displayName ? { displayName } : {})
	});

export const getChatHistory = (accessToken) =>
	request(`/api/chat?accessToken=${encodeURIComponent(accessToken)}`);

export async function logout() {
	try {
		await fetch('/api/auth/logout', { method: 'POST' });
	} finally {
		window.location.href = '/login';
	}
}
