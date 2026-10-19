import { defineConfig } from '#q-app';

export default defineConfig(() => {
  return {
    boot: [],
    css: ['app.css', 'ide.scss'],
    extras: ['material-icons'],
    build: {
      target: {},
      typescript: { strict: true, vueShim: true },
      vueRouterMode: 'history',
      vueRouterBase: '/',
      publicPath: '/',
      // the Go binary embeds this directory (web/embed/embed.go: //go:embed all:dist)
      distDir: 'embed/dist',
      allowOutsideProjectDistDir: true,
    },
    devServer: {
      open: false,
      port: 9000,
      proxy: {
        '/api': { target: 'http://localhost:8738', ws: true, changeOrigin: true },
        '/mcp': { target: 'http://localhost:8738', changeOrigin: true },
      },
    },
    framework: {
      config: { dark: 'auto' },
      plugins: ['Dark', 'Notify', 'Dialog'],
    },
    animations: [],
  };
});
