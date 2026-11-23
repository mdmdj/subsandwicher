const { app, BrowserWindow, ipcMain, shell, Menu, screen, clipboard } = require('electron');
const path = require('path');
const fs = require('fs');
const os = require('os');
const { spawn } = require('child_process');
const { initLog, log, logPath } = require('./logger');
const { resolveBinaries, binaries } = require('./bins');
const { launchPreviewSession } = require('./mpv-ipc');

let mainWindow = null;
let activeSession = null;
let tempDir = null;
let previewCtx = null; // { videoPath, mergedPath } for auto-relaunch after mpv death
const previewBoxCache = new Map(); // path|mtime|time -> current libass boxes
let activeMergeProcess = null; // cli process for the running merge
let playbackTimer = null; // pauses a non-looping window at its end
let playbackClock = null; // polls mpv time-pos while a conflict window plays
let playbackRange = null; // { start, end } for the active window loop
let playbackTickBusy = false;

const dragScriptPath = path.join(__dirname, 'mpv-drag.lua');

function appBuildInfo() {
  const info = { version: app.getVersion(), commit: null, buildNumber: null, buildTime: null };
  try {
    const pkg = require(path.join(__dirname, '..', 'package.json'));
    if (pkg && pkg.buildCommit) info.commit = String(pkg.buildCommit);
    if (pkg && Number.isFinite(Number(pkg.buildNumber))) info.buildNumber = Number(pkg.buildNumber);
    if (pkg && pkg.buildTime) info.buildTime = String(pkg.buildTime);
  } catch { /* dev tree without injected build metadata */ }
  return info;
}

function ensureWindow() {
  if (mainWindow && !mainWindow.isDestroyed()) return mainWindow;
  mainWindow = new BrowserWindow({
    width: 1120,
    height: 760,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
    },
  });
  mainWindow.loadFile(path.join(__dirname, 'renderer', 'index.html'));
  mainWindow.setTitle('SubSandwicher');
  mainWindow.webContents.on('did-finish-load', () => {
    if (mainWindow) mainWindow.setTitle(`SubSandwicher v${app.getVersion()} — logs: ${logPath()}`);
  });
  return mainWindow;
}

function tempFile(name) {
  if (!tempDir) tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'subsandwicher-'));
  log('info', 'temp dir', tempDir);
  return path.join(tempDir, name);
}

function runCli(args, opts = {}) {
  const { onProgress = null, track = false } = opts;
  const { cli } = binaries();
  log('info', 'cli run:', cli, JSON.stringify(args));
  return new Promise((resolve, reject) => {
    // On POSIX the CLI leads its own process group so quit can kill the whole
    // subtree; Windows relies on the kill-on-close Job Object in the CLI.
    const detached = process.platform !== 'win32';
    const p = spawn(cli, args, { windowsHide: true, detached });
    if (track) activeMergeProcess = p;
    let out = '', errText = '';
    p.stdout.on('data', d => { out += d; });
    p.stderr.on('data', d => {
      const chunk = d.toString();
      errText += chunk;
      // merge emits {"role"...,"overall":0..1} progress lines on stderr
      if (onProgress && chunk.includes('"overall"')) {
        for (const line of chunk.split('\n')) {
          if (!line.trim().startsWith('{')) continue;
          try {
            const parsed = JSON.parse(line);
            if (typeof parsed.overall === 'number') onProgress(parsed);
          } catch { /* not a progress line */ }
        }
      }
    });
    p.on('error', (e) => reject(e));
    p.on('close', (code) => {
      if (activeMergeProcess === p) activeMergeProcess = null;
      log('info', 'cli exited', { code, stderr: errText.trim(), stdout: out.slice(0, 400) });
      if (code === 0) {
        resolve(JSON.parse(out));
      } else {
        reject(new Error(`cli exited ${code}: ${errText.trim() || 'no stderr'}`));
      }
    });
  });
}

function defaultMergedPath(videoPath, primaryIndex, secondaryIndex) {
  return tempFile(`merge.${primaryIndex}-${secondaryIndex}.ass`);
}

function extensionlessBase(videoPath) {
  return path.basename(videoPath, path.extname(videoPath));
}

