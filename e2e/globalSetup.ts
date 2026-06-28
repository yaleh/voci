import { spawn, execSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';

const URL_FILE = path.join(os.tmpdir(), 'voci-playwright-url.txt');
const DONE_FILE = path.join(os.tmpdir(), 'voci-playwright-done.txt');
const PID_FILE = path.join(os.tmpdir(), 'voci-playwright-pid.txt');

export default async function globalSetup() {
  // Clean up any leftover files from previous runs
  for (const f of [URL_FILE, DONE_FILE, PID_FILE]) {
    if (fs.existsSync(f)) fs.unlinkSync(f);
  }

  const projectRoot = path.resolve(__dirname, '..');

  // Start the Go playwright setup server as a background process
  const proc = spawn(
    'go', ['test', '-v', '-count=1', '-run', 'TestPlaywrightSetup', '-tags', 'playwright', '-timeout', '10m', './internal/daemon/'],
    {
      cwd: projectRoot,
      env: {
        ...process.env,
        PLAYWRIGHT_URL_FILE: URL_FILE,
        PLAYWRIGHT_DONE_FILE: DONE_FILE,
      },
      detached: false,
      stdio: ['ignore', 'pipe', 'pipe'],
    }
  );

  proc.on('exit', (code: number | null) => {
    if (code !== null && code !== 0) console.error(`[globalSetup] Go test process exited with code ${code}`);
  });

  fs.writeFileSync(PID_FILE, String(proc.pid));

  // Store for teardown
  (global as any).__PLAYWRIGHT_GO_PROC__ = proc;
  (global as any).__PLAYWRIGHT_DONE_FILE__ = DONE_FILE;

  // Wait for URL file to appear (Go server started)
  const deadline = Date.now() + 30_000;
  while (!fs.existsSync(URL_FILE) && Date.now() < deadline) {
    await new Promise(r => setTimeout(r, 200));
  }

  if (!fs.existsSync(URL_FILE)) {
    proc.kill();
    throw new Error('Go playwright server did not start within 30s');
  }

  const baseURL = fs.readFileSync(URL_FILE, 'utf8').trim();
  process.env.BASE_URL = baseURL;
  console.log(`\n[globalSetup] Go server started at ${baseURL}`);
}
