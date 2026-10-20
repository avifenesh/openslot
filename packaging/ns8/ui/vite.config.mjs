import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig({
  base: './',
  plugins: [vue({ template: { compilerOptions: { compatConfig: { MODE: 2 } } } })],
  resolve: { extensions: ['.mjs', '.js', '.json', '.vue'], alias: {
    vue: '@vue/compat',
    '@': fileURLToPath(new URL('./src', import.meta.url)),
    crypto: fileURLToPath(new URL('./src/crypto.js', import.meta.url)),
  } },
  css: { preprocessorOptions: { scss: { silenceDeprecations: ['import', 'global-builtin', 'color-functions', 'if-function'] } } },
});
