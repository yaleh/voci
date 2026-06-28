import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

const DONE_FILE = path.join(os.tmpdir(), 'voci-playwright-done.txt');

export default async function globalTeardown() {
  // Signal the Go process to exit
  fs.writeFileSync(DONE_FILE, 'done');

  const proc = (global as any).__PLAYWRIGHT_GO_PROC__;
  if (proc) {
    // Give Go process a moment to see the done file
    await new Promise(r => setTimeout(r, 1000));
    try {
      proc.kill('SIGTERM');
    } catch (_) {
      // already exited
    }
  }

  console.log('\n[globalTeardown] Go server stopped');
}