// Raw Dialogue lines in the merged file, indexed by the same order the report
// uses for event ids. Attached to the review payload so a copied state can be
// replayed without shipping the whole file.
function readDialogueLines(assPath) {
  try {
    const text = fs.readFileSync(assPath, 'utf8');
    const out = [];
    for (const line of text.split('\n')) {
      if (line.startsWith('Dialogue:')) out.push(line.replace(/\r$/, ''));
    }
    return out;
  } catch (e) {
    log('warn', 'could not read merged .ass for raw lines', e.message);
    return [];
  }
}

function loadReview(stats) {
  if (!stats || !stats.report_path) return null;
  try {
    const report = JSON.parse(fs.readFileSync(stats.report_path, 'utf8'));
    const raw = readDialogueLines(stats.out);
    for (const ev of report.events || []) {
      if (Number.isInteger(ev.id) && raw[ev.id]) ev.raw = raw[ev.id];
    }
    log('info', 'review loaded', {
      report: stats.report_path,
      windows: (report.windows || []).length,
      conflicts: (report.conflicts || []).length,
    });
    return report;
  } catch (e) {
    log('warn', 'review load failed', e.message);
    return null;
  }
}

function clearPlaybackTimer() {
  if (playbackTimer) {
    clearTimeout(playbackTimer);
    playbackTimer = null;
  }
}

function clearPlaybackClock() {
  if (playbackClock) {
    clearInterval(playbackClock);
    playbackClock = null;
  }
  playbackRange = null;
}

// Report progress within the window loop to the renderer. Polling keeps the
// mpv IPC surface small and works the same for looping and one-shot playback.
function startPlaybackClock(start, end) {
  clearPlaybackClock();
  if (!(Number.isFinite(start) && Number.isFinite(end) && end > start)) return;
  playbackRange = { start, end };
  const tick = async () => {
    if (playbackTickBusy) return;
    if (!activeSession || activeSession.isDead() || !playbackRange) return;
    playbackTickBusy = true;
    try {
      const t = await activeSession.getProperty('time-pos');
      if (typeof t === 'number' && playbackRange && mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send('playback-position', {
          time: t, start: playbackRange.start, end: playbackRange.end,
        });
      }
    } catch (e) {
      log('warn', 'playback position poll failed', e.message);
    } finally {
      playbackTickBusy = false;
    }
  };
  playbackClock = setInterval(tick, 100);
  tick();
}

// Pause and clear any window loop. Used before seeking or reloading subs.
async function stopPlayback(seekTo) {
  clearPlaybackTimer();
  clearPlaybackClock();
  if (!activeSession || activeSession.isDead()) return;
  try {
    await activeSession.exec(['set', 'pause', 'yes']);
    await activeSession.exec(['set', 'ab-loop-a', 'no']);
    await activeSession.exec(['set', 'ab-loop-b', 'no']);
    if (typeof seekTo === 'number') await activeSession.seek(seekTo);
  } catch (e) {
    log('warn', 'stop playback failed', e.message);
  }
}

// Ensure a live mpv session is at `time`, relaunching if the user closed it.
// The reload-safe preview working copy is used when present.
async function ensurePreviewAt(videoPath, mergedPath, time) {
  const ctx = previewCtx || { videoPath, mergedPath };
  if (activeSession && !activeSession.isDead()) {
    await activeSession.seek(time);
    return true;
  }
  log('warn', 'no live mpv session; relaunching for preview seek');
  if (activeSession) activeSession.quit();
  activeSession = await launchPreview({
    video: ctx.videoPath, mergedSub: ctx.previewPath || ctx.mergedPath, seekTo: time,
  });
  return true;
}

function launchPreview(opts) {
  return launchPreviewSession({ ...opts, scriptPath: dragScriptPath }).then((session) => {
    session.onClientMessage = (args) => handleMpvClientMessage(args);
    return session;
  });
}

