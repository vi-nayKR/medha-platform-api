import { execSync } from 'child_process';
import fs from 'fs';
import path from 'path';
import os from 'os';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '..');

console.log('🚀 Starting Medha API setup...');

// Helper to check command availability
function commandExists(cmd) {
  try {
    execSync(os.platform() === 'win32' ? `where ${cmd}` : `command -v ${cmd}`, { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
}

// 1. Verify Go is installed
if (!commandExists('go')) {
  console.log('❌ Go is not installed or not in your PATH.');
  const platform = os.platform();
  if (platform === 'darwin') {
    console.log('👉 Please install it using Homebrew: brew install go');
  } else if (platform === 'linux') {
    console.log('👉 Please install it using apt: sudo apt install -y golang-go');
  } else if (platform === 'win32') {
    console.log('👉 Please download and install Go from: https://go.dev/dl/');
  }
  process.exit(1);
} else {
  console.log('✅ Go is installed.');
}

// 2. Setup .env file
const envPath = path.join(projectRoot, '.env');
const envExamplePath = path.join(projectRoot, '.env.example');

if (!fs.existsSync(envPath)) {
  if (fs.existsSync(envExamplePath)) {
    console.log('📄 Creating .env from .env.example...');
    fs.copyFileSync(envExamplePath, envPath);
  } else {
    console.log('⚠️  .env.example file not found.');
  }
} else {
  console.log('ℹ️  .env already exists.');
}

// 3. Generate JWT keys using the Go script (cross-platform alternative to openssl)
const keysDir = path.join(projectRoot, 'keys');
const privateKeyPath = path.join(keysDir, 'jwt_private.pem');

if (!fs.existsSync(privateKeyPath)) {
  console.log('🔑 Generating JWT RS256 keys...');
  try {
    execSync('go run cmd/genkeys/main.go', { cwd: projectRoot, stdio: 'inherit' });
  } catch (error) {
    console.error('❌ Failed to generate JWT keys:', error.message);
  }
} else {
  console.log('ℹ️  JWT keys already exist in keys/ folder.');
}

// 4. Install Go tools
console.log('🛠️  Installing Go tools (goose, air, swag)...');
try {
  execSync('go install github.com/pressly/goose/v3/cmd/goose@latest', { stdio: 'inherit' });
  execSync('go install github.com/air-verse/air@latest', { stdio: 'inherit' });
  execSync('go install github.com/swaggo/swag/cmd/swag@latest', { stdio: 'inherit' });
  console.log('✅ Go tools installed/updated successfully.');
} catch (error) {
  console.error('⚠️  Some Go tools failed to install. Make sure your Go environment path is configured.', error.message);
}

console.log('\n✅ Setup complete! You are ready to go.');
console.log('👉 To start local Docker services and run the server, use your Makefile commands or:');
console.log('   go run cmd/medha-api/main.go');
console.log('👉 Explore the API documentation at: http://localhost:8080/api/docs');
