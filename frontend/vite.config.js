import vue from '@vitejs/plugin-vue2';
import { defineConfig, loadEnv } from 'vite';

const path = require('path');

// https://vitejs.dev/config/
export default defineConfig(({ _, mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  return {
    plugins: [vue()],
    base: '/admin',
    mode,
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
        bulma: require.resolve('bulma/bulma.sass'),
      },
    },
    build: {
      assetsDir: 'static',
    },
    server: {
      port: env.LISTMONK_FRONTEND_PORT || 8080,
      // Docker Desktop bind mounts on Windows can miss native file-change events.
      // Keep polling opt-in so native local development does not pay that cost.
      watch: env.LISTMONK_WATCH_POLLING === 'true' ? { usePolling: true, interval: 500 } : undefined,
      proxy: {
        '^/$': {
          target: env.LISTMONK_API_URL || 'http://127.0.0.1:9173',
        },
        '^/(api|webhooks|subscription|public|health)': {
          target: env.LISTMONK_API_URL || 'http://127.0.0.1:9173',
        },
        '^/admin/login': {
          target: env.LISTMONK_API_URL || 'http://127.0.0.1:9173',
        },
        // These pages are rendered by Go and are part of the authentication
        // flow. Keeping them on the backend preserves workspace selection and
        // two-factor redirects when the admin UI is served by Vite.
        '^/admin/select-workspace': {
          target: env.LISTMONK_API_URL || 'http://127.0.0.1:9173',
        },
        '^/admin/(forgot|reset)': {
          target: env.LISTMONK_API_URL || 'http://127.0.0.1:9173',
        },
        '^/auth/oidc': {
          target: env.LISTMONK_API_URL || 'http://127.0.0.1:9173',
        },
        '^/(admin\/custom\.(css|js))': {
          target: env.LISTMONK_API_URL || 'http://127.0.0.1:9173',
        },
      },
    },
  };
});