// Current libass boxes for preview hit-testing. These include collision
// stacking, fades and Effect-column motion, which the merge-time masks cannot
// know at an arbitrary preview time.
async function previewBoxesAt(time) {
  const p = previewCtx && previewCtx.previewPath;
  if (!p || !Number.isFinite(time)) return null;
  let stamp = '';
  try {
    const st = fs.statSync(p);
    stamp = `${st.mtimeMs}:${st.size}`;
  } catch (e) {
    return null;
  }
  const key = `${p}|${stamp}|${time.toFixed(3)}`;
  if (previewBoxCache.has(key)) return previewBoxCache.get(key);
  const res = await runCli(['boxes', `--ass=${p}`, `--time=${time}`], {});
  const boxes = Array.isArray(res.boxes) ? res.boxes : [];
  log('info', 'preview boxes at', time, { count: boxes.length, ids: boxes.map(b => b.id) });
  previewBoxCache.set(key, boxes);
  if (previewBoxCache.size > 40) {
    const first = previewBoxCache.keys().next().value;
    previewBoxCache.delete(first);
  }
  return boxes;
}

function handleMpvClientMessage(args) {
  let list = args || [];
  if (list[0] === 'subsandwicher-drag') {
    handleMpvDrag(list.slice(1));
    return;
  }
  if (list[0] === 'subsandwicher-base') {
    list = list.slice(1);
  }
  const kind = list[0];
  if (kind === 'quad' || kind === 'anchor' || kind === 'clear') {
    handleMpvBase(list);
    return;
  }
  // Older mpv/script combinations may omit the message name.
  handleMpvDrag(list);
}

function handleMpvBase(list) {
  if (!mainWindow || mainWindow.isDestroyed()) return;
  const [kind, ...rest] = list || [];
  if (kind === 'quad' || kind === 'anchor') {
    stopPlayback().catch((e) => log('warn', 'pause for base edit failed', e.message));
  }
  if (kind === 'quad') {
    const nums = rest.map(Number);
    if (nums.length < 8 || nums.some((n) => !Number.isFinite(n))) return;
    const quad = [];
    for (let i = 0; i < 8; i += 2) quad.push({ x: nums[i], y: nums[i + 1] });
    log('info', 'preview base quad', { quad });
    mainWindow.webContents.send('preview-base', { kind: 'quad', quad });
  } else if (kind === 'anchor') {
    const index = Number(rest[0]);
    if (!Number.isInteger(index)) return;
    log('info', 'preview base anchor', { index });
    mainWindow.webContents.send('preview-base', { kind: 'anchor', index });
  } else if (kind === 'clear') {
    log('info', 'preview base cleared');
    mainWindow.webContents.send('preview-base', { kind: 'clear' });
  }
}

// Mouse drag/nudge messages from mpv-drag.lua. Down pauses the preview so the
// frame and its hit-test boxes stay fixed; coordinates are normalized to the
// video rectangle by the Lua script.
function handleMpvDrag(args) {
  if (!previewCtx || !mainWindow || mainWindow.isDestroyed()) return;
  let list = args || [];
  // JSON IPC may include the script-message name as the first payload arg.
  if (list[0] === 'subsandwicher-drag') list = list.slice(1);
  const [phase, x, y, mode, time, dx, dy, screenW, screenH,
    osdW, osdH, marginL, marginT, mouseX, mouseY, par] = list;
  if (!['down', 'move', 'up', 'cancel', 'nudge', 'reset'].includes(phase)) return;
  const numOrNull = (v) => {
    if (v === undefined || v === null || v === '') return null;
    const n = Number(v);
    return Number.isFinite(n) ? n : null;
  };
  // Mouse-move payloads are high frequency; log only the phase boundaries and
  // capture the full OSD geometry so a coordinate mismatch can be diagnosed.
  if (phase !== 'move') {
    log('info', 'preview drag', {
      phase, x, y, mode, time, dx, dy, screenW, screenH,
      osdW: numOrNull(osdW), osdH: numOrNull(osdH),
      marginL: numOrNull(marginL), marginT: numOrNull(marginT),
      mouseX: numOrNull(mouseX), mouseY: numOrNull(mouseY),
      par: numOrNull(par),
    });
  }
  if (phase === 'down') {
    stopPlayback().catch((e) => log('warn', 'pause for drag failed', e.message));
  }
  mainWindow.webContents.send('preview-drag', {
    phase,
    nx: numOrNull(x),
    ny: numOrNull(y),
    mode: mode || 'normal',
    time: numOrNull(time),
    dx: numOrNull(dx),
    dy: numOrNull(dy),
    screenW: numOrNull(screenW),
    screenH: numOrNull(screenH),
    osdW: numOrNull(osdW),
    osdH: numOrNull(osdH),
    marginL: numOrNull(marginL),
    marginT: numOrNull(marginT),
    mouseX: numOrNull(mouseX),
    mouseY: numOrNull(mouseY),
    par: numOrNull(par),
  });
}

