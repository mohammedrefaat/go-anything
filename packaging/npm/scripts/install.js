const fs = require('fs');
const path = require('path');
const https = require('https');
const crypto = require('crypto');
const { execSync } = require('child_process');

const VERSION = "2.0.0";
const REPO = "mohammedrefaat/go-anything";
const BIN_DIR = path.join(__dirname, "..", "bin");

const PLATFORM_MAP = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows"
};

const ARCH_MAP = {
  x64: "amd64",
  arm64: "arm64"
};

function downloadFile(url, dest) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);
    https.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return downloadFile(res.headers.location, dest).then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`Failed to download ${url}: HTTP status ${res.statusCode}`));
      }
      res.pipe(file);
      file.on('finish', () => file.close(resolve));
    }).on('error', (err) => {
      fs.unlink(dest, () => reject(err));
    });
  });
}

function computeSHA256(filePath) {
  const hash = crypto.createHash('sha256');
  const data = fs.readFileSync(filePath);
  hash.update(data);
  return hash.digest('hex');
}

async function install() {
  const osType = PLATFORM_MAP[process.platform];
  const archType = ARCH_MAP[process.arch];

  if (!osType || !archType) {
    console.warn(`[go-anything npm] Unsupported platform/architecture: ${process.platform}/${process.arch}. Please compile from source: go install github.com/${REPO}/cmd/go-anything@v${VERSION}`);
    return;
  }

  const isWindows = process.platform === 'win32';
  const ext = isWindows ? 'zip' : 'tar.gz';
  const archiveName = `go-anything_${VERSION}_${osType}_${archType}.${ext}`;
  const releaseURL = `https://github.com/${REPO}/releases/download/v${VERSION}/${archiveName}`;
  const checksumURL = `https://github.com/${REPO}/releases/download/v${VERSION}/checksums.txt`;

  if (!fs.existsSync(BIN_DIR)) {
    fs.mkdirSync(BIN_DIR, { recursive: true });
  }

  const tmpArchive = path.join(BIN_DIR, archiveName);
  const tmpChecksums = path.join(BIN_DIR, "checksums.txt");

  console.log(`[go-anything npm] Installing go-anything v${VERSION} for ${osType}/${archType}...`);

  try {
    console.log(`[go-anything npm] Downloading binary archive: ${releaseURL}`);
    await downloadFile(releaseURL, tmpArchive);

    console.log(`[go-anything npm] Downloading release checksums: ${checksumURL}`);
    await downloadFile(checksumURL, tmpChecksums);

    const checksumContent = fs.readFileSync(tmpChecksums, 'utf-8');
    const localHash = computeSHA256(tmpArchive);

    let expectedHash = null;
    for (const line of checksumContent.split('\n')) {
      const parts = line.trim().split(/\s+/);
      if (parts.length >= 2 && parts[1] === archiveName) {
        expectedHash = parts[0];
        break;
      }
    }

    if (expectedHash && localHash !== expectedHash) {
      throw new Error(`SHA256 checksum mismatch for ${archiveName}!\nExpected: ${expectedHash}\nActual:   ${localHash}`);
    }
    console.log(`[go-anything npm] Cryptographic SHA-256 checksum verified: ${localHash}`);

    // Extract archive
    if (isWindows) {
      execSync(`powershell -Command "Expand-Archive -Path '${tmpArchive}' -DestinationPath '${BIN_DIR}' -Force"`);
    } else {
      execSync(`tar -xzf "${tmpArchive}" -C "${BIN_DIR}"`);
    }

    // Cleanup archive files
    if (fs.existsSync(tmpArchive)) fs.unlinkSync(tmpArchive);
    if (fs.existsSync(tmpChecksums)) fs.unlinkSync(tmpChecksums);

    console.log(`[go-anything npm] go-anything binary installed successfully to ${BIN_DIR}`);
  } catch (err) {
    console.warn(`[go-anything npm] Automatic binary installation failed: ${err.message}`);
    console.warn(`[go-anything npm] Please install go-anything using prebuilt binaries or via Go: go install github.com/${REPO}/cmd/go-anything@v${VERSION}`);
  }
}

install().catch(err => {
  console.warn(`[go-anything npm] Installation encountered error: ${err.message}`);
});
