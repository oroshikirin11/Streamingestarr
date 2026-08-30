import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		proxy: {
			'/api': 'http://localhost:8080',
			'/hls': 'http://localhost:8080',
			'/logo': 'http://localhost:8080',
			'/thumbnail.jpg': 'http://localhost:8080',
			'/ws': { target: 'ws://localhost:8080', ws: true }
		}
	}
});
