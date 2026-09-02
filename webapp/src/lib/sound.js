// The viewer's sound preference, owned above the player.
//
// GlowFrame remounts whenever the room goes offline and back — which is
// exactly what an episode whose frame shape differs does, since that
// starts a new publish session. Volume and mute lived inside the
// component, so every one of those remounts rebuilt them from defaults:
// the slider snapped back to the 5% first-visit volume, and `muted`
// starting false meant autoplay-with-sound was refused (no fresh gesture
// on a page that has been open a while) and the viewer was dropped to
// muted. Holding the preference at module scope makes it survive
// remounts within the session; localStorage carries it across reloads.
//
// `touched` is the distinction that matters: the quiet default is for
// someone who has never chosen, and must never override someone who has.

const VOL_KEY = 'sgr_volume';
const MUTED_KEY = 'sgr_muted';
const DEFAULT_VOLUME = 0.05;

function read(key) {
	try {
		return localStorage.getItem(key);
	} catch {
		return null; // private mode, blocked storage — memory still works
	}
}

function write(key, value) {
	try {
		localStorage.setItem(key, value);
	} catch {
		/* preference lives for this session only */
	}
}

const storedVolume = read(VOL_KEY);
const storedMuted = read(MUTED_KEY);
// A stored "0" is a real choice (silence), so parse it rather than
// treating every falsy value as "unset" — `Number(x) || DEFAULT` turned
// a deliberate zero back into 5%.
const parsed = storedVolume == null ? NaN : Number(storedVolume);

export const sound = {
	volume: Number.isFinite(parsed) && parsed >= 0 && parsed <= 1 ? parsed : DEFAULT_VOLUME,
	muted: storedMuted === '1',
	/** Has this viewer ever chosen? Only if not do defaults apply. */
	touched: storedVolume != null || storedMuted != null
};

/** An explicit choice: remembered here, in storage, and across remounts. */
export function setSoundVolume(v) {
	sound.volume = v;
	sound.touched = true;
	write(VOL_KEY, String(v));
}

/** An explicit mute choice — NOT an autoplay-forced one, see setForcedMute. */
export function setSoundMuted(m) {
	sound.muted = m;
	sound.touched = true;
	write(MUTED_KEY, m ? '1' : '0');
}

/**
 * The browser refused unmuted autoplay. That is the page's constraint,
 * not the viewer's wish: it must not be remembered, or one refused
 * autoplay would mute every later visit.
 */
export function setForcedMute(m) {
	sound.muted = m;
}
