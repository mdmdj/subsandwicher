const path = require('path');
const os = require('os');

// IPC server address across platforms. The same value must be passed to mpv
// via --input-ipc-server and used by net.connect(path).
//
// The name is unique per app instance (pid) and per session (random), so any
// number of app copies — or unrelated mpv instances running elsewhere — can
// never contend on the same pipe/socket. On Windows named pipes disappear
// entirely with the creating (mpv) process, so a stale name is impossible;
// on unix we additionally clean up the socket when we quit.
let seq = 0;

function getPipePath() {
  seq++;
  const uniq = `${process.pid}-${seq}-${Math.random().toString(36).slice(2, 8)}`;
  if (process.platform === 'win32') {
    return `\\\\.\\pipe\\subsandwicher-mpv-${uniq}`;
  }
  return path.join(os.tmpdir(), `subsandwicher-mpv-${uniq}.sock`);
}

// Args evaluated before the pipe exists: mpv creates the IPC server in the
// named pipe before we connect. The "=" form is used because some mpv builds
// reject the space-separated form for this option on Windows.
function argsForLaunch(pipePath) {
  return [
    '--no-config',                      // full control: ignore user mpv profiles
    `--input-ipc-server=${pipePath}`,   // our control channel
  ];
}

module.exports = { getPipePath, argsForLaunch };
