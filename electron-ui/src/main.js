const { app, BrowserWindow, ipcMain, shell, Menu, screen } = require('electron');
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
let activeMergeProcess = null; // cli process for the running merge

function ensureWindow() {
  if (mainWindow && !mainWindow.isDestroyed()) return mainWindow;
  mainWindow = new BrowserWindow({
    width: 960,
    height: 640,
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
    if (mainWindow) mainWindow.setTitle(`SubSandwicher — logs: ${logPath()}`);
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

app.whenReady().then(() => {
  Menu.setApplicationMenu(null); // no menu bar; not relevant to our flow
  initLog(app.getPath('userData'));
  log('info', 'app starting', { appVersion: app.getVersion(), electron: process.versions.electron, node: process.versions.node });
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
    log('info', 'merge ok', { out: stats.out, styles: stats.styles, lines: stats.lines, events: (stats.events || []).length });
    return stats;
  });

  ipcMain.handle('launch-preview', async (_e, { videoPath, mergedPath, seekTo }) => {
    log('info', 'launch preview', { videoPath, mergedPath, seekTo });
    if (activeSession) {
      log('info', 'existing session found; shutting it down first');
      activeSession.quit();
      activeSession = null;
    }
    previewCtx = { videoPath, mergedPath };
    activeSession = await launchPreviewSession({ video: videoPath, mergedSub: mergedPath, seekTo });
    log('info', 'preview session ready (seekTo=' + seekTo + ')');
    return true;
  });

  ipcMain.handle('review-next-frame', async (_e, dir) => {
    if (activeSession) { await activeSession.stepFrame(dir); return true; }
    return false;
  });

  ipcMain.handle('jump-random-frame', async (_e, { videoPath, mergedPath, events }) => {
    log('info', 'pick random candidate among', events.length, 'events');
    const t = pickPreviewTime(events);
    // If mpv was closed by hand, relaunch it at the requested time.
    if (!activeSession || activeSession.isDead()) {
      log('warn', 'no live mpv session; relaunching for this command');
      const ctx = previewCtx || { videoPath, mergedPath };
      try {
        if (activeSession) activeSession.quit();
        activeSession = await launchPreviewSession({ video: ctx.videoPath, mergedSub: ctx.mergedPath, seekTo: t });
      } catch (e) {
        log('error', 'relaunch failed', e.message);
        return { ok: false, error: 'mpv relaunch failed: ' + e.message };
      }
    } else {
      await activeSession.seek(t);
    }
    log('info', 'jumped to', t);
    return { ok: true, time: t };
  });

  ipcMain.handle('confirm-save', async (_e, { mergedPath, videoPath, primaryLang, secondaryLang }) => {
    log('info', 'confirm save', { mergedPath, videoPath, primaryLang, secondaryLang });
    return saveMerged(mergedPath, videoPath, primaryLang, secondaryLang);
  });

  ipcMain.handle('open-logs-folder', () => shell.showItemInFolder(logPath()));

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

function saveMerged(mergedPath, videoPath, primaryLang, secondaryLang) {
  return new Promise(async (resolve) => {
    if (!mergedPath || !fs.existsSync(mergedPath)) {
      log('error', 'no merged file to save', mergedPath);
      resolve({ ok: false, error: 'no merged preview file present' });
      return;
    }
    if (!primaryLang || !secondaryLang) {
      // fall back to a generic suffix if the UI failed to supply tags
      log('warn', 'missing lang tags at save time', { primaryLang, secondaryLang });
    }
    let dest;
    try {
      const [{ totalKB }] = await safeStat(mergedPath);
      log('info', 'merged file size kb', totalKB);
      dest = destMergedPath(videoPath, primaryLang, secondaryLang);
      log('info', 'dest', dest);
      fs.copyFileSync(mergedPath, dest);
      log('info', 'saved merged subs', dest);
      resolve({ ok: true, dest });
    } catch (e) {
      log('error', 'save failed', e.message);
      resolve({ ok: false, error: e.message });
    }
    log('info', 'save complete');
  });
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
