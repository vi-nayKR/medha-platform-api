import { spawn, execSync } from 'child_process';
import net from 'net';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '..');

const remoteAlias = 'medha-server';

console.log('\x1b[36m>>> Starting Medha API with REMOTE DATA (SSH Tunnel)...\x1b[0m\n');

// Step 0: Ensure local Docker services are down
console.log('\x1b[33m[0/3] Stopping local Docker services to free ports...\x1b[0m');
try {
  execSync('make services-down', { cwd: projectRoot, stdio: 'ignore' });
} catch {
  // Ignore errors if make services-down fails
}

// Step 1: Establish SSH Tunnel
console.log(`\x1b[33m[1/3] Establishing SSH tunnel (5432, 6379, 9000, 9001) using: ${remoteAlias}...\x1b[0m`);

const sshArgs = [
  '-L', '5432:localhost:5432',
  '-L', '6379:localhost:6379',
  '-L', '9000:localhost:9000',
  '-L', '9001:localhost:9001',
  remoteAlias,
  '-N'
];

const sshProcess = spawn('ssh', sshArgs, { stdio: 'pipe' });

// Ensure SSH tunnel is terminated when Node exits
const cleanup = () => {
  if (sshProcess && !sshProcess.killed) {
    console.log('\n\x1b[33mStopping SSH tunnel...\x1b[0m');
    sshProcess.kill('SIGINT');
  }
};

process.on('exit', cleanup);
process.on('SIGINT', () => process.exit(0));
process.on('SIGTERM', () => process.exit(0));
process.on('uncaughtException', (err) => {
  console.error(err);
  process.exit(1);
});

// Helper to probe a port
function checkPort(port, host = 'localhost', timeout = 1000) {
  return new Promise((resolve) => {
    const socket = new net.Socket();
    let status = false;
    socket.setTimeout(timeout);
    
    socket.once('connect', () => {
      status = true;
      socket.destroy();
    });
    socket.once('timeout', () => socket.destroy());
    socket.once('error', () => socket.destroy());
    socket.once('close', () => resolve(status));
    
    socket.connect(port, host);
  });
}

// Step 2: Verify ports are open
async function verifyTunnels() {
  console.log('\x1b[33m[2/3] Waiting for tunnel connection and verifying ports...\x1b[0m');
  
  // Wait 5 seconds for tunnel connection
  await new Promise(r => setTimeout(r, 5000));

  const postgresOk = await checkPort(5432);
  const redisOk = await checkPort(6379);
  const s3Ok = await checkPort(8333);

  if (!postgresOk) {
    console.error('\x1b[31m❌ Postgres tunnel failed (port 5432 is not reachable)\x1b[0m');
    process.exit(1);
  }
  if (!redisOk) {
    console.error('\x1b[31m❌ Redis tunnel failed (port 6379 is not reachable)\x1b[0m');
    process.exit(1);
  }
  if (!s3Ok) {
    console.error('\x1b[31m❌ S3 storage tunnel failed (port 8333 is not reachable)\x1b[0m');
    process.exit(1);
  }

  console.log('\x1b[32m      ✅ All SSH tunnels verified and active.\x1b[0m');
}

// Step 3: Start local Go API pointing to tunnels
async function startAPI() {
  await verifyTunnels();
  
  if (!fs.existsSync(path.join(projectRoot, '.env'))) {
    console.error('\x1b[31m❌ .env file not found.\x1b[0m');
    process.exit(1);
  }

  console.log('\x1b[33m[3/3] Starting API server pointing to remote data...\x1b[0m');

  // Check if air is installed, otherwise fallback to go run
  let hasAir = false;
  try {
    execSync(process.platform === 'win32' ? 'where air' : 'command -v air', { stdio: 'ignore' });
    hasAir = true;
  } catch {}

  const cmd = hasAir ? 'air' : 'go run cmd/medha-api/main.go';
  
  try {
    execSync(cmd, { cwd: projectRoot, stdio: 'inherit' });
  } catch (error) {
    console.error('\x1b[31m❌ API Server stopped:\x1b[0m', error.message);
  }
}

startAPI();
