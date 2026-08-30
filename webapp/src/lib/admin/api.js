// Admin API layer. Same-origin, admin-session cookie. The {value: x}
// envelope is the inherited config-setter convention.

async function req(path, options = {}) {
	const res = await fetch(path, options);
	if (res.status === 401) {
		window.location.href = '/login';
		throw new Error('not authenticated');
	}
	const body = await res.json().catch(() => ({}));
	if (!res.ok && body.message === undefined) throw new Error(`${path}: ${res.status}`);
	return body;
}

const post = (path, payload) =>
	req(path, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(payload)
	});

export const getServerConfig = () => req('/api/admin/serverconfig');
export const getAdminStatus = () => req('/api/admin/status');
export const getChatMessages = () => req('/api/admin/chat/messages');
export const getDisabledUsers = () => req('/api/admin/chat/users/disabled');
export const getConnectedClients = () => req('/api/admin/chat/clients');

export const setConfigValue = (path, value) => post(`/api/admin/config/${path}`, { value });
export const setStreamKeys = (keys) => post('/api/admin/config/streamkeys', { value: keys });
export const setOutputVariants = (variants) =>
	post('/api/admin/config/video/streamoutputvariants', { value: variants });
export const setAdminPassword = (value) => post('/api/admin/config/adminpass', { value });
export const setRoomPassword = (viewerPassword) =>
	post('/api/admin/config/viewerlogin', { viewerPassword });
export const setNameReservationDays = (value) =>
	post('/api/admin/config/chat/namereservationdays', { value });
export const setSegmentFormat = (value) => post('/api/admin/config/video/segmentformat', { value });
export const disconnectStream = () => post('/api/admin/disconnect', {});
export const setMessageVisibility = (idArray, visible) =>
	post('/api/admin/chat/messagevisibility', { idArray, visible });
export const setUserEnabled = (userId, enabled) =>
	post('/api/admin/chat/users/setenabled', { userId, enabled });
export const setSRTEnabled = (value) => post('/api/admin/config/srt/enabled', { value });
export const setSRTPort = (value) => post('/api/admin/config/srt/port', { value });
