import { createRequire } from 'node:module';

const require = createRequire(new URL('./web/package.json', import.meta.url));
const globals = require('globals');

export default [
  {
    files: ['internal/web/static/js/app*.js'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        ...globals.browser,
        Alpine: 'readonly',
        echarts: 'readonly',
        htmx: 'readonly',
      },
    },
    rules: {
      'no-undef': 'error',
    },
  },
];
