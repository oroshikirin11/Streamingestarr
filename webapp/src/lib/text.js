export function dedupeSubtitle(title, subtitle) {
	if (!subtitle) return '';
	if (!title) return subtitle;
	const t = title.trim();
	let s = subtitle.trim();
	if (s.toLowerCase().startsWith(t.toLowerCase())) {
		s = s.slice(t.length).replace(/^[\s\u2014\u2013:·,-]+/, '');
	}
	return s;
}