function applyArgs(assPath, reportPath, outPath, fixes) {
  const args = ['apply', `--ass=${assPath}`, `--report=${reportPath}`, `--out=${outPath}`];
  for (const f of fixes || []) {
    if (!f || !Number.isInteger(f.windowId)) continue;
    // Manual overrides and comments need the full object form.
    if (f.override || f.comment) {
      args.push(`--fix-json=${JSON.stringify(f)}`);
    } else if (Number.isInteger(f.suggestionIndex)) {
      args.push(`--fix=${f.windowId}:${f.suggestionIndex}`);
    }
  }
  return args;
}

// Write a working copy with the accepted plus previewed fixes and reload it in
// mpv. The original merged file is never modified.
async function applyPreviewFixes(fixes, seekTo, keepPlaying) {
  const ctx = previewCtx;
  if (!ctx || !ctx.previewPath) return { ok: false, error: 'preview session is not ready' };
  if (!ctx.reportPath) return { ok: false, error: 'this merge has no decision log' };
  if (!keepPlaying) await stopPlayback();
  const res = await runCli(applyArgs(ctx.mergedPath, ctx.reportPath, ctx.previewPath, fixes || []), {});
  let resumed = false;
  if (activeSession && !activeSession.isDead()) {
    await activeSession.reloadSubs();
    if (keepPlaying) {
      resumed = true;
      // sub-reload should leave a playing session alone, but make sure.
      try { await activeSession.exec(['set', 'pause', 'no']); } catch (e) { log('warn', 'resume playback', e.message); }
    } else if (typeof seekTo === 'number') {
      await activeSession.seek(seekTo);
    }
  } else {
    activeSession = await launchPreview({
      video: ctx.videoPath, mergedSub: ctx.previewPath, seekTo: typeof seekTo === 'number' ? seekTo : 0,
    });
  }
  log('info', 'preview fixes applied', {
    previewPath: ctx.previewPath,
    seekTo: typeof seekTo === 'number' ? seekTo : null,
    keepPlaying,
    applied: (res.applied || []).map(a => ({
      event: a.event, kind: a.kind, bbox: a.bbox,
      safeOutside: a.safeOutside, frameOutside: a.frameOutside,
    })),
  });
  return {
    ok: true,
    applied: res.applied || [],
    remainingConflicts: res.remainingConflicts || [],
    keepPlaying: resumed,
  };
}
app.whenReady().then(() => {
  Menu.setApplicationMenu(null); // no menu bar; not relevant to our flow
  initLog(app.getPath('userData'));
  log('info', 'app starting', { ...appBuildInfo(), electron: process.versions.electron, node: process.versions.node });
  resolveBinaries();
  ensureWindow();

  ipcMain.handle('fit-window', async () => {
    const h = await mainWindow.webContents.executeJavaScript('document.documentElement.scrollHeight');
    const [w] = mainWindow.getSize();
    const work = screen.getPrimaryDisplay().workArea;
    const target = Math.min(h + 30, work.height);
    log('info', 'fit-window', { contentH: h, target });
    mainWindow.setSize(w, Math.max(target, mainWindow.getSize()[1] || target), true);
    return true;
  });

  ipcMain.handle('get-log-path', () => logPath());

  // Renderer-side diagnostics (hit testing, preview geometry) land in the same
  // stamped log as main-process events, so one file is enough for a report.
  ipcMain.handle('debug-log', (_e, level, message, data) => {
    const lvl = ['debug', 'info', 'warn', 'error'].includes(level) ? level : 'info';
    if (data === undefined) log(lvl, `ui: ${message}`);
    else log(lvl, `ui: ${message}`, data);
    return true;
  });

  ipcMain.handle('probe-video', async (_e, videoPath) => {
    log('info', 'probe video', videoPath);
    const res = await runCli(['probe', `--video=${videoPath}`], { track: true });
    const bitmap = new Set(['hdmv_pgs_subtitle', 'dvd_subtitle', 'dvb_subtitle', 'arib_caption']);
    const streams = res.streams || [];
    for (const s of streams) {
      s.bitmapped = bitmap.has(s.codec_name);
      s.display = `${s.tags?.title || 'untitled'} (${s.codec_name})`;
    }
    return { videoPath, streams };
  });

  ipcMain.handle('merge-subtitles', async (_e, { videoPath, primaryIndex, secondaryIndex }) => {
    log('info', 'merge request', { videoPath, primaryIndex, secondaryIndex });
    const outPath = defaultMergedPath(videoPath, primaryIndex, secondaryIndex);
    const stats = await runCli(['merge', `--video=${videoPath}`,
      `--primary-index=${primaryIndex}`, `--secondary-index=${secondaryIndex}`,
      `--out=${outPath}`], {
      track: true,
      onProgress: (p) => {
        if (mainWindow && !mainWindow.isDestroyed()) mainWindow.webContents.send('merge-progress', p);
      },
    });
    log('info', 'merge ok', {
      out: stats.out, styles: stats.styles, lines: stats.lines,
      events: (stats.events || []).length, report: stats.report_path,
      conflicts: stats.conflicts, windows: stats.conflict_windows,
      reviewRequired: stats.review_required, exact: stats.exact,
      exactEvents: stats.exact_events,
    });
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.webContents.send('merge-progress', {
        stage: 'review', overall: 0.97, indeterminate: true,
        message: 'loading the decision log for review…',
      });
    }
    stats.review = loadReview(stats);
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.webContents.send('merge-progress', {
        stage: 'done', overall: 1, message: 'review ready',
      });
    }
    return stats;
  });

  ipcMain.handle('launch-preview', async (_e, { videoPath, mergedPath, reportPath, seekTo }) => {
    log('info', 'launch preview', { videoPath, mergedPath, seekTo });
    clearPlaybackTimer();
    if (activeSession) {
      log('info', 'existing session found; shutting it down first');
      activeSession.quit();
      activeSession = null;
    }
    let previewPath = mergedPath;
    try {
      previewPath = tempFile('preview.ass');
      fs.copyFileSync(mergedPath, previewPath);
    } catch (e) {
      log('warn', 'could not create preview working copy; using the merged file', e.message);
      previewPath = mergedPath;
    }
    const rep = reportPath || (fs.existsSync(mergedPath + '.report.json') ? mergedPath + '.report.json' : null);
    previewCtx = { videoPath, mergedPath, previewPath, reportPath: rep };
    activeSession = await launchPreview({ video: videoPath, mergedSub: previewPath, seekTo });
    log('info', 'preview session ready (seekTo=' + seekTo + ')', { previewPath, reportPath: rep });
    // The review UI is the next step; keep it in front of the new mpv window.
    if (mainWindow && !mainWindow.isDestroyed()) mainWindow.focus();
    return true;
  });

  ipcMain.handle('review-next-frame', async (_e, dir) => {
    if (activeSession) { await activeSession.stepFrame(dir); return true; }
    return false;
  });

  ipcMain.handle('jump-random-frame', async (_e, { videoPath, mergedPath, events }) => {
    log('info', 'pick random candidate among', events.length, 'events');
    const t = pickPreviewTime(events);
    // If mpv was closed by hand, ensurePreviewAt relaunches it at the time.
    try {
      await ensurePreviewAt(previewCtx?.videoPath || videoPath, previewCtx?.mergedPath || mergedPath, t);
    } catch (e) {
      log('error', 'relaunch failed', e.message);
      return { ok: false, error: 'mpv relaunch failed: ' + e.message };
    }
    log('info', 'jumped to', t);
    return { ok: true, time: t };
  });

  ipcMain.handle('seek-preview', async (_e, { videoPath, mergedPath, time }) => {
    log('info', 'seek preview to', time);
    try {
      await stopPlayback();
      await ensurePreviewAt(videoPath, mergedPath, time);
      return { ok: true, time };
    } catch (e) {
      log('error', 'seek preview failed', e.message);
      return { ok: false, error: e.message };
    }
  });

  ipcMain.handle('apply-preview', async (_e, { fixes, seekTo, keepPlaying }) => {
    log('info', 'apply preview fixes', { fixes, seekTo, keepPlaying });
    try {
      return await applyPreviewFixes(fixes, seekTo, keepPlaying);
    } catch (e) {
      log('error', 'apply preview failed', e.message);
      return { ok: false, error: e.message };
    }
  });

  ipcMain.handle('preview-boxes', async (_e, { time }) => {
    try {
      const boxes = await previewBoxesAt(Number(time));
      return { ok: true, time, boxes: boxes || [] };
    } catch (e) {
      log('warn', 'preview boxes failed', e.message);
      return { ok: false, error: e.message, boxes: [] };
    }
  });

  ipcMain.handle('preview-drag-boxes', async (_e, { playW, playH, boxes }) => {
    if (!activeSession || activeSession.isDead()) return false;
    const args = ['script-message', 'subsandwicher-set-box', String(playW), String(playH)];
    for (const b of boxes || []) {
      args.push(String(b.x0), String(b.y0), String(b.x1), String(b.y1));
    }
    try {
      await activeSession.exec(args);
      return true;
    } catch (e) {
      log('warn', 'preview drag overlay failed', e.message);
      return false;
    }
  });

  ipcMain.handle('preview-set-base', async (_e, { playW, playH, quad, anchors }) => {
    log('info', 'preview set base', {
      playW, playH, quads: (quad || []).length, anchors: (anchors || []).length,
    });
    if (!activeSession || activeSession.isDead()) return false;
    const args = ['script-message', 'subsandwicher-set-base', String(playW), String(playH)];
    for (const p of quad || []) args.push(String(p.x), String(p.y));
    for (const a of anchors || []) args.push(String(a.x), String(a.y));
    try {
      await activeSession.exec(args);
      return true;
    } catch (e) {
      log('warn', 'preview base overlay failed', e.message);
      return false;
    }
  });

  ipcMain.handle('preview-clear-base', async () => {
    if (!activeSession || activeSession.isDead()) return false;
    try {
      await activeSession.exec(['script-message', 'subsandwicher-clear-base']);
      return true;
    } catch (e) {
      log('warn', 'preview base clear failed', e.message);
      return false;
    }
  });

  ipcMain.handle('preview-drag-cancel', async () => {
    if (!activeSession || activeSession.isDead()) return false;
    try {
      await activeSession.exec(['script-message', 'subsandwicher-cancel']);
      return true;
    } catch (e) {
      log('warn', 'preview drag cancel failed', e.message);
      return false;
    }
  });

  ipcMain.handle('preview-playback', async (_e, payload) => {
    const { action } = payload || {};
    log('info', 'preview playback', payload);
    try {
      if (action === 'stop') {
        await stopPlayback(typeof payload.start === 'number' ? payload.start : undefined);
        return { ok: true };
      }
      if (action === 'pause') {
        // Hold the current frame; no seek, so fix previews can reload subtitles
        // against the exact image the user is looking at.
        await stopPlayback();
        return { ok: true };
      }
      if (!activeSession || activeSession.isDead()) {
        const ctx = previewCtx;
        if (!ctx) return { ok: false, error: 'preview session is not ready' };
        activeSession = await launchPreview({
          video: ctx.videoPath,
          mergedSub: ctx.previewPath || ctx.mergedPath,
          seekTo: typeof payload.start === 'number' ? payload.start : 0,
        });
      }
      const session = activeSession;
      const speed = Number(payload.speed) > 0 ? Number(payload.speed) : 1;
      const muted = payload.muted !== false;
      await session.exec(['set', 'mute', muted ? 'yes' : 'no']);
      await session.exec(['set', 'speed', String(speed)]);
      if (action === 'resume') {
        if (!activeSession || activeSession.isDead()) return { ok: false, error: 'preview session is not ready' };
        if (payload.loop) {
          await activeSession.exec(['set', 'ab-loop-a', String(payload.start)]);
          await activeSession.exec(['set', 'ab-loop-b', String(payload.end)]);
        }
        await activeSession.exec(['set', 'pause', 'no']);
        startPlaybackClock(payload.start, payload.end);
        return { ok: true };
      }
      if (action === 'play' || action === 'update') {
        if (payload.loop) {
          await session.exec(['set', 'ab-loop-a', String(payload.start)]);
          await session.exec(['set', 'ab-loop-b', String(payload.end)]);
        } else {
          await session.exec(['set', 'ab-loop-a', 'no']);
          await session.exec(['set', 'ab-loop-b', 'no']);
        }
      }
      if (action === 'play') {
        clearPlaybackTimer();
        await session.seek(payload.start);
        await session.exec(['set', 'pause', 'no']);
        startPlaybackClock(payload.start, payload.end);
        if (!payload.loop) {
          const seconds = Math.max(0.2, (payload.end - payload.start) / speed);
          playbackTimer = setTimeout(async () => {
            playbackTimer = null;
            try { await session.exec(['set', 'pause', 'yes']); } catch {}
            clearPlaybackClock();
            if (mainWindow && !mainWindow.isDestroyed()) mainWindow.webContents.send('playback-ended');
          }, seconds * 1000 + 120);
        }
      } else if (action === 'update') {
        startPlaybackClock(payload.start, payload.end);
      }
      return { ok: true };
    } catch (e) {
      log('error', 'preview playback failed', e.message);
      return { ok: false, error: e.message };
    }
  });

  ipcMain.handle('confirm-save', async (_e, payload) => {
    log('info', 'confirm save', {
      mergedPath: payload && payload.mergedPath,
      videoPath: payload && payload.videoPath,
      primaryLang: payload && payload.primaryLang,
      secondaryLang: payload && payload.secondaryLang,
      fixes: (payload && payload.fixes || []).length,
    });
    return saveMerged(payload || {});
  });

  ipcMain.handle('open-logs-folder', () => shell.showItemInFolder(logPath()));

  ipcMain.handle('copy-text', (_e, text) => {
    clipboard.writeText(String(text ?? ''));
    return true;
  });

  ipcMain.handle('eject', async () => {
    clearPlaybackTimer();
    clearPlaybackClock();
    if (activeSession) {
      activeSession.quit();
      activeSession = null;
    }
    previewCtx = null;
    log('info', 'ejected preview');
    return true;
  });

  ipcMain.handle('get-app-meta', () => ({
    ...appBuildInfo(),
    electron: process.versions.electron,
    node: process.versions.node,
    platform: process.platform,
    arch: process.arch,
    logPath: logPath(),
  }));

  app.on('activate', () => ensureWindow());
});

