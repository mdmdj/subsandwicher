const { spawn } = require('child_process');
const net = require('net');
const { log } = require('./logger');
const { getPipePath, argsForLaunch } = require('./ipc-paths');
const { binaries } = require('./bins');

// Small client for mpv's JSON IPC protocol (input IPC server), with the
// request/response and event plumbing we need to steer preview playback.
class MpvSession {
  constructor({ video, mergedSub, seekTo }) {
    this.video = video;
    this.mergedSub = mergedSub;
    this.seekTo = seekTo;
    this.proc = null;
    this.client = null;
    this.pipePath = getPipePath();
    this.reqId = 0;
    this.pending = new Map(); // request_id -> {resolve, reject}
    this.buffer = '';
    this.ready = false;
    this.dead = false;
    this.pendingCommandsOnReady = [];
    this.onLog = (msg) => log('info', 'mpv:', msg);
    this.onExit = null;
  }

  spawn(minimal = false) {
    if (this.proc) throw new Error('already spawned');
    const { mpv } = binaries();
    let args;
    if (minimal) {
      args = [...argsForLaunch(this.pipePath), '--pause', '--keep-open=always', this.video];
    } else {
      args = [
        ...argsForLaunch(this.pipePath),
        '--vo=gpu-next',
        '--hwdec=auto-safe',
        '--keep-open=always',
        '--pause',
        '--sub-auto=no',
        // kiosk-ish behavior: no OSD, no on-screen controller, no default
        // key bindings or mouse actions; our controller drives everything.
        '--osd-level=0',
        '--osc=no',
        '--input-default-bindings=no',
        '--window-dragging=no',
        this.video,
      ];
    }
    log('info', `spawning mpv (minimal=${minimal}):`, mpv, args);
    this.proc = spawn(mpv, args, { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
    this.proc.stdout.on('data', (d) => log('info', 'mpv(stdout):', d.toString().trim()));
    this.proc.stderr.on('data', (d) => log('info', 'mpv(stderr):', d.toString().trim()));
    this.proc.on('error', (e) => { log('error', 'mpv spawn error', e.message); this.ready = false; this.onExit && this.onExit(-1); });
    this.proc.on('exit', (code, signal) => {
      log('info', 'mpv exited', { code, signal });
      this.ready = false;
      this.dead = true;
      this.onExit && this.onExit(code, signal);
    });
    return this.proc;
  }

  async connect(timeoutMs = 8000) {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      try {
        this.client = await tcpConnect(this.pipePath);
        log('info', 'connected to mpv IPC at', this.pipePath);
        break;
      } catch (e) {
        if (Date.now() > deadline) {
          throw new Error(`could not connect to mpv IPC at ${this.pipePath}: ${e.message}`);
        }
        await sleep(200);
      }
    }
    this.client.on('data', (d) => this.onData(d)).setEncoding('utf8');
    this.client.on('error', (e) => log('error', 'mpv IPC error', e.message));
    this.client.on('close', () => log('info', 'mpv IPC closed'));
    // pipeline the post-load commands; exec them once file is loaded
    await this.exec(['sub-add', this.mergedSub, 'select']);
    if (this.seekTo > 0) {
      await this.exec(['seek', this.seekTo, 'absolute+exact']);
    }
    await this.exec(['set', 'pause', 'yes']);
    this.ready = true;
    this.onLog(`mpv session ready (video=${this.video})`);
    while (this.pendingCommandsOnReady.length) {
      const cmd = this.pendingCommandsOnReady.shift();
      await this.exec(cmd);
    }
  }

  onData(chunk) {
    this.buffer += chunk;
    let idx;
    while ((idx = this.buffer.indexOf('\n')) >= 0) {
      const line = this.buffer.slice(0, idx);
      this.buffer = this.buffer.slice(idx + 1);
      if (!line.trim()) continue;
      let msg;
      try { msg = JSON.parse(line); } catch (e) {
        log('error', 'mpv IPC non-JSON:', line.slice(0, 200));
        continue;
      }
      if (msg.request_id && this.pending.has(msg.request_id)) {
        const { resolve, reject } = this.pending.get(msg.request_id);
        this.pending.delete(msg.request_id);
        if (msg.error === 'success' || msg.request_id) resolve(msg);
        else reject(new Error(`mpv error: ${msg.error}`));
      } else if (msg.event) {
        this.onLog(`event ${msg.event}`);
      }
    }
  }

  exec(cmd) {
    const id = ++this.reqId;
    return new Promise((resolve, reject) => {
      const line = { command: cmd, request_id: id };
      const payload = JSON.stringify(line) + '\n';
      if (!this.client) { reject(new Error('not connected')); return; }
      this.pending.set(id, { resolve, reject });
      this.client.write(payload, (err) => { if (err) { this.pending.delete(id); reject(err); } });
      setTimeout(() => {
        if (this.pending.has(id)) {
          this.pending.delete(id);
          reject(new Error(`mpv IPC timeout for ${JSON.stringify(cmd)}`));
        }
      }, 10000);
    });
  }

  seek(t) { return this.exec(['seek', t, 'absolute+exact']); }
  stepFrame(dir = 1) { return this.exec(['frame-step', dir]); }
  anotherRandomFrame(t) { return this.seek(t); }
  isDead() { return this.dead || !this.proc || this.proc.killed; }

  quit() {
    if (this.stopped) return;
    this.dead = true;
    this.stopped = true;
    if (this.client) { try { this.client.end(); } catch {} }
    if (this.proc) { try { this.proc.kill(); } catch {} }
    if (process.platform !== 'win32') {
      try { require('fs').unlinkSync(this.pipePath); } catch {}
    }
  }
}

function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

// net.connect handles both unix sockets and Windows named pipes paths.
function tcpConnect(pipePath) {
  return new Promise((resolve, reject) => {
    const sock = net.connect({ path: pipePath }, () => resolve(sock));
    sock.once('error', (e) => reject(e));
  });
}

async function launchPreviewSession(opts) {
  // First attempt uses the full option set. If mpv exits before we can attach
  // (code != 0 within 3s), retry with a minimal launch: IPC + pause + file
  // only, and apply what we can over IPC. That tells us whether the failure
  // is a command-line option problem or a playback problem.
  const session = new MpvSession(opts);
  session.spawn(false);
  const early = await waitForEarlyExit(session, 3000);
  if (early) {
    log('warn', `mpv exited within 3s with full options (code=${early.code}); retrying minimal args to isolate the problem`);
    session.spawn(true /* minimal */);
    await session.connect();
    for (const [k, v] of [['hwdec', 'auto-safe']]) {
      try { await session.exec(['set', k, v]); } catch (e) { log('warn', `set ${k} failed: ${e.message}`); }
    }
    await session.exec(['set', 'pause', 'yes']);
  } else {
    if (!session.ready) await session.connect();
  }
  return session;
}

// Returns mpv's exit info if the process dies before the timeout, else null
// (i.e. it's still alive and we can connect).
function waitForEarlyExit(session, ms) {
  return new Promise((resolve) => {
    const t = setTimeout(() => resolve(null), ms);
    session.onExit = (code, signal) => { clearTimeout(t); resolve({ code, signal }); };
  });
}

module.exports = { MpvSession, launchPreviewSession };
