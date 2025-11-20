#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const https = require('https');
const { spawn } = require('child_process');
const os = require('os');

const REPO = 'fadlee/gowa-manager';
const CACHE_DIR = process.env.LOCALAPPDATA 
  ? path.join(process.env.LOCALAPPDATA, 'gowa-manager') 
  : path.join(os.homedir(), '.cache', 'gowa-manager');

const VERSION_FILE = path.join(CACHE_DIR, 'version.txt');

// Map platform to binary name
const getBinaryName = () => {
  const platform = process.platform;
  const arch = process.arch;

  if (platform === 'linux') {
    if (arch === 'x64') return 'gowa-manager-linux-x64';
    if (arch === 'arm64') return 'gowa-manager-linux-arm64';
  } else if (platform === 'darwin') {
    if (arch === 'x64') return 'gowa-manager-macos-x64';
    if (arch === 'arm64') return 'gowa-manager-macos-arm64';
  } else if (platform === 'win32') {
    if (arch === 'x64') return 'gowa-manager-windows-x64.exe';
  }
  
  throw new Error(`Unsupported platform: ${platform} ${arch}`);
};

const BINARY_NAME = getBinaryName();
const BINARY_PATH = path.join(CACHE_DIR, BINARY_NAME);

function getLatestVersion() {
  return new Promise((resolve, reject) => {
    const options = {
      hostname: 'github.com',
      path: `/${REPO}/releases/latest`,
      method: 'HEAD',
      headers: { 'User-Agent': 'Node.js' }
    };

    const req = https.request(options, (res) => {
      if (res.statusCode === 302) {
        const location = res.headers.location;
        const version = location.split('/').pop();
        resolve(version);
      } else {
        reject(new Error(`Failed to get latest version: ${res.statusCode}`));
      }
    });

    req.on('error', reject);
    req.end();
  });
}

function downloadBinary(version) {
  console.log(`Downloading ${BINARY_NAME} (${version})...`);
  if (!fs.existsSync(CACHE_DIR)) {
    fs.mkdirSync(CACHE_DIR, { recursive: true });
  }

  const url = `https://github.com/${REPO}/releases/download/${version}/${BINARY_NAME}`;
  const file = fs.createWriteStream(BINARY_PATH);

  return new Promise((resolve, reject) => {
    const handleResponse = (res) => {
      if (res.statusCode !== 200) {
        reject(new Error(`Failed to download binary: ${res.statusCode}`));
        return;
      }

      const totalLength = parseInt(res.headers['content-length'], 10);
      let downloaded = 0;
      let lastLogged = 0;

      res.on('data', (chunk) => {
        downloaded += chunk.length;
        if (!isNaN(totalLength)) {
          const percent = ((downloaded / totalLength) * 100).toFixed(1);
          // Update only if changed significantly to avoid console spam or every chunk
          if (Date.now() - lastLogged > 100 || downloaded === totalLength) {
             process.stdout.write(`\rProgress: ${percent}% (${(downloaded / 1024 / 1024).toFixed(2)} MB)`);
             lastLogged = Date.now();
          }
        } else {
           process.stdout.write(`\rDownloaded: ${(downloaded / 1024 / 1024).toFixed(2)} MB`);
        }
      });

      res.pipe(file);
      file.on('finish', () => {
        file.close(() => {
          process.stdout.write('\n'); // New line after progress
          if (process.platform !== 'win32') {
            fs.chmodSync(BINARY_PATH, 0o755);
          }
          fs.writeFileSync(VERSION_FILE, version);
          console.log('Download complete.');
          resolve();
        });
      });
    };

    https.get(url, (res) => {
      if (res.statusCode === 302) {
        // Handle redirect
        https.get(res.headers.location, handleResponse).on('error', reject);
      } else {
        handleResponse(res);
      }
    }).on('error', reject);
  });
}

async function main() {
  try {
    // 1. Check latest version
    let latestVersion;
    try {
      latestVersion = await getLatestVersion();
    } catch (e) {
      // If check fails (offline?), try to use cached version if available
      console.warn('Could not check for updates:', e.message);
      if (fs.existsSync(BINARY_PATH)) {
        console.log('Using cached binary.');
        runBinary();
        return;
      }
      throw e;
    }

    // 2. Check local version
    let localVersion = null;
    if (fs.existsSync(VERSION_FILE)) {
      localVersion = fs.readFileSync(VERSION_FILE, 'utf8').trim();
    }

    // 3. Download if needed
    if (!fs.existsSync(BINARY_PATH) || localVersion !== latestVersion) {
      if (localVersion && localVersion !== latestVersion) {
        console.log(`New version available: ${latestVersion} (current: ${localVersion})`);
      }
      await downloadBinary(latestVersion);
    }

    // 4. Run
    runBinary();

  } catch (error) {
    console.error('Error:', error.message);
    process.exit(1);
  }
}

function runBinary() {
  const args = process.argv.slice(2);
  const child = spawn(BINARY_PATH, args, { stdio: 'inherit' });
  
  child.on('close', (code) => {
    process.exit(code);
  });
  
  child.on('error', (err) => {
    console.error('Failed to start subprocess:', err);
    process.exit(1);
  });
}

main();
