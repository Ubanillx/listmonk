// Run from the repository root: node dev/run-cypress.js --spec cypress/e2e/customer-lists.cy.js
const { spawnSync } = require('child_process');
const path = require('path');

const root = path.resolve(__dirname, '..');
const frontend = path.join(root, 'frontend');
const compose = ['compose', '-p', 'listmonk-cypress', '-f', 'dev/cypress-compose.yml'];

function run(command, args, cwd = root) {
  const result = spawnSync(command, args, { cwd, stdio: 'inherit' });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} exited with ${result.status}`);
}

function waitForBackend() {
  for (let attempt = 0; attempt < 120; attempt += 1) {
    const curl = process.platform === 'win32' ? 'curl.exe' : 'curl';
    const check = spawnSync(curl, ['-fsS', '--max-time', '2', 'http://127.0.0.1:9273/admin/login'], {
      cwd: root, stdio: 'ignore',
    });
    if (check.status === 0) return;
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 2000);
  }
  throw new Error('Isolated Cypress backend did not become healthy at port 9273');
}

try {
  run(process.execPath, ['node_modules/eslint/bin/eslint.js', '--ext', '.js,.vue', '--ignore-path', '.gitignore', 'src'], frontend);
  run(process.execPath, ['node_modules/vite/bin/vite.js', 'build'], frontend);
  process.env.CYPRESS_TEST_ADMIN_USER = 'admin';
  run('docker', [...compose, 'up', '-d', '--build']);
  waitForBackend();
  process.env.LISTMONK_CYPRESS_ISOLATED = '1';
  delete process.env.CYPRESS_BASE_URL;
  delete process.env.CYPRESS_apiUrl;
  run(process.execPath, ['node_modules/cypress/bin/cypress', 'run', '--browser', 'electron', ...process.argv.slice(2)], frontend);
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
} finally {
  run('docker', [...compose, 'down', '--volumes', '--remove-orphans']);
}
