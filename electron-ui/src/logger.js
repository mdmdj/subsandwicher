const fs = require('fs');
const path = require('path');
const { app } = require('electron');

let logDir = null;
let logPath = null;
let logStream = null;

function pad(n) { return String(n).padStart(2, '0'); }

function initLog(userDataDir) {
  logDir = path.join(userDataDir, 'logs');
  try {
    fs.mkdirSync(logDir, { recursive: true });
  } catch (e) {
    console.error('failed to create log dir', e);
    return path.join(process.cwd(), 'subsandwicher-gui.log');
  }
  const d = new Date();
  const stamp = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}-${pad(d.getMinutes())}-${pad(d.getSeconds())}`;
  logPath = path.join(logDir, `${stamp}-subsandwicher-gui.log`);
  logStream = fs.createWriteStream(logPath, { flags: 'a' });
  log('info', `log started: ${logPath}`);
  return logPath;
}

function ts() { return new Date().toISOString(); }

function log(level, ...parts) {
  const line = `[${ts()}] [${level}] ${parts.map(p => (typeof p === 'string' ? p : JSON.stringify(p))).join(' ')}`;
  console.log(line);
  if (logStream) {
    logStream.write(line + '\n');
  } else {
    console.log('(pre-init)', line);
  }
}

function currentLogPath() { return logPath; }
function logDirectory() { return logDir; }

module.exports = { initLog, log, logPath: currentLogPath, logDirectory };
