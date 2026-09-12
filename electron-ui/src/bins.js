const path = require('path');
const fs = require('fs');
const { log } = require('./logger');

// The packaged dist layout places the externals next to the electron exe:
//   SubSandwicher/
//     SubSandwicher.exe (electron)
//     subsandwicher-cli.exe
//     ffmpeg.exe ffprobe.exe mpv.exe
// Dev runs also fall back to ../vendor/bin/<os>/ or PATH.
function exeName(base, isWin) {
  const p = process.platform;
  if (p === 'win32' || isWin) return `${base}.exe`;
  return base;
}

// Candidate locations, in order. In dev, app.getAppPath() points at electron-ui/.
function fileCandidates(base, isWin) {
  const name = exeName(base, isWin);
  return [
    appRoot(),
    appRoot('third_party'),
    appRoot('third_party/bin'),
    path.join(appRoot(), '..'),
    path.join(appRoot(), '..', 'third_party'),
    path.join(appRoot(), '..', 'third_party', 'bin'),
    path.join(appRoot(), 'third_party'),
    path.join(appRoot(), 'third_party', 'bin'),
    path.dirname(process.execPath),
  ].filter(Boolean);
}

let cached = null;

// This file lives at resources/app/src/bins.js in the packaged dist, so the
// app root (which contains third_party/bin and subsandwicher-cli.exe) is one
// level up.
function appRoot() { return path.join(__dirname, '..'); }

function findBinary(name, isWin = false) {
  const envOverride = process.env['SUBS_BIN_' + name.toUpperCase().replace(/-/g, '_')];
  if (envOverride) return envOverride;
  const nameExe = exeName(name, isWin);
  const roots = [appRoot(), path.dirname(process.execPath), path.join(appRoot(), '..', 'third_party', 'bin'), path.join(appRoot(), 'third_party', 'bin')];
  for (const root of roots) {
    for (const sub of ['', 'windows']) {
      const cand = path.join(root, sub, nameExe);
      if (fs.existsSync(cand)) return cand;
    }
  }
  // fall back to PATH (works for dev on linux, or system-wide installs)
  return nameExe;
}

function resolveBinaries() {
  cached = {
    ffmpeg: findBinary('ffmpeg'),
    ffprobe: findBinary('ffprobe'),
    mpv: findBinary('mpv'),
    cli: findBinary('subsandwicher-cli'),
  };
  log('info', 'resolved binaries', cached);
  return cached;
}

function binaries() { return cached || resolveBinaries(); }

module.exports = { resolveBinaries, binaries, findBinary, exeName };