function destMergedPath(videoPath, primaryLang, secondaryLang) {
  const base = extensionlessBase(videoPath);
  const dir = path.dirname(videoPath);
  if (primaryLang && secondaryLang) {
    return path.join(dir, `${base}.${primaryLang}-${secondaryLang}.ass`);
  } else if (primaryLang) {
    return path.join(dir, `${base}.${primaryLang}.ass`);
  } else {
    return path.join(dir, `${base}.merged.ass`);
  }
}

function pickPreviewTime(events) {
  if (!events || !events.length) return 0;
  const cands = events.filter(e => e.end > e.start);
  if (!cands.length) return 0;
  const e = cands[Math.floor(Math.random() * cands.length)];
  const dur = e.end - e.start;
  return Math.max(0.05, e.start + Math.min(0.3, dur / 2));
}

async function saveMerged(payload) {
  const { mergedPath, videoPath, primaryLang, secondaryLang, reportPath, fixes, comments, statuses, bases } = payload;
  if (!mergedPath || !fs.existsSync(mergedPath)) {
    log('error', 'no merged file to save', mergedPath);
    return { ok: false, error: 'no merged preview file present' };
  }
  if (!primaryLang || !secondaryLang) {
    log('warn', 'missing lang tags at save time', { primaryLang, secondaryLang });
  }
  const chosen = Array.isArray(fixes) ? fixes.filter(f => f && Number.isInteger(f.windowId) && Number.isInteger(f.suggestionIndex)) : [];
  if (chosen.length && !reportPath) {
    return { ok: false, error: 'review fixes were selected but the decision log is missing' };
  }

  let finalPath = mergedPath;
  let applied = [];
  try {
    if (chosen.length) {
      finalPath = tempFile('final.ass');
      const res = await runCli(applyArgs(mergedPath, reportPath, finalPath, chosen), {});
      applied = res.applied || [];
      log('info', 'applied review fixes', { requested: chosen.length, applied: applied.length });
    }
  } catch (e) {
    log('error', 'apply review fixes failed', e.message);
    return { ok: false, error: 'applying review fixes failed: ' + e.message };
  }

  let dest;
  let logPath = null;
  let logError = null;
  try {
    const [{ totalKB }] = await safeStat(finalPath);
    log('info', 'output file size kb', totalKB);
    dest = destMergedPath(videoPath, primaryLang, secondaryLang);
    log('info', 'dest', dest);
    fs.copyFileSync(finalPath, dest);
    log('info', 'saved merged subs', dest);
  } catch (e) {
    log('error', 'save failed', e.message);
    return { ok: false, error: e.message };
  }

  if (reportPath && fs.existsSync(reportPath)) {
    try {
      const report = JSON.parse(fs.readFileSync(reportPath, 'utf8'));
      const raw = readDialogueLines(mergedPath);
      for (const ev of report.events || []) {
        if (Number.isInteger(ev.id) && raw[ev.id]) ev.raw = raw[ev.id];
      }
      const windows = new Map((report.windows || []).map(w => [w.id, w]));
      const fixDetails = chosen.map(f => {
        const w = windows.get(f.windowId);
        const suggestion = w && w.suggestions ? w.suggestions[f.suggestionIndex] : null;
        return { ...f, suggestion: suggestion || null };
      });
      logPath = dest.replace(/\.ass$/i, '') + '.review.json';
      const session = {
        kind: 'subsandwicher.review-session',
        version: 1,
        createdAt: new Date().toISOString(),
        app: {
          ...appBuildInfo(),
          electron: process.versions.electron,
          node: process.versions.node,
          platform: process.platform,
          arch: process.arch,
        },
        video: videoPath,
        mergedAss: mergedPath,
        finalAss: dest,
        reportPath,
        fixes: fixDetails,
        applied,
        comments: comments || {},
        statuses: statuses || {},
        bases: bases || {},
        report,
      };
      fs.writeFileSync(logPath, JSON.stringify(session, null, 2) + '\n');
      log('info', 'review session log saved', logPath);
    } catch (e) {
      logError = e.message;
      log('error', 'review session log failed', e.message);
    }
  }

  log('info', 'save complete', { dest, log: logPath, applied: applied.length });
  return { ok: true, dest, log: logPath, logError, applied: applied.length };
}

