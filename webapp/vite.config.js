import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// Dev proxy target — override with SGR_BACKEND when the Go server runs
// on a non-default port (SGR_BACKEND=http://localhost:8098 npm run dev).
const backend = process.env.SGR_BACKEND ?? 'http://localhost:8080';
const wsBackend = backend.replace(/^http/, 'ws');

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		proxy: {
			'/api': backend,
			'/hls': backend,
			'/artwork': backend,
			'/logo': backend,
			'/thumbnail.jpg': backend,
			'/ws': { target: wsBackend, ws: true }
		}
	}
});
