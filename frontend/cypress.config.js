const { defineConfig } = require('cypress');

module.exports = defineConfig({
  env: {
    apiUrl: 'http://127.0.0.1:9273',
    LISTMONK_ADMIN_USER: 'admin',
    LISTMONK_ADMIN_PASSWORD: 'listmonk-test',
  },
  viewportWidth: 1400,
  viewportHeight: 950,
  e2e: {
    experimentalRunAllSpecs: true,
    testIsolation: false,
    // We've imported your old cypress plugins here.
    // You may want to clean this up later by importing these.
    setupNodeEvents(on, config) {
      return require('./cypress/plugins/index.js')(on, config);
    },
    baseUrl: 'http://127.0.0.1:9273',
  },
});