async function safeStat(p) {
  return new Promise((resolve) => {
    fs.stat(p, (e, st) => resolve([{ totalKB: st && st.size ? Math.round(st.size / 1024) : -1 }]));
  });
}

app.on('window-all-closed', () => {
  log('info', 'window all closed; quitting');
  if (process.platform !== 'darwin') app.quit();
});

app.on('quit', () => {
  log('info', 'shutting down children', {
    previewSession: activeSession ? 'live' : 'none',
    merge: activeMergeProcess ? 'in flight' : 'idle'
  });
  clearPlaybackTimer();
  if (activeSession) {
    activeSession.quit();
  }
  if (activeMergeProcess) {
    treeKill(activeMergeProcess);
    activeMergeProcess = null;
  }
  // probe/extract child processes share the same kill-on-close job the CLI
  // creates (Windows); POSIX relies on the process-group SIGTERM above.
});

// Kill a (or spawned) CLI process along with any children (Windows Job keeps
// ffmpeg gone too; posix relies on distro's pgrep-style kill group).
function treeKill(cliProc) {
  const pid = cliProc.pid;
  if (!pid) return;
  try {
    if (process.platform === 'win32') {
      spawn('taskkill', ['/F', '/T', '/PID', String(pid)], { windowsHide: true });
      log('info', 'taskkill /T sent for', pid);
    } else {
      // POSIX: SIGTERM the CLI; its children are killed via its own errhandle
      // (graceful) plus a follow-up kill of the process group as fallback
      process.kill(-pid, 'SIGTERM');
      log('info', 'SIGTERM to pgid', pid);
    }
  } catch (e) {
    log('warn', 'treeKill failed', e.message);
  }
}
