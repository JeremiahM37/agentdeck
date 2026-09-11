import {defineConfig} from 'vite';
export default defineConfig({build:{rollupOptions:{input:{board:'board-harness.html',sessions:'sessions-harness.html',settings:'settings-harness.html'}}}});
