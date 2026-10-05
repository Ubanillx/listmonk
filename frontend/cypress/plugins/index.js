const { execFileSync } = require('child_process');
const path = require('path');
const http = require('http');

const root = path.resolve(__dirname, '../../..');
const compose = ['compose', '-p', 'listmonk-cypress', '-f', 'dev/cypress-compose.yml'];
const testURL = 'http://127.0.0.1:9273';

function docker(...args) {
  return execFileSync('docker', [...compose, ...args], { cwd: root, encoding: 'utf8' });
}

function ready() {
  return new Promise((resolve) => {
    http.get(`${testURL}/admin/login`, (response) => {
      response.resume();
      resolve(response.statusCode === 200);
    }).on('error', () => resolve(false));
  });
}

async function waitForBackend() {
  for (let attempt = 0; attempt < 90; attempt += 1) {
    if (await ready()) return;
    await new Promise((resolve) => setTimeout(resolve, 2000));
  }
  throw new Error('Isolated Cypress backend did not become healthy at port 9273');
}

module.exports = (on, config) => {
  on('task', {
    async resetDatabase({ blank = false } = {}) {
      if (process.env.LISTMONK_CYPRESS_ISOLATED !== '1'
        || config.baseUrl !== testURL || config.env.apiUrl !== testURL) {
        throw new Error('Database reset requires the isolated Cypress runner and port 9273');
      }

      // Verify the live connection before executing destructive SQL. This
      // Compose project has no shared development volume or published DB port.
      const database = docker('exec', '-T', 'db', 'psql', '-U', 'listmonk_cypress',
        '-d', 'listmonk_cypress', '-Atc', 'SELECT current_database()').trim();
      if (database !== 'listmonk_cypress') throw new Error(`Unexpected Cypress database: ${database}`);

      docker('stop', 'backend');
      docker('exec', '-T', 'db', 'psql', '-U', 'listmonk_cypress', '-d', 'listmonk_cypress',
        '-v', 'ON_ERROR_STOP=1', '-c', 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;');
      const adminUser = blank ? '' : 'admin';
      if (process.env.CYPRESS_TEST_ADMIN_USER !== adminUser) {
        process.env.CYPRESS_TEST_ADMIN_USER = adminUser;
        docker('up', '-d', '--force-recreate', 'backend');
      } else {
        docker('start', 'backend');
      }
      await waitForBackend();
      return null;
    },
  });
  return config;
};
