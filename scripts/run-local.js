import { execSync } from 'child_process';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '..');

console.log('\x1b[34m>>> Starting Medha API Local Dev Environment...\x1b[0m\n');

try {
  // 1. Start Docker services (PostgreSQL + Redis)
  console.log('\x1b[33m[1/4] Starting Docker Services...\x1b[0m');
  execSync('make services-up', { cwd: projectRoot, stdio: 'inherit' });

  // 2. Sync Environment Variables
  console.log('\x1b[33m[2/4] Syncing environment from medha-infra...\x1b[0m');
  execSync('make local-setup', { cwd: projectRoot, stdio: 'inherit' });

  // Check if .env was created
  const envPath = path.join(projectRoot, '.env');
  if (!fs.existsSync(envPath)) {
    console.error('\x1b[31mError: .env file was not created. Check your medha-infra directory path.\x1b[0m');
    process.exit(1);
  }

  // Parse .env to extract DATABASE_URL
  const envContent = fs.readFileSync(envPath, 'utf8');
  let databaseUrl = '';
  const lines = envContent.split('\n');
  for (const line of lines) {
    if (line.trim().startsWith('DATABASE_URL=')) {
      databaseUrl = line.split('DATABASE_URL=')[1].trim();
      // Remove surrounding quotes if present
      if ((databaseUrl.startsWith('"') && databaseUrl.endsWith('"')) || 
          (databaseUrl.startsWith("'") && databaseUrl.endsWith("'"))) {
        databaseUrl = databaseUrl.slice(1, -1);
      }
      break;
    }
  }

  if (!databaseUrl) {
    console.error('\x1b[31mError: DATABASE_URL not found in .env file.\x1b[0m');
    process.exit(1);
  }

  // 3. Run Database Migrations
  console.log('\x1b[33m[3/4] Running DB Migrations...\x1b[0m');
  execSync(`goose -dir migrations postgres "${databaseUrl}" up`, { cwd: projectRoot, stdio: 'inherit' });

  // 4. Final Verification and Start
  console.log('\x1b[33m[4/4] Starting API with hot-reload (air)...\x1b[0m');
  console.log('\x1b[32m>>> API will be available at: http://localhost:8082\x1b[0m');
  console.log('\x1b[32m>>> Swagger UI: http://localhost:8082/api/docs\x1b[0m\n');

  // Check if air is installed, otherwise fallback to go run
  let hasAir = false;
  try {
    execSync(process.platform === 'win32' ? 'where air' : 'command -v air', { stdio: 'ignore' });
    hasAir = true;
  } catch {}

  const cmd = hasAir ? 'air' : 'go run cmd/medha-api/main.go';
  execSync(cmd, { cwd: projectRoot, stdio: 'inherit' });

} catch (error) {
  console.error('\x1b[31m❌ Local start script failed:\x1b[0m', error.message);
  process.exit(1);
}
