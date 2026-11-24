let state = {
  videoPath: null,
  streams: [],
  primaryIndex: null,
  secondaryIndex: null,
  merged: null, // {out, styles, lines, events, report_path, review}
  review: null, // decision log from the Go core
  reviewWindow: null,
  notes: {}, // window id -> comment
  appMeta: null,
  logPath: null,
  merging: false,
  mergeProgress: -1,
  chosen: {}, // window id -> { event id -> suggestion id }
  edits: {}, // "window:event" -> { suggestionId, override, dirty } manual fix edits
  layerEdits: {}, // "window:event" -> { layer, ownLayer } layer/isolation edits
  groupSel: {}, // window id -> { event id -> true } selection for group move
  selected: null, // { windowId, eventId } picked in the mpv preview
  drag: null, // active preview drag state
  bases: {}, // "window:event" -> { quad, anchors } referent box
  pendingBase: null, // base drawn before a subtitle is selected
  fixWarnings: {}, // "window:event" -> applied fix with geometry warnings
  recheck: [], // remaining conflicts from the last apply (estimated)
  playbackPos: null, // { time, start, end } from the main process while playing
  windowScroll: {}, // window id -> remembered #window-detail scrollTop
  applyingPreview: false,
  pendingPreview: null,
  playingWindow: null,
  pausedWindow: null,
  statuses: {}, // window id -> "done" | "again" (absent = open)
  langsCollapsed: false,
  probing: false,
  diagnostics: {}, // last preview hit/drag/OSD geometry, copied with review state
  lastPreviewTime: null, // last frame used for a preview click, for box warming
};

const sel = (s) => document.querySelector(s);
const S = window.subsandwicher;
const appVersionBadge = sel('#app-version');

function renderAppVersion() {
  if (!appVersionBadge) return;
  const m = state.appMeta || {};
  const version = m.version || 'dev';
  const commit = m.commit ? ` · ${m.commit}` : '';
  const build = m.buildTime ? ` · built ${m.buildTime}` : '';
  appVersionBadge.textContent = `SubSandwicher v${version}${commit}`;
  appVersionBadge.title = `SubSandwicher ${version}${commit}${build}`;
}

const dropzone = sel('#dropzone');
const dropstatus = sel('#dropstatus');
const droperror = sel('#droperror');
const actions = sel('#actions');
const topbarName = sel('#topbar-name');
const setup = sel('#setup');
const langLists = sel('#lang-lists');
const langSummary = sel('#lang-summary');
const langSummaryText = sel('#lang-summary-text');
const btnChangeLangs = sel('#btn-change-langs');
const btnChangeLangsSummary = sel('#btn-change-langs-summary');
const btnEject = sel('#btn-eject');
const status = sel('#status');
const logline = sel('#logline');
const btnPreview = sel('#btn-preview');
const progwrap = sel('#progwrap');
const progbar = sel('#progbar');
const btnAnother = sel('#btn-other-frame');
const btnSave = sel('#btn-save');

const review = sel('#review');
const reviewSummary = sel('#review-summary');
const windowList = sel('#window-list');
const windowDetail = sel('#window-detail');
const windowEmpty = sel('#window-empty');
const windowContent = sel('#window-content');
const windowMeta = sel('#window-meta');
const windowSuggestions = sel('#window-suggestions');
const filterSurface = sel('#filter-surface');
const filterCause = sel('#filter-cause');
const filterStatus = sel('#filter-status');
const reviewNote = sel('#review-note');
const btnCopyAll = sel('#btn-copy-all');
const btnCopyWindow = sel('#btn-copy-window');
const btnPreviewWindow = sel('#btn-preview-window');
const btnPlayWindow = sel('#btn-play-window');
const btnStopWindow = sel('#btn-stop-window');
const btnPauseWindow = sel('#btn-pause-window');
const optLoop = sel('#opt-loop');
const optUnmute = sel('#opt-unmute');
const optSpeed = sel('#opt-speed');
const playState = sel('#play-state');
const windowProgress = sel('#window-progress');
const windowProgressFill = sel('#window-progress-fill');

const ICONS = {
  play: '<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M4.5 2.8v10.4L13 8z"/></svg>',
  prev: '<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M13 2.8v10.4L5.6 8z"/><rect x="3" y="2.8" width="1.8" height="10.4" rx="0.5"/></svg>',
  stop: '<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><rect x="4" y="4" width="8" height="8" rx="1"/></svg>',
  pause: '<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><rect x="3.5" y="3" width="3.2" height="10" rx="0.8"/><rect x="9.3" y="3" width="3.2" height="10" rx="0.8"/></svg>',
  random: '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 4h2.5c1.2 0 2 .7 2.7 1.7l2.1 3.2c.7 1 1.5 1.7 2.7 1.7H14"/><path d="M11.5 2.5 14 4l-2.5 1.5"/><path d="M2 12h2.5c1.2 0 2-.7 2.7-1.7l1-1.6"/><path d="M8.2 5.7l1.1-1.5C10 3.2 10.8 2.5 12 2.5h2"/><path d="M11.5 10.5 14 12l-2.5 1.5"/></svg>',
  done: '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2.5 8.5l3.5 3.5 7.5-8"/></svg>',
  reopen: '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M13 8a5 5 0 1 1-1.6-3.7"/><path d="M13 2.5v4h-4"/></svg>',
  again: '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 8a5 5 0 1 0 1.6-3.7"/><path d="M3 2.5v4h4"/></svg>',
};

const windowStatusBadge = sel('#window-status-badge');
const btnDone = sel('#btn-done');
const btnAgain = sel('#btn-again');

btnPlayWindow.innerHTML = ICONS.play;
btnStopWindow.innerHTML = ICONS.stop;
btnPauseWindow.innerHTML = ICONS.pause;
btnAnother.innerHTML = ICONS.random;
btnDone.innerHTML = ICONS.done;
btnAgain.innerHTML = ICONS.again;
if (window.ResizeObserver) {
  new ResizeObserver(() => updatePlaybackStickyOffset()).observe(sel('#playback'));
}


function setStatus(msg, isError = false) {
  if (isError) console.error(msg);
  status.innerHTML = isError
    ? `<span id="error">${escapeHtml(msg)}</span>`
    : escapeHtml(msg);
  status.classList.toggle('error', isError);
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function fmtClock(s) {
  const cs = Math.round(Math.max(0, Number(s) || 0) * 100);
  const m = Math.floor(cs / 6000);
  const sec = Math.floor((cs % 6000) / 100);
  const frac = cs % 100;
  return `${m}:${String(sec).padStart(2, '0')}.${String(frac).padStart(2, '0')}`;
}

function fmtNum(n, digits = 1) {
  return Number(n || 0).toFixed(digits);
}

function signed(n) {
  const v = Number(n) || 0;
  return (v > 0 ? '+' : '') + v.toFixed(1);
}

// ---------- log path + app meta ----------
(async () => {
  try {
    const p = await S.getLogPath();
    state.logPath = p;
    logline.textContent = `logs: ${p} (click to open folder)`;
    logline.onclick = () => S.openLogsFolder();
  } catch (e) { console.warn('log path failed', e); }
})();

(async () => {
  try { state.appMeta = await S.getAppMeta(); } catch (e) { console.warn('app meta failed', e); }
  renderAppVersion();
})();

// ---------- drag & drop ----------
['dragover', 'dragenter'].forEach(ev =>
  dropzone.addEventListener(ev, (e) => { e.preventDefault(); dropzone.classList.add('dragover'); }));
['dragleave', 'drop'].forEach(ev =>
  dropzone.addEventListener(ev, (e) => { e.preventDefault(); dropzone.classList.remove('dragover'); }));

dropzone.addEventListener('drop', (e) => {
  e.preventDefault();
  const files = e.dataTransfer.files;
  if (!files.length) { setStatus('no file was dropped', true); return; }
  const p = S.pathForFile(files[0]);
  loadVideo(p);
});

dropzone.addEventListener('click', () => {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = 'video/*,.mkv,.mp4,.avi,.ts,.webm';
  input.onchange = () => { if (input.files[0]) loadVideo(S.pathForFile(input.files[0])); };
  input.click();
});

function friendlyProbeError(e, p) {
  const base = String(p || '').split(/[\\/]/).pop() || 'this file';
  const msg = String((e && e.message) || e || '');
  if (/no subtitle streams|no subtitle track/i.test(msg)) {
    return `"${base}" has no subtitle tracks. Drop a file with text subtitles.`;
  }
  if (/out of scope|image-based/i.test(msg)) {
    return `"${base}" only has image-based subtitles, which this tool cannot merge.`;
  }
  if (/ffprobe failed|invalid data|not a readable|unsupported|moov atom|could not find codec/i.test(msg)) {
    return `Couldn't read "${base}" as a video. Drop a different file.`;
  }
  return `Couldn't load "${base}". Drop a different file.\n${msg.slice(0, 240)}`;
}

function resetToLoad(message = '') {
  state.videoPath = null;
  state.streams = [];
  state.primaryIndex = null;
  state.secondaryIndex = null;
  state.merged = null;
  state.review = null;
  state.reviewWindow = null;
  state.notes = {};
  state.chosen = {};
  state.edits = {};
  state.layerEdits = {};
  state.groupSel = {};
  state.fixWarnings = {};
  state.recheck = [];
  state.selected = null;
  state.drag = null;
  state.bases = {};
  state.pendingBase = null;
  state.statuses = {};
  state.windowScroll = {};
  state.playingWindow = null;
  state.pausedWindow = null;
  state.langsCollapsed = false;
  state.diagnostics = {};
  actions.classList.add('hidden');
  actions.classList.remove('review-mode');
  review.classList.add('hidden');
  setup.classList.remove('hidden');
  btnSave.classList.add('hidden');
  btnChangeLangs.classList.add('hidden');
  dropzone.classList.remove('hidden');
  topbarName.textContent = '';
  sel('#filename').textContent = '';
  sel('#primaryOptions').innerHTML = '';
  sel('#secondaryOptions').innerHTML = '';
  dropstatus.textContent = '';
  droperror.textContent = message || '';
  renderReview();
  updateButtonStates();
  updatePlaybackButtons();
}

async function failToLoad(message) {
  try { if (S.eject) await S.eject(); } catch (e) { console.warn('eject failed', e); }
  resetToLoad(message);
}

async function loadVideo(p) {
  if (!p) { droperror.textContent = "Couldn't resolve the dropped file path."; return; }
  if (state.probing) return;
  state.probing = true;
  state.videoPath = p;
  sel('#filename').textContent = p;
  dropstatus.textContent = 'Probing streams…';
  droperror.textContent = '';
  try {
    const res = await S.probeVideo(p);
    const streams = res.streams || [];
    const usable = streams.filter(s => !s.bitmapped);
    const externalCount = streams.filter(s => s.external).length;
    if (!usable.length) {
      await failToLoad(`No text subtitle tracks were found in "${String(p).split(/[\\/]/).pop()}".`);
      return;
    }
    state.streams = streams;
    state.primaryIndex = null;
    state.secondaryIndex = null;
    state.merged = null;
    state.review = null;
    state.reviewWindow = null;
    state.notes = {};
    state.chosen = {};
    state.edits = {};
    state.layerEdits = {};
    state.groupSel = {};
    state.fixWarnings = {};
    state.recheck = [];
    state.selected = null;
    state.drag = null;
    state.bases = {};
    state.pendingBase = null;
    state.statuses = {};
    state.windowScroll = {};
    state.playingWindow = null;
    state.pausedWindow = null;
    state.langsCollapsed = false;
    dropzone.classList.add('hidden');
    actions.classList.remove('hidden');
    actions.classList.remove('review-mode');
    review.classList.add('hidden');
    setup.classList.remove('hidden');
    btnSave.classList.add('hidden');
    btnChangeLangs.classList.add('hidden');
    topbarName.textContent = p;
    buildStreamLists();
    updateSetup();
    renderReview();
    updatePlaybackButtons();
    dropstatus.textContent = '';
    try { await S.fitWindow(); } catch (e) { console.warn('fit-window failed', e); }
    setStatus(`${streams.length} subtitle stream(s) found${externalCount ? `, ${externalCount} external file(s)` : ''} — pick primary and secondary, then "Continue".`);
  } catch (e) {
    await failToLoad(friendlyProbeError(e, p));
  } finally {
    state.probing = false;
  }
}

async function eject() {
  if (state.playingWindow || state.pausedWindow) {
    try { await S.previewPlayback({ action: 'stop' }); } catch (e) { console.warn(e); }
  }
  try { if (S.eject) await S.eject(); } catch (e) { console.warn('eject failed', e); }
  resetToLoad('');
}

// ---------- stream list building ----------
const LANG_LABELS = {
  ara: 'Arabic', bul: 'Bulgarian', chi: 'Chinese', ger: 'German', ell: 'Greek',
  eng: 'English', fin: 'Finnish', fre: 'French', hun: 'Hungarian', ita: 'Italian',
  jpn: 'Japanese', kor: 'Korean', may: 'Malay', pol: 'Polish', por: 'Portuguese',
  rum: 'Romanian', rus: 'Russian', spa: 'Spanish', swe: 'Swedish',
  tha: 'Thai', tur: 'Turkish', vie: 'Vietnamese', zho: 'Chinese',
};

let langDisplay = null;
try {
  langDisplay = new Intl.DisplayNames(['en'], { type: 'language' });
} catch (e) { /* older Electron */ }

function langName(code) {
  const c = (code || '').toLowerCase();
  if (!c) return 'no tag';
  if (LANG_LABELS[c]) return LANG_LABELS[c];
  if (langDisplay) {
    try {
      const name = langDisplay.of(c);
      if (name && name !== c) return name;
    } catch (e) { /* not a language tag */ }
  }
  return c;
}

function streamFile(s) {
  if (!s) return '';
  if (s.file_name) return s.file_name;
  return String(s.path || '').split(/[\\/]/).pop() || 'external subtitle';
}

function streamLabel(s) {
  if (!s) return '';
  const lang = (s.tags && s.tags.language) || '';
  if (s.external) {
    const extra = (s.tags && s.tags.title) ? ` — ${s.tags.title}` : '';
    return `${streamFile(s)} — ${langName(lang)}${extra} [external file]`;
  }
  const title = (s.tags && s.tags.title) || 'untitled';
  const tag = s.bitmapped ? ' [image-based — not usable]' : '';
  return `#${s.index}  ${title} — ${langName(lang)}${tag}`;
}

function streamOptionLabel(s) {
  if (s.external) {
    const extra = (s.tags && s.tags.title) ? ` ${s.tags.title}` : '';
    return `${streamFile(s)} — ${langName(s.tags?.language)}${extra}`;
  }
  return `#${s.index} ${(s.tags && s.tags.title) || 'untitled'} — ${langName(s.tags?.language)}`;
}

function buildStreamLists() {
  const makeList = (selId, role) => {
    const box = document.querySelector(selId);
    box.innerHTML = '';
    const wrap = box.closest('.streamlist');
    for (const s of state.streams) {
      if (s.bitmapped) continue;
      const label = document.createElement('label');
      if (s.external) label.classList.add('external');
      label.innerHTML = `<input type="radio" name="${role}" value="${s.index}"> ` +
        `<span>${escapeHtml(streamOptionLabel(s))}</span>`;
      const input = label.querySelector('input');
      input.addEventListener('change', () => {
        const wasBoth = bothChosen();
        state[role === 'prim' ? 'primaryIndex' : 'secondaryIndex'] = s.index;
        for (const l of wrap.querySelectorAll('label')) l.classList.remove('checked');
        label.classList.add('checked');
        if (!wasBoth && bothChosen()) state.langsCollapsed = true;
        updateButtonStates();
      });
      box.appendChild(label);
    }
  };
  makeList('#primaryOptions', 'prim');
  makeList('#secondaryOptions', 'sec');
  updateButtonStates();
}

function bothChosen() {
  return state.primaryIndex != null && state.secondaryIndex != null && state.primaryIndex !== state.secondaryIndex;
}

function updateSetup() {
  const both = bothChosen();
  if (both && state.langsCollapsed) {
    langSummary.classList.remove('hidden');
    langLists.classList.add('hidden');
  } else {
    langSummary.classList.add('hidden');
    langLists.classList.remove('hidden');
  }
  if (both) {
    const p = state.streams.find(x => x.index === state.primaryIndex);
    const s = state.streams.find(x => x.index === state.secondaryIndex);
    langSummaryText.innerHTML = `<strong>${escapeHtml(streamLabel(p))}</strong> → <strong>${escapeHtml(streamLabel(s))}</strong>`;
    btnPreview.textContent = 'Continue';
  } else {
    btnPreview.textContent = 'Generate preview';
  }
  btnPreview.disabled = !both;
}

function updateButtonStates() {
  updateSetup();
  btnAnother.disabled = true;
  btnSave.disabled = true;
}

function enterReviewMode() {
  actions.classList.add('review-mode');
  setup.classList.add('hidden');
  review.classList.remove('hidden');
  btnSave.classList.remove('hidden');
  btnChangeLangs.classList.remove('hidden');
}

function showSetup() {
  if (state.playingWindow || state.pausedWindow) {
    state.playingWindow = null;
    state.pausedWindow = null;
    S.previewPlayback({ action: 'stop' }).catch(() => {});
  }
  actions.classList.remove('review-mode');
  review.classList.add('hidden');
  setup.classList.remove('hidden');
  btnSave.classList.add('hidden');
  btnChangeLangs.classList.add('hidden');
  state.langsCollapsed = false;
  updateSetup();
  updatePlaybackButtons();
}

async function eject() {
  if (state.playingWindow || state.pausedWindow) {
    try { await S.previewPlayback({ action: 'stop' }); } catch (e) { console.warn(e); }
  }
  try { if (S.eject) await S.eject(); } catch (e) { console.warn('eject failed', e); }
  state.videoPath = null;
  state.streams = [];
  state.primaryIndex = null;
  state.secondaryIndex = null;
  state.merged = null;
  state.review = null;
  state.reviewWindow = null;
  state.notes = {};
  state.chosen = {};
  state.edits = {};
  state.layerEdits = {};
  state.groupSel = {};
  state.fixWarnings = {};
  state.recheck = [];
  state.selected = null;
  state.drag = null;
  state.bases = {};
  state.pendingBase = null;
  state.statuses = {};
  state.windowScroll = {};
  state.playingWindow = null;
  state.pausedWindow = null;
  state.langsCollapsed = false;
  state.diagnostics = {};
  actions.classList.add('hidden');
  actions.classList.remove('review-mode');
  review.classList.add('hidden');
  setup.classList.remove('hidden');
  btnSave.classList.add('hidden');
  btnChangeLangs.classList.add('hidden');
  dropzone.classList.remove('hidden');
  topbarName.textContent = '';
  sel('#filename').textContent = '';
  sel('#primaryOptions').innerHTML = '';
  sel('#secondaryOptions').innerHTML = '';
  renderReview();
  updateButtonStates();
  updatePlaybackButtons();
}

btnChangeLangs.addEventListener('click', showSetup);
btnChangeLangsSummary.addEventListener('click', () => {
  state.langsCollapsed = false;
  updateSetup();
});
btnEject.addEventListener('click', eject);

// ---------- actions ----------
btnPreview.addEventListener('click', async () => {
  try {
    btnPreview.disabled = true;
    setStatus('extracting and merging subtitle streams…');
    state.merging = true;
    state.mergeProgress = -1;
    let stats;
    try {
      stats = await S.mergeSubtitles({
        videoPath: state.videoPath,
        primaryIndex: state.primaryIndex,
        secondaryIndex: state.secondaryIndex,
      });
    } finally {
      state.merging = false;
      progwrap.classList.add('hidden');
    }
    state.merged = stats;
    state.review = stats.review || null;
    state.reviewWindow = null;
    state.notes = {};
    state.chosen = {};
    state.edits = {};
    state.layerEdits = {};
    state.groupSel = {};
    state.fixWarnings = {};
    state.recheck = [];
    state.selected = null;
    state.drag = null;
    state.bases = {};
    state.pendingBase = null;
    state.statuses = {};
    state.windowScroll = {};
    state.playingWindow = null;
    state.pausedWindow = null;
    updatePlaybackButtons();
    renderReview();
    enterReviewMode();

    setStatus(`merged: ${stats.styles} styles, ${stats.lines} lines → launching preview…`);
    const first = state.review && state.review.windows && state.review.windows[0];
    const close = first ? first.peak : pickEventTime(stats.events);
    await S.launchPreview({
      videoPath: state.videoPath,
      mergedPath: stats.out,
      reportPath: stats.report_path,
      seekTo: close,
    });
    syncBaseToPreview();
    if (state.review) {
      setStatus(`preview open. ${(state.review.windows || []).length} review window(s), ` +
        `${(state.review.conflicts || []).length} conflict(s). Pick a window, then "Copy state" to report it.`);
    } else {
      setStatus('preview open. Conflict data was unavailable for this merge — see the log.');
    }
    btnAnother.disabled = false;
    btnSave.disabled = false;
    btnPreview.disabled = false;
    try { await S.fitWindow(); } catch (e) { console.warn('fit-window failed', e); }
  } catch (e) {
    setStatus('preview failed: ' + e.message, true);
    btnPreview.disabled = false;
  }
});

btnAnother.addEventListener('click', async () => {
  if (!state.merged || !state.merged.events) return;
  const res = await S.jumpRandomFrame({ videoPath: state.videoPath, events: state.merged.events });
  if (res && res.ok) {
    setStatus(`jumped to another frame at t≈${res.time.toFixed(2)}s`);
  } else {
    setStatus('jump failed — see logs', true);
  }
});

btnSave.addEventListener('click', async () => {
  dblSave();
});

async function dblSave() {
  btnSave.disabled = true;
  try {
    setStatus('saving merged subs next to the video…');
    const res = await S.confirmSave({
      mergedPath: state.merged && state.merged.out,
      videoPath: state.videoPath,
      primaryLang: (state.streams.find(s => s.index === state.primaryIndex)?.tags?.language) || '',
      secondaryLang: (state.streams.find(s => s.index === state.secondaryIndex)?.tags?.language) || '',
      reportPath: state.merged && state.merged.report_path,
      fixes: chosenFixes(),
      comments: { ...state.notes },
      statuses: { ...state.statuses },
      bases: { ...state.bases },
    });
    if (res.ok) {
      let msg = `saved: ${res.dest}`;
      if (res.applied) msg += ` (${res.applied} fix(es) applied)`;
      if (res.log) msg += ` — review log: ${res.log}`;
      if (res.logError) msg += ` — log failed: ${res.logError}`;
      setStatus(msg, !!res.logError);
    } else {
      setStatus('save failed: ' + (res.error || 'unknown'), true);
      btnSave.disabled = false;
    }
  } catch (e) {
    setStatus('save failed: ' + e.message, true);
    btnSave.disabled = false;
  }
}

// ---------- review UI ----------
function reviewEvents() {
  const map = new Map();
  for (const e of (state.review?.events || [])) map.set(e.id, e);
  return map;
}

function surfaceBadge(surface) {
  const cls = surface === 'required' ? 'required' : surface === 'review' ? 'review' : '';
  return `<span class="badge ${cls}">${escapeHtml(surface)}</span>`;
}

function windowStatus(w) { return state.statuses[w.id] || 'open'; }
function windowChanged(w) {
  const m = state.chosen[w.id];
  return !!(m && Object.keys(m).length);
}
function statusBadge(w) {
  const st = windowStatus(w);
  const label = st === 'done' ? 'done' : st === 'again' ? 'check again' : 'open';
  return `<span class="badge ${st}">${label}</span>`;
}
function changedBadge(w) {
  return windowChanged(w) ? '<span class="badge changed">changed</span>' : '';
}
function rowBadges(w) {
  return `<span class="row-badges">${statusBadge(w)}${changedBadge(w)}</span>`;
}
function renderReviewSummary() {
  if (!state.review) { reviewSummary.textContent = ''; return; }
  const wins = state.review.windows || [];
  const done = wins.filter(w => windowStatus(w) === 'done').length;
  const again = wins.filter(w => windowStatus(w) === 'again').length;
  const changed = wins.filter(windowChanged).length;
  const s = state.review.summary || {};
  reviewSummary.textContent =
    `${wins.length} windows · ${done} done · ${changed} changed · ${again} to recheck · ${s.conflicts || 0} conflicts`;
}
function updateWindowStatus(w) {
  if (!w) return;
  windowStatusBadge.innerHTML = `${statusBadge(w)} ${windowChanged(w) ? changedBadge(w) : ''}`;
  const done = windowStatus(w) === 'done';
  btnDone.innerHTML = done ? ICONS.reopen : ICONS.done;
  btnDone.title = done ? 'Reopen window' : 'Mark done';
  btnDone.setAttribute('aria-label', btnDone.title);
  btnAgain.title = 'Check again';
  btnAgain.setAttribute('aria-label', 'Check again');
  updatePlaybackStickyOffset();
}
function updateRowBadges(id) {
  const w = (state.review?.windows || []).find(x => x.id === id);
  const row = windowList.querySelector(`.wrow[data-id="${id}"]`);
  if (w && row) {
    const holder = row.querySelector('.row-badges');
    if (holder) holder.outerHTML = rowBadges(w);
  }
  renderReviewSummary();
  if (state.reviewWindow && state.reviewWindow.id === id) updateWindowStatus(state.reviewWindow);
}

function filteredWindows() {
  if (!state.review) return [];
  const surface = filterSurface.value;
  const cause = filterCause.value;
  const statusFilter = filterStatus.value;
  return (state.review.windows || []).filter(w => {
    if (surface === 'required' && w.surface !== 'required') return false;
    if (surface === 'review' && w.surface !== 'review' && w.surface !== 'required') return false;
    if (cause && !(w.causes || [w.cause]).includes(cause)) return false;
    if (statusFilter && windowStatus(w) !== statusFilter) return false;
    return true;
  });
}

function renderReview() {
  const r = state.review;
  if (!r) {
    review.classList.add('hidden');
    return;
  }
  review.classList.remove('hidden');
  renderReviewSummary();

  const causes = new Set();
  for (const w of (r.windows || [])) {
    for (const c of (w.causes || [w.cause])) if (c) causes.add(c);
  }
  filterCause.innerHTML = '<option value="">All causes</option>' +
    [...causes].sort().map(c => `<option value="${escapeHtml(c)}">${escapeHtml(c)}</option>`).join('');
  filterCause.value = '';
  filterSurface.value = 'review';
  filterStatus.value = '';
  filterAndRenderList();
}

function filterAndRenderList() {
  const wins = filteredWindows();
  windowList.innerHTML = '';
  if (!wins.length) {
    state.reviewWindow = null;
    windowEmpty.textContent = (state.review.windows || []).length
      ? 'No windows match this filter.'
      : 'No conflict windows. Nothing to review.';
    windowEmpty.classList.remove('hidden');
    windowContent.classList.add('hidden');
    return;
  }
  for (const w of wins) {
    const row = document.createElement('div');
    row.className = 'wrow';
    row.dataset.id = w.id;
    row.innerHTML =
      `<div class="line1"><span>#${w.id} ${escapeHtml(fmtClock(w.start))}–${escapeHtml(fmtClock(w.end))}</span>` +
      `<span>${rowBadges(w)} ${surfaceBadge(w.surface)}</span></div>` +
      `<div class="line2">${escapeHtml(w.kind || '')} · ${escapeHtml(w.cause || '')} · sev ${w.severity} · ` +
      `${(w.events || []).length} event(s)</div>`;
    row.addEventListener('click', () => selectWindow(w.id, true, true));
    windowList.appendChild(row);
  }
  const keep = wins.find(w => state.reviewWindow && w.id === state.reviewWindow.id);
  selectWindow((keep || wins[0]).id, false);
}

function selectWindow(id, scroll = true, seek = false) {
  const w = (state.review?.windows || []).find(x => x.id === id);
  if (!w) return;
  // Remember the candidate panel position per window. A first visit starts at
  // the top instead of inheriting the previous window's scroll offset.
  if (state.reviewWindow && state.reviewWindow.id !== id) {
    state.windowScroll[state.reviewWindow.id] = windowDetail.scrollTop;
    state.selected = null;
    if (state.drag) {
      state.drag = null;
      if (S.cancelDrag) S.cancelDrag();
    }
  }
  if ((state.playingWindow !== null && state.playingWindow !== id) ||
      (state.pausedWindow !== null && state.pausedWindow !== id)) {
    state.playingWindow = null;
    state.pausedWindow = null;
    S.previewPlayback({ action: 'stop' }).catch(() => {});
  }
  state.reviewWindow = w;
  reviewNote.value = state.notes[id] || '';
  syncBaseToPreview();

  for (const row of windowList.querySelectorAll('.wrow')) {
    row.classList.toggle('active', Number(row.dataset.id) === id);
  }
  renderWindowDetail(w);
  resetWindowProgress();
  windowDetail.scrollTop = state.windowScroll[id] || 0;
  if (scroll) {
    const row = windowList.querySelector(`.wrow[data-id="${id}"]`);
    if (row && row.scrollIntoView) row.scrollIntoView({ block: 'nearest' });
  }
  updatePlaybackButtons();
  if (seek) seekToWindow(w);
}

async function seekToWindow(w) {
  if (!w || !state.merged) return;
  state.playingWindow = null;
  state.pausedWindow = null;
  resetWindowProgress();
  updatePlaybackButtons();
  try {
    const res = await S.seekPreview({
      videoPath: state.videoPath,
      mergedPath: state.merged.out,
      time: w.peak,
    });
    if (res && res.ok) {
      state.lastPreviewTime = w.peak;
      setStatus(`previewing window ${w.id} at ${fmtClock(w.peak)}`);
    }
    else setStatus('preview seek failed: ' + ((res && res.error) || 'see logs'), true);
  } catch (e) {
    setStatus('preview seek failed: ' + e.message, true);
  }
}

function chosenFixes() {
  const out = [];
  for (const [windowId, byEvent] of Object.entries(state.chosen)) {
    for (const [eventId, suggestionId] of Object.entries(byEvent || {})) {
      if (!Number.isInteger(suggestionId) || suggestionId < 0) continue;
      const key = `${windowId}:${eventId}`;
      const fix = { windowId: Number(windowId), suggestionIndex: suggestionId };
      const edit = state.edits[key];
      if (edit && edit.dirty && edit.suggestionId === suggestionId && edit.override) {
        fix.override = { ...edit.override };
      }
      const le = state.layerEdits[key];
      if (le) {
        fix.override = fix.override || {};
        if (Number.isFinite(Number(le.layer))) fix.override.layer = Number(le.layer);
        if (le.ownLayer) fix.override.ownLayer = true;
      }
      const note = state.notes[windowId];
      if (note) fix.comment = note;
      out.push(fix);
    }
  }
  return out;
}

async function applyPreviewTo(seekTo, forceStop = false) {
  if (!state.review || !state.merged) return;
  const keepPlaying = !forceStop && state.playingWindow !== null &&
    state.reviewWindow && state.playingWindow === state.reviewWindow.id;
  if (!keepPlaying) {
    state.playingWindow = null;
    updatePlaybackButtons();
  }
  state.pendingPreview = { seekTo, keepPlaying };
  if (state.applyingPreview) return;
  state.applyingPreview = true;
  try {
    while (state.pendingPreview) {
      const { seekTo: target, keepPlaying: keep } = state.pendingPreview;
      state.pendingPreview = null;
      setStatus(`updating preview with ${chosenFixes().length} chosen fix(es)…`);
      const res = await S.applyPreview({
        fixes: chosenFixes(),
        seekTo: keep ? null : target,
        keepPlaying: keep,
      });
      if (!res || !res.ok) {
        setStatus('preview update failed: ' + ((res && res.error) || 'see logs'), true);
        break;
      }
      if (keep && res.keepPlaying === false) {
        state.playingWindow = null;
        updatePlaybackButtons();
      }
      updateAppliedIndicators(res);
      if (state.reviewWindow) refreshAppliedIndicators(state.reviewWindow);
      // Warm the boxes for the frame we are paused on, so the next mouse-down
      // does not wait for the CLI/libass render.
      if (S.previewBoxes) {
        const warm = Number.isFinite(target) ? target : state.lastPreviewTime;
        if (Number.isFinite(warm)) S.previewBoxes({ time: warm }).catch(() => {});
      }
      const warned = Object.keys(state.fixWarnings).length;
      const remaining = state.recheck.length;
      let extra = '';
      if (warned) extra += `; warning: ${warned} fix(es) leave the safe area or frame`;
      if (remaining) extra += `; re-check: ${remaining} estimated conflict(s) still touch changed events`;
      setStatus((keep
        ? `preview updated — ${chosenFixes().length} chosen fix(es); playback continues.`
        : `preview updated — ${chosenFixes().length} chosen fix(es); changes are preview-only until save.`) + extra);
    }
  } catch (e) {
    setStatus('preview update failed: ' + e.message, true);
  } finally {
    state.applyingPreview = false;
  }
}

function chooseFix(w, eventId, suggestionId) {
  const byEvent = { ...(state.chosen[w.id] || {}) };
  if (suggestionId >= 0) byEvent[eventId] = suggestionId;
  else delete byEvent[eventId];
  delete state.edits[`${w.id}:${eventId}`];
  if (suggestionId < 0) delete state.layerEdits[`${w.id}:${eventId}`];
  if (Object.keys(byEvent).length) state.chosen[w.id] = byEvent;
  else delete state.chosen[w.id];
  updateRowBadges(w.id);
  // Re-render so a paired sign can surface the "same move as #N" candidate as
  // soon as its sibling gets a fix. Keep the user's place in the list.
  const scroll = windowDetail.scrollTop;
  renderWindowDetail(w);
  windowDetail.scrollTop = scroll;
  state.windowScroll[w.id] = scroll;
  // While paused, keep the current frame and only reload the subtitle file, so
  // the user can compare fixes against the same image.
  applyPreviewTo(state.pausedWindow === w.id ? null : eventSeek(w, eventId));
}

function eventSeek(w, eventId) {
  const g = (w.byEvent || []).find(x => x.event === eventId);
  if (g && Number.isFinite(g.seek)) return g.seek;
  const s = (w.suggestions || []).find(x => x.event === eventId && Number.isFinite(x.seek));
  if (s) return s.seek;
  const ev = reviewEvents().get(eventId);
  if (ev) return Math.min(ev.start + 0.3, Math.max(ev.start, (ev.start + ev.end) / 2));
  return w.peak;
}

function playbackSettings() {
  return {
    loop: optLoop.checked,
    muted: !optUnmute.checked,
    speed: Number(optSpeed.value) || 1,
  };
}

function updateWindowProgress(pos) {
  if (!pos || !Number.isFinite(pos.time) || !Number.isFinite(pos.start) || !Number.isFinite(pos.end) || pos.end <= pos.start) {
    windowProgressFill.style.width = '0%';
    windowProgress.setAttribute('aria-valuenow', '0');
    return;
  }
  const pct = Math.max(0, Math.min(100, ((pos.time - pos.start) / (pos.end - pos.start)) * 100));
  windowProgressFill.style.width = pct.toFixed(1) + '%';
  windowProgress.setAttribute('aria-valuenow', String(Math.round(pct)));
  state.playbackPos = { ...pos };
  state.lastPreviewTime = pos.time;
}

function resetWindowProgress() {
  state.playbackPos = null;
  updateWindowProgress(null);
}

function updatePlaybackButtons() {
  const w = state.reviewWindow;
  const playing = !!(w && state.playingWindow === w.id);
  const paused = !!(w && state.pausedWindow === w.id);
  btnStopWindow.disabled = !(playing || paused);
  btnPauseWindow.disabled = !(playing || paused);
  btnPauseWindow.innerHTML = paused ? ICONS.play : ICONS.pause;
  btnPauseWindow.title = paused ? 'Resume' : 'Pause (hold this frame)';
  btnPauseWindow.setAttribute('aria-label', btnPauseWindow.title);
  // Once a window is playing or paused, Play restarts from the window start,
  // so use a previous-track glyph instead of duplicating the play/resume icon.
  btnPlayWindow.innerHTML = (playing || paused) ? ICONS.prev : ICONS.play;
  btnPlayWindow.title = (playing || paused) ? 'Restart window' : 'Play window';
  btnPlayWindow.setAttribute('aria-label', btnPlayWindow.title);
  playState.textContent = '';
}

async function playWindow() {
  const w = state.reviewWindow;
  if (!w) return;
  const settings = playbackSettings();
  btnPlayWindow.disabled = true;
  try {
    const res = await S.previewPlayback({ action: 'play', start: w.start, end: w.end, ...settings });
    if (res && res.ok) {
      state.playingWindow = w.id;
      state.pausedWindow = null;
      updateWindowProgress({ time: w.start, start: w.start, end: w.end });
      setStatus(`playing window ${w.id} ${settings.loop ? 'on loop' : 'once'}${settings.muted ? ', muted' : ''} at ${settings.speed}x`);
    } else {
      setStatus('playback failed: ' + ((res && res.error) || 'see logs'), true);
    }
  } finally {
    btnPlayWindow.disabled = false;
    updatePlaybackButtons();
  }
}

async function stopWindow() {
  const w = state.reviewWindow;
  if (!w) return;
  await S.previewPlayback({ action: 'stop', start: w.start });
  state.playingWindow = null;
  state.pausedWindow = null;
  resetWindowProgress();
  updatePlaybackButtons();
  setStatus('playback stopped');
}

async function pauseWindow() {
  const w = state.reviewWindow;
  if (!w) return;
  const playing = state.playingWindow === w.id;
  const paused = state.pausedWindow === w.id;
  if (!playing && !paused) return;
  try {
    if (paused) {
      const res = await S.previewPlayback({
        action: 'resume', start: w.start, end: w.end, ...playbackSettings(),
      });
      if (!res || !res.ok) throw new Error((res && res.error) || 'see logs');
      state.pausedWindow = null;
      state.playingWindow = w.id;
      setStatus(`resumed window ${w.id}`);
    } else {
      const res = await S.previewPlayback({ action: 'pause', start: w.start });
      if (!res || !res.ok) throw new Error((res && res.error) || 'see logs');
      state.playingWindow = null;
      state.pausedWindow = w.id;
      setStatus(`paused window ${w.id}; picking a fix updates this frame`);
    }
  } catch (e) {
    setStatus('pause failed: ' + e.message, true);
  }
  updatePlaybackButtons();
}

function updatePlaybackSettings() {
  updatePlaybackButtons();
  const w = state.reviewWindow;
  if (w && state.playingWindow === w.id) {
    S.previewPlayback({ action: 'update', start: w.start, end: w.end, ...playbackSettings() })
      .catch(e => setStatus('playback settings failed: ' + e.message, true));
  }
}

function windowGroups(w) {
  if (Array.isArray(w.byEvent) && w.byEvent.length) {
    return w.byEvent.map(g => ({ event: g.event, ids: g.suggestionIds || [], seek: g.seek, note: g.note || '', related: g.related || [] }));
  }
  // Backward compatibility for reports written before per-event grouping.
  const map = new Map();
  (w.suggestions || []).forEach((s, i) => {
    if (!map.has(s.event)) map.set(s.event, []);
    map.get(s.event).push(i);
  });
  return [...map.entries()].map(([event, ids]) => {
    const s = (w.suggestions || []).find((x, i) => ids.includes(i) && Number.isFinite(x.seek));
    return { event, ids, seek: s ? s.seek : undefined, note: '' };
  });
}

function eventColors(ev) {
  const raw = (ev && ev.raw) || '';
  const re = /\\([1-4]?c)&H([0-9A-Fa-f]{6})&/g;
  const seen = new Set();
  const out = [];
  let m;
  while ((m = re.exec(raw)) !== null) {
    const bgr = m[2];
    const rgb = '#' + bgr.slice(4, 6) + bgr.slice(2, 4) + bgr.slice(0, 2);
    if (!seen.has(rgb)) {
      seen.add(rgb);
      out.push(rgb);
    }
  }
  if (!out.length) out.push('#ffffff');
  return out.slice(0, 5);
}

function swatchesFor(ev) {
  return '<span class="swatches">' +
    eventColors(ev).map(c => `<span class="swatch" style="background:${c}" title="${c}"></span>`).join('') +
    '</span>';
}

function eventDetailsHtml(ev) {
  if (!ev) return '';
  const decisions = (ev.decisions || []).map(d =>
    `<div class="dec ${d.level >= 2 ? 'warn' : ''}">${escapeHtml(d.rule)} · ${escapeHtml(d.levelName)} · ${escapeHtml(d.surface)}` +
    `${d.reason ? ' — ' + escapeHtml(d.reason) : ''}</div>`).join('');
  const box = ev.exactBBox || ev.bbox;
  const boxLabel = ev.exactBBox
    ? `exact bbox ${fmtNum(box.x0)},${fmtNum(box.y0)} → ${fmtNum(box.x1)},${fmtNum(box.y1)} @ ${escapeHtml(fmtClock(ev.exactSeek))}`
    : `bbox ${fmtNum(box.x0)},${fmtNum(box.y0)} → ${fmtNum(box.x1)},${fmtNum(box.y1)} · estimate`;
  return `<details class="event-details ev ${ev.role}">` +
    `<summary>Event details · #${ev.id} ${escapeHtml(ev.role)}</summary>` +
    `<div class="meta">#${ev.id} · ${escapeHtml(ev.track)} ${escapeHtml(ev.role)} · ${escapeHtml(ev.style)} · ` +
    `${escapeHtml(fmtClock(ev.start))}–${escapeHtml(fmtClock(ev.end))} · align ${ev.align}` +
    `${ev.pos ? ` · pos ${fmtNum(ev.pos.x)},${fmtNum(ev.pos.y)}` : ''} · importance ${fmtNum(ev.importance, 2)}</div>` +
    `<div class="txt">${escapeHtml(ev.text || '')}</div>` +
    `<div class="dec">${boxLabel}${ev.moving ? ' · moving' : ''}` +
    `${transformLabel(ev) ? ` · transform ${escapeHtml(transformLabel(ev))}` : ''}</div>` +
    decisions +
    (ev.raw ? `<details><summary class="dec">raw ASS</summary><div class="raw">${escapeHtml(ev.raw)}</div></details>` : '') +
    '</details>';
}

function updatePlaybackStickyOffset() {
  const p = sel('#playback');
  const h = p ? p.getBoundingClientRect().height : 0;
  document.documentElement.style.setProperty('--playback-h', `${Math.round(h || 44) + 2}px`);
}

function siblingChosenMove(w, g, chosenForWindow, suggestions) {
  for (const rid of (g.related || [])) {
    const sid = chosenForWindow[rid];
    if (!Number.isInteger(sid) || sid < 0) continue;
    const s = suggestions[sid];
    if (!s || s.kind !== 'nudge') continue;
    if (!s.dx && !s.dy) continue;
    return { event: rid, suggestion: s };
  }
  return null;
}

function moveMatches(a, b) {
  return a && b && a.kind === 'nudge' && b.kind === 'nudge' &&
    Math.abs((a.dx || 0) - (b.dx || 0)) < 0.01 &&
    Math.abs((a.dy || 0) - (b.dy || 0)) < 0.01;
}

function orderForSiblingMove(ids, suggestions, match) {
  if (!match) return { ids, matchId: null };
  const i = ids.findIndex(id => moveMatches(suggestions[id], match.suggestion));
  if (i < 0) return { ids, matchId: null };
  const matchId = ids[i];
  if (i === 0) return { ids, matchId };
  return { ids: [matchId, ...ids.slice(0, i), ...ids.slice(i + 1)], matchId };
}

function eventLayerEdit(w, eventId) {
  return state.layerEdits[`${w.id}:${eventId}`] || null;
}

function effectiveLayer(w, eventId, ev) {
  const le = eventLayerEdit(w, eventId);
  if (le && Number.isFinite(Number(le.layer))) return Number(le.layer);
  return Number(ev && ev.layer) || 0;
}

function eventLayerEditAny(eventId) {
  let found = null;
  const suffix = `:${eventId}`;
  for (const [key, le] of Object.entries(state.layerEdits)) {
    if (key.endsWith(suffix)) found = le;
  }
  return found;
}

function allLayerBounds() {
  let min = Infinity;
  let max = -Infinity;
  for (const ev of (state.review?.events || [])) {
    const le = eventLayerEditAny(ev.id);
    const layer = le && Number.isFinite(Number(le.layer)) ? Number(le.layer) : Number(ev.layer) || 0;
    if (layer < min) min = layer;
    if (layer > max) max = layer;
  }
  if (!Number.isFinite(min)) min = 0;
  if (!Number.isFinite(max)) max = 0;
  return { min, max };
}

function ensureChosenCarrier(w, eventId) {
  const chosen = state.chosen[w.id] || (state.chosen[w.id] = {});
  if (Number.isInteger(chosen[eventId])) return true;
  const g = (w.byEvent || []).find(x => x.event === eventId);
  const ids = g && Array.isArray(g.suggestionIds)
    ? g.suggestionIds
    : (w.suggestions || []).map((s, i) => s && s.event === eventId ? i : -1).filter(i => i >= 0);
  let id = ids.find(i => w.suggestions[i] && w.suggestions[i].kind === 'top');
  if (id === undefined) id = ids.find(i => w.suggestions[i] && w.suggestions[i].kind !== 'hide');
  if (id === undefined) return false;
  chosen[eventId] = id;
  return true;
}

function setLayerEdit(w, eventId, patch) {
  if (!ensureChosenCarrier(w, eventId)) {
    setStatus(`#${eventId}: no fix candidate can carry a layer change`, true);
    return;
  }
  const key = editKey(w, eventId);
  const cur = { ...(state.layerEdits[key] || {}) };
  Object.assign(cur, patch || {});
  if (cur.layer !== undefined && cur.layer !== null) cur.layer = Number(cur.layer);
  if (!Number.isFinite(cur.layer)) delete cur.layer;
  if (!cur.ownLayer) delete cur.ownLayer;
  if (cur.layer === undefined && !cur.ownLayer) delete state.layerEdits[key];
  else state.layerEdits[key] = cur;
  const scroll = windowDetail.scrollTop;
  renderWindowDetail(w);
  windowDetail.scrollTop = scroll;
  state.windowScroll[w.id] = scroll;
  applyPreviewTo(state.pausedWindow === w.id ? null : eventSeek(w, eventId));
  const ev = reviewEvents().get(eventId);
  setStatus(`#${eventId}: layer ${effectiveLayer(w, eventId, ev)}${cur.ownLayer ? ', own layer' : ''}`);
}

function collisionEligible(ev) {
  const raw = String((ev && ev.raw) || '');
  if (/\\pos\s*\(/.test(raw) || /\\move\s*\(/.test(raw) ||
      /\\org\s*\(/.test(raw) || /\\t\s*\(/.test(raw)) return false;
  if (/^(banner|scroll\s+(up|down))/i.test(String((ev && ev.effect) || '').trim())) return false;
  return true;
}

function boxesOverlap(a, b) {
  if (!a || !b) return false;
  return a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1;
}

// Best-guess collision state for the event header:
// fixed/own -> cannot be pushed; possible -> eligible but no same-layer peer
// overlaps in time; likely -> a peer is active concurrently; definite -> their
// estimated lifetime boxes also intersect.
function collisionState(w, ev) {
  if (!ev) return { level: 'fixed', label: 'fixed', title: 'no event data' };
  const le = eventLayerEdit(w, ev.id);
  if (le && le.ownLayer) {
    return { level: 'own', label: 'own layer', title: 'This event is kept on a layer no other event uses.' };
  }
  if (!collisionEligible(ev)) {
    return {
      level: 'fixed', label: 'fixed',
      title: 'Explicit position/move/org, an animated \\t, or a Banner/Scroll effect disables collision.',
    };
  }
  const layer = effectiveLayer(w, ev.id, ev);
  const map = reviewEvents();
  let timePeer = false;
  let boxPeer = false;
  for (const id of windowEventIds(w)) {
    if (id === ev.id) continue;
    const peer = map.get(id);
    if (!peer) continue;
    if (Math.max(ev.start, peer.start) >= Math.min(ev.end, peer.end)) continue;
    if (effectiveLayer(w, id, peer) !== layer) continue;
    const peerEdit = eventLayerEdit(w, id);
    if (peerEdit && peerEdit.ownLayer) continue;
    timePeer = true;
    if (boxesOverlap(ev.bbox, peer.bbox)) { boxPeer = true; break; }
  }
  if (boxPeer) {
    return {
      level: 'definite', label: 'definite',
      title: 'Another event on the same layer is active at the same time and their lifetime boxes intersect.',
    };
  }
  if (timePeer) {
    return {
      level: 'likely', label: 'likely',
      title: 'Another event on the same layer is active at the same time; boxes may overlap at some frames.',
    };
  }
  return {
    level: 'possible', label: 'possible',
    title: 'Collision-eligible, but no other event on this layer is active at the same time.',
  };
}

function collisionBadge(w, ev) {
  const c = collisionState(w, ev);
  return `<span class="collision-badge ${c.level}" title="${escapeHtml(c.title)}">${escapeHtml(c.label)}</span>`;
}

function renderWindowDetail(w) {
  windowEmpty.classList.add('hidden');
  windowContent.classList.remove('hidden');
  windowMeta.innerHTML =
    `<div class="big">Window ${w.id} · ${escapeHtml(fmtClock(w.start))}–${escapeHtml(fmtClock(w.end))}</div>` +
    `<div>peak ${escapeHtml(fmtClock(w.peak))} · ${escapeHtml(w.kind || '')} · ${escapeHtml(w.cause || '')} · ` +
    `severity ${w.severity} · ${escapeHtml(w.levelName || '')} ${surfaceBadge(w.surface)}</div>` +
    `<div>${escapeHtml(w.reason || '')}</div>` +
    '<div id="window-recheck" class="fix-warning hidden"></div>';

  updateWindowStatus(w);
  const byId = reviewEvents();
  const chosenForWindow = state.chosen[w.id] || {};
  windowSuggestions.innerHTML = '<h3>Fix candidates</h3>' +
    '<div class="sug-sub" style="margin:0 0 6px">Choose a fix per event; Original keeps it unchanged. Selected fixes apply on save. ' +
    '<b>best</b> is the top-ranked candidate for that event alone; <b>safe</b> is a small sign-only move that met the auto-apply threshold. Neither accounts for the other events’ chosen fixes — the combined layout appears when the preview reloads. ' +
    'Click/drag a subtitle in the preview (Shift axis, Alt fine, arrows nudge, R reset). ' +
    'Ctrl+drag marks the translated area; Ctrl+drag its corners or red handle reshapes/rotates it; click a green anchor (or the arrow buttons below) to place the selected subtitle; B clears the base.</div>';

  const groupBar = document.createElement('div');
  groupBar.className = 'group-move-bar';
  groupBar.innerHTML =
    '<span class="group-move-title">Group move</span>' +
    '<span class="group-count">tick events below</span>' +
    '<label>dx <input class="group-dx" type="number" step="1" value="0"></label>' +
    '<label>dy <input class="group-dy" type="number" step="1" value="0"></label>' +
    '<button type="button" class="small group-apply" disabled>Move selected</button>' +
    '<button type="button" class="small pick-best" title="Choose the top-ranked candidate for every event in this window, then preview the combined layout">Pick best all</button>' +
    '<label class="group-style">style <select class="group-style-select"><option value="">all styles</option></select></label>' +
    '<button type="button" class="small group-split" title="Give each selected/style event its own new layer so they cannot collide">Split layers</button>' +
    '<button type="button" class="small minor group-clear" disabled>Clear selection</button>' +
    '<button type="button" class="small minor base-clear" disabled>Clear base</button>' +
    '<span class="sug-sub">Tick one or more event headings, then move them together. Split layers uses ticked events, or every event of the chosen style when nothing is ticked.</span>';
  const styleSel = groupBar.querySelector('.group-style-select');
  const styles = new Set();
  for (const id of windowEventIds(w)) {
    const ev = byId.get(id);
    if (ev && ev.style) styles.add(ev.style);
  }
  for (const s of [...styles].sort()) {
    const opt = document.createElement('option');
    opt.value = s;
    opt.textContent = s;
    styleSel.appendChild(opt);
  }
  groupBar.querySelector('.group-split').addEventListener('click', () => splitGroupLayers(w, groupBar));
  groupBar.querySelector('.pick-best').addEventListener('click', () => pickBestForWindow(w));
  groupBar.querySelector('.group-apply').addEventListener('click', () => {
    const dx = Number(groupBar.querySelector('.group-dx').value) || 0;
    const dy = Number(groupBar.querySelector('.group-dy').value) || 0;
    applyGroupMove(w, dx, dy);
  });
  groupBar.querySelector('.group-clear').addEventListener('click', () => {
    delete state.groupSel[w.id];
    for (const cb of windowSuggestions.querySelectorAll('.group-pick input')) cb.checked = false;
    updateGroupMoveBar(w);
  });
  groupBar.querySelector('.base-clear').addEventListener('click', () => {
    const sel = state.selected;
    if (sel && sel.windowId === w.id) delete state.bases[baseKey(w, sel.eventId)];
    state.pendingBase = null;
    syncBaseToPreview();
    refreshBaseTools(w);
    setStatus('base cleared');
  });
  windowSuggestions.appendChild(groupBar);
  const baseTools = document.createElement('div');
  baseTools.id = 'base-tools';
  baseTools.className = 'base-tools hidden';
  windowSuggestions.appendChild(baseTools);

  const suggestions = w.suggestions || [];
  const groups = windowGroups(w);
  if (!groups.length) {
    const p = document.createElement('div');
    p.className = 'sug-sub';
    p.textContent = 'No candidates for this window.';
    windowSuggestions.appendChild(p);
    updateGroupMoveBar(w);
    return;
  }
  for (const g of groups) {
    const ev = byId.get(g.event);
    const wrap = document.createElement('div');
    wrap.className = 'event-fixes';
    wrap.dataset.event = String(g.event);
    if (state.selected && state.selected.windowId === w.id && state.selected.eventId === g.event) {
      wrap.classList.add('selected');
    }

    const head = document.createElement('div');
    head.className = 'fix-event-head';
    const seekTxt = Number.isFinite(g.seek) ? ` · ${fmtClock(g.seek)}` : '';
    const title = ev ? (ev.text || `Event #${g.event}`) : `Event #${g.event}`;
    const le = eventLayerEdit(w, g.event);
    const layer = effectiveLayer(w, g.event, ev);
    const own = !!(le && le.ownLayer);
    const layerLabel = own && !(le && Number.isFinite(Number(le.layer))) ? 'L*' : `L${layer}`;
    head.innerHTML =
      `<div class="fix-event-title">${escapeHtml(title)}</div>` +
      `<div class="fix-event-sub">#${g.event} ${ev ? escapeHtml(ev.role) : ''}${escapeHtml(seekTxt)} ${swatchesFor(ev)} ` +
      `${collisionBadge(w, ev)} ` +
      '<span class="layer-tools">' +
      `<span class="layer-index" title="Effective ASS layer${own ? ' (own layer assigned at apply)' : ''}">${layerLabel}</span>` +
      '<button type="button" class="small minor layer-btn" data-layer="top" title="Move to a new highest layer">Top</button>' +
      '<button type="button" class="small minor layer-btn" data-layer="up" title="One layer up">↑</button>' +
      '<button type="button" class="small minor layer-btn" data-layer="down" title="One layer down">↓</button>' +
      '<button type="button" class="small minor layer-btn" data-layer="back" title="Move to a new lowest layer">Back</button>' +
      `<label class="own-layer" title="Give this event a layer no other event uses, so nothing can push it"><input type="checkbox" ${own ? 'checked' : ''}> own</label>` +
      '</span></div>';
    const pick = document.createElement('label');
    pick.className = 'group-pick';
    pick.title = 'Select this event for a shared group move';
    const pickBox = document.createElement('input');
    pickBox.type = 'checkbox';
    pickBox.checked = !!(state.groupSel[w.id] || {})[g.event];
    pickBox.addEventListener('change', () => {
      const sel = state.groupSel[w.id] || (state.groupSel[w.id] = {});
      if (pickBox.checked) sel[g.event] = true;
      else delete sel[g.event];
      if (!Object.keys(sel).length) delete state.groupSel[w.id];
      updateGroupMoveBar(w);
    });
    pick.appendChild(pickBox);
    head.querySelector('.fix-event-sub').prepend(pick);
    head.querySelectorAll('[data-layer]').forEach((btn) => {
      btn.addEventListener('click', () => {
        const cur = effectiveLayer(w, g.event, ev);
        const bounds = allLayerBounds();
        const act = btn.dataset.layer;
        const next = act === 'top' ? bounds.max + 1
          : act === 'back' ? bounds.min - 1
            : act === 'up' ? cur + 1 : cur - 1;
        setLayerEdit(w, g.event, { layer: next });
      });
    });
    const ownBox = head.querySelector('.own-layer input');
    if (ownBox) ownBox.addEventListener('change', () => setLayerEdit(w, g.event, { ownLayer: ownBox.checked }));
    wrap.appendChild(head);
    wrap.insertAdjacentHTML('beforeend', eventDetailsHtml(ev));
    const warn = state.fixWarnings[`${w.id}:${g.event}`];
    if (warn) wrap.appendChild(fixWarningDiv(warn));
    if (g.note && g.ids.length) {
      const note = document.createElement('div');
      note.className = 'sug-sub';
      note.textContent = g.note;
      wrap.appendChild(note);
    }

    if (!g.ids.length) {
      const p = document.createElement('div');
      p.className = 'sug-sub';
      p.textContent = g.note || 'No automatic candidate for this event.';
      wrap.appendChild(p);
      windowSuggestions.appendChild(wrap);
      continue;
    }

    const siblingMove = siblingChosenMove(w, g, chosenForWindow, suggestions);
    const ordered = orderForSiblingMove(g.ids, suggestions, siblingMove);

    const originalChecked = !Number.isInteger(chosenForWindow[g.event]);
    const original = document.createElement('label');
    original.className = 'sug' + (originalChecked ? ' recommended' : '');
    original.innerHTML =
      `<input type="radio" name="fix-${w.id}-${g.event}" value="-1" ${originalChecked ? 'checked' : ''}>` +
      '<div class="sug-main"><div class="sug-title">Original (no fix) <span class="badge">keep</span></div>' +
      '<div class="sug-sub">leave this event as the merge produced it</div></div>';
    original.querySelector('input').addEventListener('change', () => chooseFix(w, g.event, -1));
    wrap.appendChild(original);

    for (const id of ordered.ids) {
      const s = suggestions[id];
      if (!s) continue;
      // "Move to top" is rendered as a button in the event heading.
      if (s.kind === 'top') continue;
      const chosen = chosenForWindow[g.event] === id;
      const matchesSibling = id === ordered.matchId;
      const sharedAll = !!(g.related && g.related.length && s.sharedWith &&
        s.sharedWith.length >= g.related.length);
      const label = document.createElement('label');
      label.className = 'sug' + (chosen ? ' recommended' : '');
      const bits = [s.kind];
      if (s.kind === 'nudge') {
        if (s.dx) bits.push(`dx ${signed(s.dx)}`);
        if (s.dy) bits.push(`dy ${signed(s.dy)}`);
        if (!s.at) bits.push('whole move path');
      }
      if (s.kind === 'shrink') bits.push(`font ${Math.round((s.fontScale || 1) * 100)}%`);
      const badges =
        (s.autoApply ? '<span class="badge auto" title="Small sign-only move that met the auto-apply threshold">safe</span> ' : '') +
        (s.manualReview ? '<span class="badge manual" title="Large move; check the preview">large</span> ' : '') +
        (s.recommended ? '<span class="badge" title="Top-ranked candidate for this event on its own; not a window-wide choice">best</span> ' : '') +
        (matchesSibling ? `<span class="badge pair">same move as #${siblingMove.event}</span> ` : '') +
        (!matchesSibling && sharedAll ? `<span class="badge pair">shared by ${1 + s.sharedWith.length} signs</span> ` : '') +
        (chosen ? '<span class="badge auto">chosen</span>' : '');
      label.innerHTML =
        `<input type="radio" name="fix-${w.id}-${g.event}" value="${id}" ${chosen ? 'checked' : ''}>` +
        `<div class="sug-main"><div class="sug-title">${escapeHtml(bits.join(' · '))} ${badges}</div>` +
        `<div class="sug-sub">cost ${fmtNum(s.cost, 2)}` +
        `${s.improvement ? ` · ${fmtNum(s.improvement, 1)}× better` : ''}` +
        `${s.at ? ` · at ${fmtNum(s.at.x)},${fmtNum(s.at.y)}` : ''} · ${escapeHtml(s.reason || '')}</div></div>`;
      label.querySelector('input').addEventListener('change', () => chooseFix(w, g.event, id));
      wrap.appendChild(label);
    }

    const chosenId = chosenForWindow[g.event];
    const chosenSuggestion = Number.isInteger(chosenId) ? suggestions[chosenId] : null;
    if (chosenSuggestion && (chosenSuggestion.kind === 'nudge' || chosenSuggestion.kind === 'shrink')) {
      wrap.appendChild(fixEditor(w, g.event, chosenId, chosenSuggestion));
    }
    windowSuggestions.appendChild(wrap);
  }
  updateGroupMoveBar(w);
  refreshBaseTools(w);
  refreshRecheckNotice(w);
  updatePlaybackStickyOffset();
}

function editKey(w, eventId) { return `${w.id}:${eventId}`; }

function editBase(suggestionId, s) {
  return {
    suggestionId,
    kind: s.kind,
    dx: Number(s.dx) || 0,
    dy: Number(s.dy) || 0,
    fontScale: Math.round((Number(s.fontScale) || 1) * 100),
    at: s.at ? { x: Number(s.at.x) || 0, y: Number(s.at.y) || 0 } : null,
    posTouched: false,
    dirty: false,
  };
}

function overrideForEdit(edit, s) {
  const num = (v, fallback) => {
    const n = Number(v);
    return Number.isFinite(n) ? n : fallback;
  };
  const o = { kind: s.kind };
  o.dx = num(edit.dx, 0);
  o.dy = num(edit.dy, 0);
  if (s.kind === 'shrink') o.fontScale = Math.max(0.05, num(edit.fontScale, 100) / 100);
  if (edit.posTouched && edit.at) {
    o.at = { x: num(edit.at.x, 0), y: num(edit.at.y, 0) };
  }
  return o;
}

let editPreviewTimer = null;
function scheduleEditPreview(w, eventId) {
  clearTimeout(editPreviewTimer);
  editPreviewTimer = setTimeout(() => {
    applyPreviewTo(state.pausedWindow === w.id ? null : eventSeek(w, eventId));
  }, 180);
}

function fixEditor(w, eventId, suggestionId, s) {
  const key = editKey(w, eventId);
  const stored = state.edits[key];
  const edit = stored && stored.suggestionId === suggestionId
    ? { ...stored, at: stored.at ? { ...stored.at } : null }
    : editBase(suggestionId, s);
  const base = editBase(suggestionId, s);
  const box = document.createElement('div');
  box.className = 'fix-editor';

  const fields = [
    { key: 'dx', label: 'dx', step: '1' },
    { key: 'dy', label: 'dy', step: '1' },
  ];
  if (s.kind === 'shrink') fields.push({ key: 'fontScale', label: 'font %', step: '1' });
  if (s.at) {
    fields.push({ key: 'x', label: 'pos x', step: '1' });
    fields.push({ key: 'y', label: 'pos y', step: '1' });
  }

  const persist = () => {
    const n = (v) => Number(v);
    edit.dirty = !Number.isFinite(n(edit.dx)) || n(edit.dx) !== base.dx ||
      !Number.isFinite(n(edit.dy)) || n(edit.dy) !== base.dy ||
      (s.kind === 'shrink' && n(edit.fontScale) !== base.fontScale) ||
      edit.posTouched;
    if (edit.dirty) {
      state.edits[key] = { ...edit, override: overrideForEdit(edit, s) };
    } else {
      delete state.edits[key];
    }
    scheduleEditPreview(w, eventId);
  };

  for (const f of fields) {
    const label = document.createElement('label');
    label.textContent = f.label;
    const input = document.createElement('input');
    input.type = 'number';
    input.step = f.step;
    if (f.key === 'x' || f.key === 'y') {
      input.value = String(edit.at ? edit.at[f.key] : 0);
      input.dataset.pos = f.key;
    } else {
      input.value = String(edit[f.key]);
      input.dataset.field = f.key;
    }
    input.addEventListener('input', () => {
      if (input.dataset.pos) {
        if (!edit.at) edit.at = { x: 0, y: 0 };
        edit.at[input.dataset.pos] = input.value;
        edit.posTouched = true;
      } else {
        edit[input.dataset.field] = input.value;
      }
      persist();
    });
    label.appendChild(input);
    box.appendChild(label);
  }

  const reset = document.createElement('button');
  reset.type = 'button';
  reset.className = 'small minor';
  reset.textContent = 'Reset to candidate';
  reset.addEventListener('click', () => {
    delete state.edits[key];
    const scroll = windowDetail.scrollTop;
    renderWindowDetail(w);
    windowDetail.scrollTop = scroll;
    state.windowScroll[w.id] = scroll;
    applyPreviewTo(state.pausedWindow === w.id ? null : eventSeek(w, eventId));
  });
  box.appendChild(reset);

  const note = document.createElement('div');
  note.className = 'fix-editor-note';
  note.textContent = 'Manual override: dx/dy shift the target, font % scales text, pos x/y set the anchor directly. Changes update the preview and are recorded in the review log.';
  box.appendChild(note);
  return box;
}

function effectiveEdit(edit, s) {
  const n = (v, d) => {
    const x = Number(v);
    return Number.isFinite(x) ? x : d;
  };
  const dx = n(edit.dx, 0);
  const dy = n(edit.dy, 0);
  let at = null;
  if (s.at) {
    if (edit.posTouched && edit.at) {
      at = { x: n(edit.at.x, 0), y: n(edit.at.y, 0) };
    } else {
      at = {
        x: Number(s.at.x) + dx - (Number(s.dx) || 0),
        y: Number(s.at.y) + dy - (Number(s.dy) || 0),
      };
    }
  }
  return { dx, dy, at };
}

function baseNudgeFor(w, eventId) {
  const g = (w.byEvent || []).find(x => x.event === eventId);
  const chosenId = (state.chosen[w.id] || {})[eventId];
  if (Number.isInteger(chosenId)) {
    const s = (w.suggestions || [])[chosenId];
    if (!s) return null;
    if (s.kind !== 'nudge' && s.kind !== 'shrink' && s.kind !== 'top' && s.kind !== 'raise') return null;
    return { id: chosenId, s };
  }
  if (g) {
    let shrink = null;
    for (const id of g.suggestionIds || []) {
      const s = (w.suggestions || [])[id];
      if (s && s.kind === 'nudge') return { id, s };
      if (s && s.kind === 'shrink' && !shrink) shrink = { id, s };
    }
    if (shrink) return shrink;
  }
  // Fallback for events outside a byEvent group or with no nudge: any
  // non-hide candidate can still get a positional override (top/raise support
  // an additional position).
  const idx = (w.suggestions || []).findIndex(s => s.event === eventId && s.kind !== 'hide');
  if (idx >= 0) return { id: idx, s: w.suggestions[idx] };
  return null;
}

function moveEventsBy(w, eventIds, dx, dy, opts = {}) {
  if (!eventIds.length || (!dx && !dy)) return 0;
  const chosen = state.chosen[w.id] || (state.chosen[w.id] = {});
  let moved = 0;
  let skipped = 0;
  for (const eventId of eventIds) {
    const hadChosen = Number.isInteger(chosen[eventId]);
    const base = baseNudgeFor(w, eventId);
    if (!base) { skipped++; continue; }
    const ev = reviewEvents().get(eventId);
    if (!ev) { skipped++; continue; }
    const key = editKey(w, eventId);
    const stored = state.edits[key];
    const baseBox = eventBaseBox(ev, ev.exactSeek, false);
    const live = opts.liveBoxes && opts.liveBoxes.get(eventId);
    const play = previewPlayRes();
    let edit;
    if (!hadChosen) {
      // No chosen candidate yet: move from the event's current/original
      // position, not from the candidate's own offset.
      if (!ev.moving) {
        if ((!baseBox && !ev.pos) || (live && !live.box)) { skipped++; continue; }
        const origAnchor = live
          ? eventAnchorJS(ev, live.box, play, true)
          : eventAnchorJS(ev, baseBox, play);
        edit = {
          suggestionId: base.id,
          kind: base.s.kind,
          dx,
          dy,
          fontScale: Math.round((Number(base.s.fontScale) || 1) * 100),
          at: { x: origAnchor.x + dx, y: origAnchor.y + dy },
          posTouched: true,
          dirty: true,
        };
      } else {
        edit = {
          suggestionId: base.id,
          kind: base.s.kind,
          dx,
          dy,
          fontScale: 100,
          at: null,
          posTouched: false,
          dirty: true,
        };
      }
    } else {
      edit = stored && stored.suggestionId === base.id
        ? { ...stored, at: stored.at ? { ...stored.at } : null }
        : editBase(base.id, base.s);
      // A candidate's `at` was computed from the layout margin. If libass has
      // collision-stacked the event, shift the first drag by that offset so it
      // does not jump to the margin.
      if (live && live.box && base.s.at && !edit.posTouched) {
        const liveAnchor = eventAnchorJS(ev, live.box, play, true);
        const layoutAnchor = eventAnchorJS(ev, baseBox, play);
        edit.at = {
          x: Number(base.s.at.x) + (liveAnchor.x - layoutAnchor.x),
          y: Number(base.s.at.y) + (liveAnchor.y - layoutAnchor.y),
        };
        edit.posTouched = true;
        edit.dx = Number(base.s.dx) || 0;
        edit.dy = Number(base.s.dy) || 0;
      }
      if (base.s.kind === 'top' || base.s.kind === 'raise') {
        // Draw-order fixes have no intrinsic position, but a drag can add one
        // while preserving the layer change.
        if (ev.moving) {
          edit.dx = (Number(edit.dx) || 0) + dx;
          edit.dy = (Number(edit.dy) || 0) + dy;
          edit.at = null;
          edit.posTouched = false;
        } else {
          if ((!baseBox && !ev.pos) || (live && !live.box)) { skipped++; continue; }
          const origAnchor = live
            ? eventAnchorJS(ev, live.box, play, true)
            : eventAnchorJS(ev, baseBox, play);
          const curAt = edit.posTouched && edit.at
            ? { x: Number(edit.at.x), y: Number(edit.at.y) }
            : { x: origAnchor.x + (Number(edit.dx) || 0), y: origAnchor.y + (Number(edit.dy) || 0) };
          edit.at = { x: curAt.x + dx, y: curAt.y + dy };
          edit.posTouched = true;
          edit.dx = 0;
          edit.dy = 0;
        }
        edit.dirty = true;
      } else {
        const eff = effectiveEdit(edit, base.s);
        if (base.s.at) {
          edit.at = { x: eff.at.x + dx, y: eff.at.y + dy };
          edit.posTouched = true;
          edit.dx = eff.dx;
          edit.dy = eff.dy;
        } else {
          edit.dx = eff.dx + dx;
          edit.dy = eff.dy + dy;
          edit.at = null;
          edit.posTouched = false;
        }
        edit.dirty = true;
      }
    }
    edit.override = overrideForEdit(edit, base.s);
    chosen[eventId] = base.id;
    state.edits[key] = edit;
    moved++;
  }
  if (!moved) {
    setStatus(`${opts.source || 'move'}: no positional candidate for the selected event(s); ` +
      'choose a nudge first or move it with the numeric editor', true);
    return 0;
  }
  const scroll = windowDetail.scrollTop;
  renderWindowDetail(w);
  windowDetail.scrollTop = scroll;
  state.windowScroll[w.id] = scroll;
  applyPreviewTo(state.pausedWindow === w.id ? null : eventSeek(w, eventIds[0]));
  const verb = opts.source === 'drag' ? 'drag'
    : opts.source === 'nudge' ? 'nudge'
      : opts.source === 'anchor' ? 'anchor'
        : opts.source === 'group' ? 'group move' : 'move';
  setStatus(`${verb}: ${moved} event(s) by ${signed(dx)},${signed(dy)}` +
    (skipped ? `; ${skipped} skipped (non-positional fix)` : ''));
  uiLog('info', 'preview move', {
    windowId: w.id, eventIds, dx, dy, moved, skipped, source: opts.source || 'move',
  });
  return moved;
}

function applyGroupMove(w, dx, dy) {
  const events = Object.keys(state.groupSel[w.id] || {}).map(Number);
  moveEventsBy(w, events, dx, dy, { source: 'group' });
}

function pickBestForWindow(w) {
  const chosen = state.chosen[w.id] || (state.chosen[w.id] = {});
  let picked = 0;
  for (const g of (w.byEvent || [])) {
    const id = (g.suggestionIds || []).find(i => w.suggestions[i] && w.suggestions[i].recommended);
    if (id === undefined) continue;
    chosen[g.event] = id;
    picked++;
  }
  if (!picked) {
    setStatus('pick best: no recommended candidates in this window', true);
    return;
  }
  const scroll = windowDetail.scrollTop;
  renderWindowDetail(w);
  windowDetail.scrollTop = scroll;
  state.windowScroll[w.id] = scroll;
  applyPreviewTo(state.pausedWindow === w.id ? null : w.peak);
  setStatus(`pick best: chose ${picked} top-ranked candidate(s); checking the combined layout`);
}

function splitGroupLayers(w, groupBar) {
  const picked = Object.keys(state.groupSel[w.id] || {}).map(Number);
  const style = groupBar.querySelector('.group-style-select').value;
  let targets = picked.length ? picked : [...windowEventIds(w)];
  if (style) {
    const map = reviewEvents();
    targets = targets.filter(id => {
      const ev = map.get(id);
      return ev && ev.style === style;
    });
  }
  targets = [...new Set(targets)];
  if (!targets.length) {
    setStatus('split layers: no matching events', true);
    return;
  }
  const map = reviewEvents();
  targets.sort((a, b) => {
    const ea = map.get(a);
    const eb = map.get(b);
    const la = effectiveLayer(w, a, ea);
    const lb = effectiveLayer(w, b, eb);
    return la !== lb ? la - lb : a - b;
  });
  let next = allLayerBounds().max + 1;
  let changed = 0;
  for (const id of targets) {
    if (!ensureChosenCarrier(w, id)) continue;
    const key = editKey(w, id);
    state.layerEdits[key] = { ...(state.layerEdits[key] || {}), layer: next++ };
    changed++;
  }
  const scroll = windowDetail.scrollTop;
  renderWindowDetail(w);
  windowDetail.scrollTop = scroll;
  state.windowScroll[w.id] = scroll;
  applyPreviewTo(state.pausedWindow === w.id ? null : eventSeek(w, targets[0]));
  setStatus(`split layers: ${changed} event(s) got exclusive layers`);
}

function previewPlayRes() {
  const job = (state.review && state.review.job) || {};
  return {
    w: Number(job.playResX) || 640,
    h: Number(job.playResY) || 360,
  };
}

function uiLog(level, message, data) {
  try {
    if (S.debugLog) S.debugLog(level, message, data);
    else if (level === 'error') console.error(message, data || '');
    else console.log(message, data || '');
  } catch (e) { /* logging must never break review */ }
}

const TRANSFORM_TAGS = [
  [/\\frx(-?[\d.]+)/g, 'rotX'],
  [/\\fry(-?[\d.]+)/g, 'rotY'],
  [/\\f(?:rz|r)(-?[\d.]+)/g, 'rotZ'],
  [/\\fax(-?[\d.]+)/g, 'skewX'],
  [/\\fay(-?[\d.]+)/g, 'skewY'],
  [/\\fscx(-?[\d.]+)/g, 'scaleX'],
  [/\\fscy(-?[\d.]+)/g, 'scaleY'],
];

// Summarize the raw ASS transform tags. The Go report already carries this,
// but events copied from older reports may not.
function transformInfoJS(ev) {
  if (ev.transform) return ev.transform;
  const raw = ev.raw || '';
  const out = {};
  for (const [re, key] of TRANSFORM_TAGS) {
    const ms = [...raw.matchAll(re)];
    if (ms.length) {
      const n = Number(ms[ms.length - 1][1]);
      if (Number.isFinite(n) && n !== 0) out[key] = n;
    }
  }
  if (/\\org\(/.test(raw)) {
    const m = raw.match(/\\org\(\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*\)/);
    if (m) out.org = { x: Number(m[1]), y: Number(m[2]) };
  }
  out.animated = /\\t\s*\(/.test(raw);
  const tags = [];
  if (out.rotX) tags.push(`\\frx${out.rotX}`);
  if (out.rotY) tags.push(`\\fry${out.rotY}`);
  if (out.rotZ) tags.push(`\\frz${out.rotZ}`);
  if (out.skewX) tags.push(`\\fax${out.skewX}`);
  if (out.skewY) tags.push(`\\fay${out.skewY}`);
  if (out.org) tags.push(`\\org(${out.org.x},${out.org.y})`);
  if (out.animated) tags.push('\\t(...)');
  if (tags.length) out.tags = tags.join(' ');
  return Object.keys(out).length ? out : null;
}

function transformLabel(ev) {
  const t = transformInfoJS(ev);
  if (!t) return '';
  return t.tags || (t.animated ? '\\t(...)' : 'transform');
}

function eventHasTransform(ev) {
  return !!transformInfoJS(ev);
}

function effectMoves(ev) {
  return /^(banner|scroll\s+up|scroll\s+down)/i.test(String((ev && ev.effect) || '').trim());
}

// Exact ink box for the requested time. Static events keep one mask; animated
// \t transforms carry several samples and moving events translate the nearest
// sample along \move.
function eventExactBox(ev, time) {
  const samples = Array.isArray(ev.boxSamples) ? ev.boxSamples.filter(s => s && s.box) : [];
  let sample = null;
  if (samples.length) {
    sample = samples[0];
    let best = Math.abs((Number(sample.time) || 0) - time);
    for (const s of samples.slice(1)) {
      const d = Math.abs((Number(s.time) || 0) - time);
      if (d < best) { best = d; sample = s; }
    }
  }
  if (!sample && ev.exactBBox) {
    sample = { time: Number(ev.exactSeek), box: ev.exactBBox };
  }
  if (!sample || !sample.box) return null;
  // Estimated samples are already positioned at their sample time. Translating
  // them along \move would count the movement twice.
  if (!ev.moving || sample.estimated || effectMoves(ev)) return sample.box;
  const from = Number(sample.time);
  if (!Number.isFinite(from)) return null;
  return movingBoxAt(ev, time, sample.box, from) || sample.box;
}

// Whether the box shown at this time comes from an exact mask sample or from
// the geometry estimator.
function eventBoxSource(ev, time) {
  const samples = Array.isArray(ev.boxSamples) ? ev.boxSamples.filter(s => s && s.box) : [];
  if (samples.length) {
    let sample = samples[0];
    let best = Math.abs((Number(sample.time) || 0) - time);
    for (const s of samples.slice(1)) {
      const d = Math.abs((Number(s.time) || 0) - time);
      if (d < best) { best = d; sample = s; }
    }
    return sample.estimated ? 'estimate' : 'samples';
  }
  return ev.exactBBox ? 'exact' : 'estimate';
}

// Coarse ink coverage for the sample nearest `time`, if one was measured.
function eventCoverage(ev, time) {
  const samples = Array.isArray(ev.boxSamples) ? ev.boxSamples.filter(s => s && s.coverage) : [];
  if (!samples.length) return ev.coverage || '';
  let sample = samples[0];
  let best = Math.abs((Number(sample.time) || 0) - time);
  for (const s of samples.slice(1)) {
    const d = Math.abs((Number(s.time) || 0) - time);
    if (d < best) { best = d; sample = s; }
  }
  return sample.coverage || ev.coverage || '';
}

const COVER_COLS = 16;
const COVER_ROWS = 8;
const COVER_MIN = 5; // out of 255; ignore nearly empty grid cells

// Test whether a click falls on actual ink inside an exact AABB. Rotated and
// skewed subtitles have large empty corners; without this, clicking near a
// label can select it even though no glyph is there.
function coverageHit(cov, box, x, y) {
  if (!cov || cov.length < COVER_COLS * COVER_ROWS * 2) return true;
  const w = box.x1 - box.x0;
  const h = box.y1 - box.y0;
  if (!(w > 0) || !(h > 0)) return true;
  let gx = Math.floor(((x - box.x0) / w) * COVER_COLS);
  let gy = Math.floor(((y - box.y0) / h) * COVER_ROWS);
  if (gx < 0) gx = 0; else if (gx >= COVER_COLS) gx = COVER_COLS - 1;
  if (gy < 0) gy = 0; else if (gy >= COVER_ROWS) gy = COVER_ROWS - 1;
  const v = parseInt(cov.slice((gy * COVER_COLS + gx) * 2, (gy * COVER_COLS + gx) * 2 + 2), 16);
  return !Number.isFinite(v) || v >= COVER_MIN;
}

// The alignment anchor libass uses for \pos. Margin-based anchors are exact for
// events without \pos, so a first drag does not jump to an estimated centre.
function eventAnchorJS(ev, box, play, preferBox = false) {
  if (ev.pos && Number.isFinite(Number(ev.pos.x)) && Number.isFinite(Number(ev.pos.y))) {
    return { x: Number(ev.pos.x), y: Number(ev.pos.y) };
  }
  if (preferBox && box) return anchorForBoxJS(box, ev.align);
  const a = Number(ev.align) || 2;
  if (Number.isFinite(Number(ev.marginL)) && play && play.w > 0 && play.h > 0) {
    const ml = Number(ev.marginL) || 0;
    const mr = Number(ev.marginR) || 0;
    const mv = Number(ev.marginV) || 0;
    let x;
    switch (a % 3) {
      case 1: x = ml; break;
      case 0: x = play.w - mr; break;
      default: x = play.w / 2;
    }
    let y;
    if (a <= 3) y = play.h - mv;
    else if (a <= 6) y = play.h / 2;
    else y = mv;
    return { x, y };
  }
  return box ? anchorForBoxJS(box, a) : null;
}

function anchorForBoxJS(b, align) {
  const a = align || 2;
  let x;
  switch (a % 3) {
    case 1: x = b.x0; break;
    case 0: x = b.x1; break;
    default: x = (b.x0 + b.x1) / 2;
  }
  let y;
  if (a <= 3) y = b.y1;
  else if (a <= 6) y = (b.y0 + b.y1) / 2;
  else y = b.y0;
  return { x, y };
}

function boxFromAnchorJS(p, w, h, align) {
  const a = align || 2;
  let x0;
  switch (a % 3) {
    case 1: x0 = p.x; break;
    case 0: x0 = p.x - w; break;
    default: x0 = p.x - w / 2;
  }
  let y0;
  if (a <= 3) y0 = p.y - h;
  else if (a <= 6) y0 = p.y - h / 2;
  else y0 = p.y;
  return { x0, y0, x1: x0 + w, y1: y0 + h };
}

function moveTagFromRaw(raw) {
  if (!raw) return null;
  const m = raw.match(/\\move\(\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)(?:\s*,\s*(-?[\d.]+)\s*,\s*(-?[\d.]+))?\s*\)/);
  if (!m) return null;
  return {
    x1: Number(m[1]), y1: Number(m[2]), x2: Number(m[3]), y2: Number(m[4]),
    t1: m[5] != null ? Number(m[5]) : 0,
    t2: m[6] != null ? Number(m[6]) : null,
  };
}

function movePointAt(mv, relSec, durSec) {
  const t1 = mv.t1 || 0;
  const t2 = mv.t2 == null ? durSec * 1000 : mv.t2;
  if (t2 <= t1) return { x: mv.x2, y: mv.y2 };
  const f = Math.max(0, Math.min(1, (relSec * 1000 - t1) / (t2 - t1)));
  return {
    x: mv.x1 + (mv.x2 - mv.x1) * f,
    y: mv.y1 + (mv.y2 - mv.y1) * f,
  };
}

function movingBoxAt(ev, time, exactBBox, fromTime) {
  const mv = moveTagFromRaw(ev.raw);
  if (!mv || !exactBBox || !Number.isFinite(fromTime)) return null;
  const dur = Math.max(0, ev.end - ev.start);
  const pe = movePointAt(mv, fromTime - ev.start, dur);
  const pn = movePointAt(mv, time - ev.start, dur);
  const dx = pn.x - pe.x;
  const dy = pn.y - pe.y;
  return {
    x0: exactBBox.x0 + dx, y0: exactBBox.y0 + dy,
    x1: exactBBox.x1 + dx, y1: exactBBox.y1 + dy,
  };
}

function eventBaseBox(ev, time, visual) {
  // Interactive boxes use the exact ink box when it is valid, so the drag ghost
  // hugs the actual glyphs. Anchor math (visual=false) keeps the estimated box,
  // which is what the placement engine used to derive candidate anchors.
  if (visual) {
    const exact = eventExactBox(ev, time);
    if (exact) return exact;
  }
  return ev.bbox || ev.exactBBox || null;
}

// Current on-screen box for an event in the preview, including its chosen fix
// and any manual override. Used for hit-testing and the drag ghost.
function eventCurrentGeometry(w, eventId, ev, time, visual = true) {
  const base = eventBaseBox(ev, time, visual);
  if (!base) return null;
  const chosenId = (state.chosen[w.id] || {})[eventId];
  const s = Number.isInteger(chosenId) ? (w.suggestions || [])[chosenId] : null;
  if (!s) return { box: base };
  const key = editKey(w, eventId);
  const stored = state.edits[key];
  if (s.kind === 'top' || s.kind === 'raise') {
    const edit = stored && stored.suggestionId === chosenId ? stored : null;
    if (!edit) return { box: base };
    if (ev.moving) {
      const dx = Number(edit.dx) || 0;
      const dy = Number(edit.dy) || 0;
      return { box: { x0: base.x0 + dx, y0: base.y0 + dy, x1: base.x1 + dx, y1: base.y1 + dy } };
    }
    const origAnchor = eventAnchorJS(ev, base, previewPlayRes());
    const at = edit.posTouched && edit.at
      ? { x: Number(edit.at.x), y: Number(edit.at.y) }
      : { x: origAnchor.x + (Number(edit.dx) || 0), y: origAnchor.y + (Number(edit.dy) || 0) };
    return { box: translatedBoxJS(base, origAnchor, at, 1) };
  }
  if (s.kind !== 'nudge' && s.kind !== 'shrink') {
    return { box: base };
  }
  const edit = stored && stored.suggestionId === chosenId ? stored : editBase(chosenId, s);
  const eff = effectiveEdit(edit, s);
  let scale = 1;
  if (s.kind === 'shrink') scale = Math.max(0.05, (Number(edit.fontScale) || 100) / 100);
  if (s.at) {
    const origAnchor = eventAnchorJS(ev, base, previewPlayRes());
    const target = eff.at || origAnchor;
    return { box: translatedBoxJS(base, origAnchor, target, scale) };
  }
  return {
    box: {
      x0: base.x0 + eff.dx, y0: base.y0 + eff.dy,
      x1: base.x1 + eff.dx, y1: base.y1 + eff.dy,
    },
  };
}

// Move a visual box by the same delta as its layout anchor. Using the anchor
// delta (instead of rebuilding the box from the alignment point) keeps a
// rotated or skewed AABB from jumping: the AABB anchor is not the layout anchor.
function translatedBoxJS(base, origAnchor, target, scale = 1) {
  const dx = target.x - origAnchor.x;
  const dy = target.y - origAnchor.y;
  return {
    x0: origAnchor.x + (base.x0 - origAnchor.x) * scale + dx,
    y0: origAnchor.y + (base.y0 - origAnchor.y) * scale + dy,
    x1: origAnchor.x + (base.x1 - origAnchor.x) * scale + dx,
    y1: origAnchor.y + (base.y1 - origAnchor.y) * scale + dy,
  };
}

function eventWindowIndex() {
  const map = new Map();
  for (const win of (state.review?.windows || [])) {
    for (const id of windowEventIds(win)) {
      if (!map.has(id)) map.set(id, []);
      map.get(id).push(win);
    }
  }
  return map;
}

function hitTestPreviewEvent(w, nx, ny, time, screenW, screenH, liveById) {
  if (!Number.isFinite(nx) || !Number.isFinite(ny) || !Number.isFinite(time)) return null;
  const play = previewPlayRes();
  const x = nx * play.w;
  const y = ny * play.h;
  const owners = eventWindowIndex();
  const pxX = screenW > 0 && play.w > 0 ? play.w / screenW : 1;
  const pxY = screenH > 0 && play.h > 0 ? play.h / screenH : 1;
  const grabPx = 8;
  let best = null;
  let nearest = null;
  const candidates = [];
  for (const ev of (state.review?.events || [])) {
    const id = ev.id;
    if (time < ev.start - 0.1 || time > ev.end + 0.1) continue;
    // Prefer the window under review; otherwise use the first window that owns
    // the event so a click can still select a visible subtitle from a neighbour.
    const wins = owners.get(id) || [];
    if (!wins.length) continue;
    const hitWin = wins.find(x => x.id === w.id) || wins[0];
    const chosenId = Number.isInteger((state.chosen[hitWin.id] || {})[id])
      ? (state.chosen[hitWin.id] || {})[id]
      : null;
    const chosenS = chosenId != null ? (hitWin.suggestions || [])[chosenId] : null;
    if (chosenS && chosenS.kind === 'hide') continue;
    let layer = Number(ev.layer) || 0;
    if (chosenS && chosenS.kind === 'top') layer = 1000000;
    else if (chosenS && chosenS.kind === 'raise') layer += 1;
    const live = liveById ? liveById.get(id) : null;
    if (liveById && !live) continue;
    const solo = eventCurrentGeometry(hitWin, id, ev, time);
    if (!live && !solo) continue;

    const contains = (b) => b && x >= b.x0 && x <= b.x1 && y >= b.y0 && y <= b.y1;
    const distToBox = (b) => {
      const dx = x < b.x0 ? b.x0 - x : x > b.x1 ? x - b.x1 : 0;
      const dy = y < b.y0 ? b.y0 - y : y > b.y1 ? y - b.y1 : 0;
      return Math.hypot(dx, dy);
    };
    let rep = null;
    let distPx = 0;
    if (live && contains(live.box)) {
      // Live box is exact for this frame. If the click is in an empty corner,
      // do not fall back to a stale solo box and select the wrong event.
      if (!coverageHit(live.coverage, live.box, x, y)) continue;
      rep = { box: live.box, source: 'live' };
    } else if (solo && contains(solo.box) && coverageHit(eventCoverage(ev, time), solo.box, x, y) &&
        (!live || distToBox(live.box) <= Math.max(80, (live.box.y1 - live.box.y0) * 2))) {
      // The live collision stack may have moved this event away from where its
      // own text is drawn. The solo box is the fallback for that case, but only
      // when the live box is still nearby (not an old position after a fix).
      rep = { box: solo.box, source: eventBoxSource(ev, time) };
    } else {
      const b = (live && live.box) || (solo && solo.box);
      if (!b) continue;
      const dx = x < b.x0 ? b.x0 - x : x > b.x1 ? x - b.x1 : 0;
      const dy = y < b.y0 ? b.y0 - y : y > b.y1 ? y - b.y1 : 0;
      distPx = Math.hypot(dx / pxX, dy / pxY);
      if (distPx > grabPx) continue;
      rep = { box: b, source: live ? 'live' : eventBoxSource(ev, time) };
    }

    const b = rep.box;
    const area = Math.max(0, b.x1 - b.x0) * Math.max(0, b.y1 - b.y0);
    const inWin = hitWin.id === w.id;
    const cand = {
      eventId: id, windowId: hitWin.id, layer, inWin, box: b, area,
      distPx: Math.round(distPx * 10) / 10, ink: true,
      source: rep.source,
      transform: transformLabel(ev) || undefined,
      moving: !!ev.moving,
      effect: ev.effect || undefined,
      role: ev.role, align: ev.align,
      text: ev.text || undefined,
    };
    if (candidates.length < 40) candidates.push(cand);
    // Layer remains the z-order. Within a layer, prefer the compact box: when
    // libass stacks two signs differently in mpv and ffmpeg, the smaller box is
    // the likely label under the pointer.
    const winsTie = (cur) => layer !== cur.layer ? layer > cur.layer
      : area !== cur.area ? area < cur.area
      : id > cur.eventId;
    if (distPx === 0) {
      if (!best || winsTie(best)) best = { ...cand };
    } else if (!nearest || distPx < nearest.distPx ||
        (distPx === nearest.distPx && winsTie(nearest))) {
      nearest = { ...cand };
    }
  }
  const hit = best || nearest;
  return hit ? { ...hit, candidates } : { miss: true, candidates };
}

function previewDragEventIds(w, hitId) {
  const sel = Object.keys(state.groupSel[w.id] || {}).map(Number);
  if (sel.length > 1 && sel.includes(hitId)) return sel;
  return [hitId];
}

function previewDragBoxes(w, ids, time, liveById) {
  const map = reviewEvents();
  const out = [];
  for (const id of ids) {
    if (liveById) {
      const live = liveById.get(id);
      if (live) out.push(live.box);
      continue;
    }
    const ev = map.get(id);
    if (!ev) continue;
    const geom = eventCurrentGeometry(w, id, ev, time);
    if (geom && geom.box) out.push(geom.box);
  }
  return out;
}

// Snap a drag to the video frame and the 5% safe area. Other subtitle boxes
// are intentionally not used yet: a wrong snap in a dense scene is worse than
// a free drag.
function snapPreviewDrag(drag, dx, dy) {
  if (!drag.boxes || !drag.boxes.length || !(drag.playW > 0) || !(drag.playH > 0)) {
    return { dx, dy };
  }
  const u = { x0: Infinity, y0: Infinity, x1: -Infinity, y1: -Infinity };
  for (const b of drag.boxes) {
    u.x0 = Math.min(u.x0, b.x0 + dx);
    u.y0 = Math.min(u.y0, b.y0 + dy);
    u.x1 = Math.max(u.x1, b.x1 + dx);
    u.y1 = Math.max(u.y1, b.y1 + dy);
  }
  // Snap within ~6 screen pixels, not 6 PlayRes pixels: a 640x360 script on a
  // 1080p video would otherwise feel sticky (18 video px). Use each axis scale
  // separately because letterboxed windows do not map x and y identically.
  const pxPerScriptX = drag.screenW > 0 && drag.playW > 0 ? drag.screenW / drag.playW : 1;
  const pxPerScriptY = drag.screenH > 0 && drag.playH > 0 ? drag.screenH / drag.playH : 1;
  const thresholdX = 6 / (pxPerScriptX || 1);
  const thresholdY = 6 / (pxPerScriptY || 1);
  const safeX0 = drag.playW * 0.05;
  const safeX1 = drag.playW * 0.95;
  const safeY0 = drag.playH * 0.05;
  const safeY1 = drag.playH * 0.95;
  const xTargets = [0, safeX0, safeX1, drag.playW];
  const yTargets = [0, safeY0, safeY1, drag.playH];
  // A single dragged event can also align to the edges of other visible
  // events. Group drags keep their relative layout and only use frame/safe.
  if (drag.eventIds && drag.eventIds.length === 1 && state.review) {
    const w = (state.review.windows || []).find(x => x.id === drag.windowId);
    const map = reviewEvents();
    if (w) {
      for (const id of windowEventIds(w)) {
        if (id === drag.eventIds[0]) continue;
        const ev = map.get(id);
        if (!ev) continue;
        if (drag.time < ev.start - 0.1 || drag.time > ev.end + 0.1) continue;
        const geom = drag.liveById && drag.liveById.get(id)
          ? { box: drag.liveById.get(id).box }
          : eventCurrentGeometry(w, id, ev, drag.time);
        if (!geom) continue;
        xTargets.push(geom.box.x0, geom.box.x1);
        yTargets.push(geom.box.y0, geom.box.y1);
      }
    }
  }
  const bestShift = (edges, targets, threshold) => {
    let best = 0;
    let bestAbs = Infinity;
    for (const e of edges) {
      for (const t of targets) {
        const d = t - e;
        if (Math.abs(d) <= threshold && Math.abs(d) < bestAbs) {
          best = d;
          bestAbs = Math.abs(d);
        }
      }
    }
    return best;
  };
  dx += bestShift([u.x0, u.x1], xTargets, thresholdX);
  dy += bestShift([u.y0, u.y1], yTargets, thresholdY);
  return { dx: Math.round(dx * 10) / 10, dy: Math.round(dy * 10) / 10 };
}

const BASE_ANCHOR_IDS = [
  'above', 'above-right', 'right', 'below-right',
  'below', 'below-left', 'left', 'above-left', 'center',
];

function baseKey(w, eventId) { return `${w.id}:${eventId}`; }

function computeBaseAnchors(quad, playH) {
  const [tl, tr, br, bl] = quad;
  const center = {
    x: (tl.x + tr.x + br.x + bl.x) / 4,
    y: (tl.y + tr.y + br.y + bl.y) / 4,
  };
  const mid = (a, b) => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 });
  const add = (p, v, k) => ({ x: p.x + v.x * k, y: p.y + v.y * k });
  const outward = (p) => {
    const dx = p.x - center.x;
    const dy = p.y - center.y;
    const len = Math.hypot(dx, dy) || 1;
    return { x: dx / len, y: dy / len };
  };
  const gap = Math.max(4, playH * 0.022);
  const top = mid(tl, tr);
  const bottom = mid(bl, br);
  const left = mid(tl, bl);
  const right = mid(tr, br);
  const points = {
    above: add(top, outward(top), gap),
    'above-right': add(tr, outward(tr), gap * 1.2),
    right: add(right, outward(right), gap),
    'below-right': add(br, outward(br), gap * 1.2),
    below: add(bottom, outward(bottom), gap),
    'below-left': add(bl, outward(bl), gap * 1.2),
    left: add(left, outward(left), gap),
    'above-left': add(tl, outward(tl), gap * 1.2),
    center,
  };
  return BASE_ANCHOR_IDS.map((id) => ({ id, x: points[id].x, y: points[id].y }));
}

function refreshBaseTools(w) {
  const host = document.getElementById('base-tools');
  if (!host || !w) return;
  const sel = state.selected;
  let b = null;
  if (sel && sel.windowId === w.id) b = state.bases[baseKey(w, sel.eventId)];
  if (!b && state.pendingBase) {
    host.classList.remove('hidden');
    host.innerHTML = '<span class="base-tools-title">Base pending</span>' +
      '<span class="sug-sub">Click a subtitle in the preview to attach it.</span>';
    return;
  }
  if (!b || !sel) {
    host.classList.add('hidden');
    host.innerHTML = '';
    return;
  }
  const anchors = computeBaseAnchors(b.quad, previewPlayRes().h);
  const glyphs = ['↑', '↗', '→', '↘', '↓', '↙', '←', '↖', '●'];
  const names = ['above', 'above-right', 'right', 'below-right', 'below', 'below-left', 'left', 'above-left', 'centre'];
  host.classList.remove('hidden');
  host.innerHTML = `<span class="base-tools-title">Place #${sel.eventId}</span>` +
    anchors.map((a, i) =>
      `<button type="button" class="base-anchor" data-i="${i}" title="${names[i]}">${glyphs[i]}</button>`).join('') +
    '<button type="button" class="small minor base-tools-clear">Clear base</button>';
  host.querySelectorAll('.base-anchor').forEach((btn) => {
    btn.addEventListener('click', () => {
      const index = Number(btn.dataset.i);
      if (anchors[index]) placeAtBaseAnchor(w, sel.eventId, anchors[index]);
    });
  });
  host.querySelector('.base-tools-clear').addEventListener('click', () => {
    delete state.bases[baseKey(w, sel.eventId)];
    state.pendingBase = null;
    syncBaseToPreview();
    refreshBaseTools(w);
    setStatus('base cleared');
  });
}

function currentPreviewTime(w) {
  if (state.playbackPos && Number.isFinite(state.playbackPos.time)) return state.playbackPos.time;
  return w.peak;
}

function syncBaseToPreview() {
  const w = state.reviewWindow;
  const sel = state.selected;
  const hasBase = !!(w && sel && sel.windowId === w.id && state.bases[baseKey(w, sel.eventId)]);
  const btn = windowSuggestions.querySelector('.base-clear');
  if (btn) btn.disabled = !(hasBase || state.pendingBase);
  if (!S.setBase) return;
  const play = previewPlayRes();
  if (!hasBase && !state.pendingBase) {
    // Still send the PlayRes so the Lua base editor knows the coordinate space
    // before the first base exists.
    S.setBase({ playW: play.w, playH: play.h, quad: [], anchors: [] });
    return;
  }
  const b = hasBase ? state.bases[baseKey(w, sel.eventId)] : state.pendingBase;
  b.anchors = computeBaseAnchors(b.quad, play.h);
  S.setBase({ playW: play.w, playH: play.h, quad: b.quad, anchors: b.anchors });
}

function placeAtBaseAnchor(w, eventId, anchor) {
  const ev = reviewEvents().get(eventId);
  if (!ev) return;
  const b = state.bases[baseKey(w, eventId)];
  const time = b && Number.isFinite(b.time) ? b.time : currentPreviewTime(w);
  const geom = eventCurrentGeometry(w, eventId, ev, time);
  if (!geom) return;
  const cw = geom.box.x1 - geom.box.x0;
  const ch = geom.box.y1 - geom.box.y0;
  const a = anchor;
  let target;
  switch (a.id) {
    case 'above':
      target = { x0: a.x - cw / 2, y0: a.y - ch, x1: a.x + cw / 2, y1: a.y };
      break;
    case 'below':
      target = { x0: a.x - cw / 2, y0: a.y, x1: a.x + cw / 2, y1: a.y + ch };
      break;
    case 'left':
      target = { x0: a.x - cw, y0: a.y - ch / 2, x1: a.x, y1: a.y + ch / 2 };
      break;
    case 'right':
      target = { x0: a.x, y0: a.y - ch / 2, x1: a.x + cw, y1: a.y + ch / 2 };
      break;
    case 'above-left':
      target = { x0: a.x - cw, y0: a.y - ch, x1: a.x, y1: a.y };
      break;
    case 'above-right':
      target = { x0: a.x, y0: a.y - ch, x1: a.x + cw, y1: a.y };
      break;
    case 'below-left':
      target = { x0: a.x - cw, y0: a.y, x1: a.x, y1: a.y + ch };
      break;
    case 'below-right':
      target = { x0: a.x, y0: a.y, x1: a.x + cw, y1: a.y + ch };
      break;
    default: // center
      target = { x0: a.x - cw / 2, y0: a.y - ch / 2, x1: a.x + cw / 2, y1: a.y + ch / 2 };
  }
  const dx = target.x0 - geom.box.x0;
  const dy = target.y0 - geom.box.y0;
  moveEventsBy(w, [eventId], dx, dy, { source: 'anchor' });
  setStatus(`placed #${eventId} ${a.id} the base`);
}

function handlePreviewBase(p) {
  if (!p || !state.reviewWindow) return;
  const w = state.reviewWindow;
  if (p.kind === 'quad' || p.kind === 'anchor') {
    if (state.playingWindow === w.id) {
      state.playingWindow = null;
      state.pausedWindow = w.id;
      updatePlaybackButtons();
    }
  }
  if (p.kind === 'quad') {
    if (!p.quad || p.quad.length !== 4) return;
    const sel = state.selected;
    if (!sel || sel.windowId !== w.id) {
      state.pendingBase = { quad: p.quad, time: currentPreviewTime(w) };
      syncBaseToPreview();
      refreshBaseTools(w);
      setStatus('base saved; click a subtitle in the preview to attach it');
      return;
    }
    const key = baseKey(w, sel.eventId);
    state.pendingBase = null;
    state.bases[key] = { quad: p.quad, time: currentPreviewTime(w) };
    syncBaseToPreview();
    refreshBaseTools(w);
    setStatus(`base set for #${sel.eventId}; click an anchor (in mpv or below) to place it`);
  } else if (p.kind === 'anchor') {
    const sel = state.selected;
    if (!sel || sel.windowId !== w.id) return;
    const b = state.bases[baseKey(w, sel.eventId)];
    if (!b) return;
    const anchors = computeBaseAnchors(b.quad, previewPlayRes().h);
    const a = anchors[p.index];
    if (a) placeAtBaseAnchor(w, sel.eventId, a);
  } else if (p.kind === 'clear') {
    const sel = state.selected;
    if (sel && sel.windowId === w.id) {
      delete state.bases[baseKey(w, sel.eventId)];
    }
    state.pendingBase = null;
    syncBaseToPreview();
    refreshBaseTools(w);
    setStatus('base cleared');
  }
}

function highlightPreviewSelection(w) {
  const sel = state.selected;
  for (const el of windowSuggestions.querySelectorAll('.event-fixes[data-event]')) {
    const id = Number(el.dataset.event);
    el.classList.toggle('selected', !!(sel && sel.windowId === w.id && sel.eventId === id));
  }
}

let previewDragQueue = Promise.resolve();

// Drag messages must stay ordered. The down handler waits for fresh boxes from
// the CLI; queue any up/cancel that arrives while that request is in flight.
function handlePreviewDrag(p) {
  previewDragQueue = previewDragQueue
    .then(() => handlePreviewDragNow(p))
    .catch((e) => uiLog('error', 'preview drag handler failed', { error: e.message }));
  return previewDragQueue;
}

async function handlePreviewDragNow(p) {
  if (!p || !state.reviewWindow || !state.merged) return;
  let w = state.reviewWindow;
  const play = previewPlayRes();
  if (p.phase === 'down') {
    // mpv pauses on mousedown so the frame and hit-test geometry stay fixed.
    const wasPlaying = state.playingWindow === w.id;
    state.playingWindow = null;
    state.pausedWindow = w.id;
    updatePlaybackButtons();
    const t = Number.isFinite(p.time)
      ? p.time
      : (state.playbackPos && Number.isFinite(state.playbackPos.time) ? state.playbackPos.time : w.peak);
    state.lastPreviewTime = t;
    // Immediate feedback from the stored boxes while the live libass render
    // runs. The final drag still uses the live boxes (the mouse-up is queued).
    const provisional = hitTestPreviewEvent(w, p.nx, p.ny, t, p.screenW, p.screenH, null);
    const provisionalHit = provisional && !provisional.miss ? provisional : null;
    if (provisionalHit) {
      const pids = previewDragEventIds(w, provisionalHit.eventId);
      const pboxes = previewDragBoxes(w, pids, t);
      state.selected = { windowId: w.id, eventId: provisionalHit.eventId };
      state.drag = {
        windowId: w.id, eventIds: pids, hitEvent: provisionalHit.eventId, mode: p.mode, time: t,
        boxes: pboxes, playW: play.w, playH: play.h,
        screenW: p.screenW, screenH: p.screenH,
        hitInfo: provisionalHit, liveById: null, provisional: true,
      };
      highlightPreviewSelection(w);
      if (S.dragBoxes) S.dragBoxes({ playW: play.w, playH: play.h, boxes: pboxes });
      setStatus(`selected #${provisionalHit.eventId}; measuring the exact box…`);
      uiLog('info', 'preview provisional hit', {
        eventId: provisionalHit.eventId, box: provisionalHit.box,
        source: provisionalHit.source, text: provisionalHit.text,
      });
    }
    // Ask the CLI for the real boxes at this frame. Merge-time exact boxes are
    // rendered per event and miss libass collision stacking (and fades/effects).
    let liveById = null;
    if (S.previewBoxes) {
      try {
        const live = await S.previewBoxes({ time: t });
        if (live && live.ok && Array.isArray(live.boxes)) {
          liveById = new Map(live.boxes.map(b => [b.id, b]));
          state.diagnostics.liveBoxes = {
            time: t, count: live.boxes.length, ids: live.boxes.map(b => b.id),
          };
        }
      } catch (e) {
        uiLog('warn', 'preview boxes unavailable', { time: t, error: e.message });
      }
    }
    const hitResult = hitTestPreviewEvent(w, p.nx, p.ny, t, p.screenW, p.screenH, liveById);
    const hit = hitResult && !hitResult.miss ? hitResult : null;
    const candidates = hitResult ? hitResult.candidates : [];
    const diag = {
      time: Math.round(t * 1000) / 1000,
      nx: p.nx, ny: p.ny,
      scriptX: Number.isFinite(p.nx) ? Math.round(p.nx * play.w * 10) / 10 : null,
      scriptY: Number.isFinite(p.ny) ? Math.round(p.ny * play.h * 10) / 10 : null,
      playW: play.w, playH: play.h,
      screenW: p.screenW, screenH: p.screenH,
      osdW: p.osdW, osdH: p.osdH,
      marginL: p.marginL, marginT: p.marginT,
      mouseX: p.mouseX, mouseY: p.mouseY,
      par: p.par,
      windowId: w.id,
      hit: hit ? {
        eventId: hit.eventId, windowId: hit.windowId, inWindow: hit.inWin,
        box: hit.box, distPx: hit.distPx, source: hit.source,
        transform: hit.transform, role: hit.role, align: hit.align,
        layer: hit.layer, moving: hit.moving, text: hit.text,
        effect: hit.effect,
      } : null,
      candidates: candidates,
    };
    state.diagnostics.lastHit = diag;
    uiLog('info', 'preview hit-test', diag);
    if (hit && hit.windowId !== w.id) {
      // The click landed on a subtitle that belongs to another review window.
      // Switch the panel so the fix list matches the selection.
      selectWindow(hit.windowId, false, false);
      w = state.reviewWindow;
      if (w) state.pausedWindow = w.id;
      updatePlaybackButtons();
      uiLog('info', 'preview hit switched review window', {
        from: diag.windowId, to: w && w.id, eventId: hit.eventId,
      });
    }
    if (!hit) {
      state.selected = null;
      state.drag = null;
      highlightPreviewSelection(w);
      syncBaseToPreview();
      refreshBaseTools(w);
      if (S.dragBoxes) S.dragBoxes({ playW: play.w, playH: play.h, boxes: [] });
      // Don't let a click on empty video stop an otherwise-playing window.
      if (wasPlaying) {
        S.previewPlayback({ action: 'resume', start: w.start, end: w.end, ...playbackSettings() })
          .then((res) => {
            if (res && res.ok) {
              state.playingWindow = w.id;
              state.pausedWindow = null;
              updatePlaybackButtons();
            }
          })
          .catch(() => {});
      }
      return;
    }
    const ids = previewDragEventIds(w, hit.eventId);
    const boxes = previewDragBoxes(w, ids, t, liveById);
    state.drag = {
      windowId: w.id, eventIds: ids, hitEvent: hit.eventId, mode: p.mode, time: t,
      boxes, playW: play.w, playH: play.h,
      screenW: p.screenW, screenH: p.screenH,
      hitInfo: hit, liveById,
    };
    state.selected = { windowId: w.id, eventId: hit.eventId };
    uiLog('info', 'preview selected event', {
      eventId: hit.eventId, windowId: w.id, mode: p.mode, time: t,
      box: hit.box, boxes: boxes.length, source: hit.source,
      transform: hit.transform, role: hit.role, align: hit.align,
      distancePx: hit.distPx, inWindow: hit.inWin,
      layer: hit.layer, moving: hit.moving, text: hit.text,
      effect: hit.effect,
    });
    if (state.pendingBase) {
      state.bases[baseKey(w, hit.eventId)] = state.pendingBase;
      state.pendingBase = null;
    }
    highlightPreviewSelection(w);
    syncBaseToPreview();
    refreshBaseTools(w);
    const el = windowSuggestions.querySelector(`.event-fixes[data-event="${hit.eventId}"]`);
    if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest' });
    if (S.dragBoxes) S.dragBoxes({ playW: play.w, playH: play.h, boxes });
    const tf = hit.transform ? `, transform ${hit.transform}` : '';
    const src = ['exact', 'samples', 'live'].includes(hit.source)
      ? '' : ', estimated box';
    setStatus(`selected #${hit.eventId}${tf}${src}; drag to move` +
      (p.mode === 'axis' ? ', axis locked' : p.mode === 'fine' ? ', fine' : ''));
  } else if (p.phase === 'up') {
    const drag = state.drag;
    state.drag = null;
    if (!drag || drag.windowId !== w.id) return;
    let dx = Number(p.dx) || 0;
    let dy = Number(p.dy) || 0;
    const raw = { dx, dy };
    if (Math.abs(dx) < 0.2 && Math.abs(dy) < 0.2) {
      uiLog('info', 'preview drag ended below threshold', { ...raw, eventIds: drag.eventIds });
      setStatus(`selected #${drag.hitEvent}; drag to move, arrows to nudge`);
      return;
    }
    ({ dx, dy } = snapPreviewDrag(drag, dx, dy));
    const moved = moveEventsBy(w, drag.eventIds, dx, dy, {
      source: 'drag', liveBoxes: drag.liveById,
    });
    state.diagnostics.lastDrag = {
      windowId: w.id, eventIds: drag.eventIds, hitEvent: drag.hitEvent,
      rawDx: raw.dx, rawDy: raw.dy, dx, dy, moved,
      playW: drag.playW, playH: drag.playH,
      screenW: drag.screenW, screenH: drag.screenH,
      source: drag.hitInfo && drag.hitInfo.source,
      transform: drag.hitInfo && drag.hitInfo.transform,
    };
    uiLog('info', 'preview drag applied', state.diagnostics.lastDrag);
  } else if (p.phase === 'cancel') {
    state.drag = null;
    if (S.cancelDrag) S.cancelDrag();
  } else if (p.phase === 'nudge') {
    if (!state.selected || state.selected.windowId !== w.id) return;
    const dx = Number(p.dx) || 0;
    const dy = Number(p.dy) || 0;
    if (!dx && !dy) return;
    moveEventsBy(w, [state.selected.eventId], dx, dy, { source: 'nudge' });
  } else if (p.phase === 'reset') {
    if (!state.selected || state.selected.windowId !== w.id) return;
    const eventId = state.selected.eventId;
    if (state.chosen[w.id]) {
      delete state.chosen[w.id][eventId];
      if (!Object.keys(state.chosen[w.id]).length) delete state.chosen[w.id];
    }
    delete state.edits[editKey(w, eventId)];
    const scroll = windowDetail.scrollTop;
    renderWindowDetail(w);
    windowDetail.scrollTop = scroll;
    state.windowScroll[w.id] = scroll;
    applyPreviewTo(state.pausedWindow === w.id ? null : eventSeek(w, eventId));
    setStatus(`reset #${eventId} to original`);
  }
}

function updateGroupMoveBar(w) {
  const bar = windowSuggestions.querySelector('.group-move-bar');
  if (!bar) return;
  const n = Object.keys(state.groupSel[w.id] || {}).length;
  bar.querySelector('.group-count').textContent = n
    ? `${n} event${n === 1 ? '' : 's'} selected`
    : 'tick events below';
  bar.querySelector('.group-apply').disabled = n === 0;
  bar.querySelector('.group-clear').disabled = n === 0;
}

function fixWarningText(a) {
  const bits = [];
  if (a.outOfFrame) {
    bits.push('fully outside the video frame');
  } else if ((a.frameOutside || 0) > 0.0001) {
    bits.push(`${Math.round(a.frameOutside * 100)}% outside the video frame`);
  }
  if (a.introducedSafeArea) {
    bits.push(`${Math.round((a.safeOutside || 0) * 100)}% outside the 5% safe area`);
  }
  return bits.join(' · ');
}

function fixWarningDiv(a) {
  const d = document.createElement('div');
  d.className = 'fix-warning-inline';
  d.textContent = '⚠ ' + fixWarningText(a);
  return d;
}

function windowEventIds(w) {
  const ids = new Set(w.events || []);
  for (const g of (w.byEvent || [])) ids.add(g.event);
  return ids;
}

function recheckForWindow(w) {
  const ids = windowEventIds(w);
  return (state.recheck || []).filter(c => (c.events || []).some(id => ids.has(id)));
}

function refreshRecheckNotice(w) {
  const el = sel('#window-recheck');
  if (!el) return;
  const conf = recheckForWindow(w);
  if (!conf.length) {
    el.classList.add('hidden');
    el.innerHTML = '';
    return;
  }
  const items = conf.slice(0, 8).map(c =>
    `<li>${escapeHtml(fmtClock(c.start))}–${escapeHtml(fmtClock(c.end))}: ${escapeHtml(c.reason || c.kind || '')}</li>`).join('');
  el.classList.remove('hidden');
  el.innerHTML = `<strong>Re-check (estimated):</strong> ${conf.length} conflict(s) still involve changed events in this window. Intentional overlaps can be accepted.` +
    `<details><summary>show</summary><ul>${items}</ul></details>`;
}

function refreshAppliedIndicators(w) {
  refreshRecheckNotice(w);
  for (const wrap of windowSuggestions.querySelectorAll('.event-fixes[data-event]')) {
    const eventId = Number(wrap.dataset.event);
    const warn = state.fixWarnings[`${w.id}:${eventId}`];
    let el = wrap.querySelector('.fix-warning-inline');
    if (warn) {
      if (!el) {
        el = fixWarningDiv(warn);
        wrap.appendChild(el);
      } else {
        el.textContent = '⚠ ' + fixWarningText(warn);
      }
    } else if (el) {
      el.remove();
    }
  }
}

function updateAppliedIndicators(res) {
  const warnings = {};
  for (const a of (res.applied || [])) {
    if (a.outOfFrame || a.introducedFrame || (a.frameOutside || 0) > 0.0001 || a.introducedSafeArea) {
      warnings[`${a.windowId}:${a.event}`] = a;
    }
  }
  state.fixWarnings = warnings;
  state.recheck = Array.isArray(res.remainingConflicts) ? res.remainingConflicts : [];
}

function nextFilteredWindow(id) {
  const list = filteredWindows();
  if (!list.length) return null;
  const idx = list.findIndex(w => w.id === id);
  if (idx === -1) return list[0].id;
  for (let k = 1; k < list.length; k++) {
    const w = list[(idx + k) % list.length];
    if (windowStatus(w) !== 'done') return w.id;
  }
  return null;
}

btnDone.addEventListener('click', () => {
  const w = state.reviewWindow;
  if (!w) return;
  const marking = windowStatus(w) !== 'done';
  if (marking) state.statuses[w.id] = 'done';
  else delete state.statuses[w.id];
  renderReviewSummary();
  const next = marking ? nextFilteredWindow(w.id) : null;
  filterAndRenderList();
  if (next != null) selectWindow(next, true, true);
  setStatus(marking ? `window ${w.id} marked done` : `window ${w.id} reopened`);
});

btnAgain.addEventListener('click', () => {
  const w = state.reviewWindow;
  if (!w) return;
  state.statuses[w.id] = 'again';
  renderReviewSummary();
  filterAndRenderList();
  setStatus(`window ${w.id} marked for another check`);
});

btnPlayWindow.addEventListener('click', playWindow);
btnStopWindow.addEventListener('click', stopWindow);
btnPauseWindow.addEventListener('click', pauseWindow);
optLoop.addEventListener('change', updatePlaybackSettings);
optUnmute.addEventListener('change', updatePlaybackSettings);
optSpeed.addEventListener('change', updatePlaybackSettings);

filterSurface.addEventListener('change', filterAndRenderList);
filterCause.addEventListener('change', filterAndRenderList);
filterStatus.addEventListener('change', filterAndRenderList);

reviewNote.addEventListener('input', () => {
  if (state.reviewWindow) state.notes[state.reviewWindow.id] = reviewNote.value;
});

btnPreviewWindow.addEventListener('click', async () => {
  const w = state.reviewWindow;
  if (!w) return;
  btnPreviewWindow.disabled = true;
  try {
    await applyPreviewTo(w.peak, true);
  } finally {
    btnPreviewWindow.disabled = false;
  }
});

function basePayload() {
  return {
    kind: 'subsandwicher.review-context',
    version: 1,
    createdAt: new Date().toISOString(),
    comment: reviewNote.value.trim(),
    app: state.appMeta,
    video: state.videoPath,
    chosen: chosenFixes(),
    comments: { ...state.notes },
    statuses: { ...state.statuses },
    bases: state.bases,
    layerEdits: state.layerEdits,
    pendingBase: state.pendingBase,
    recheck: state.recheck,
    fixWarnings: state.fixWarnings,
    paths: {
      mergedAss: state.merged && state.merged.out,
      report: state.merged && state.merged.report_path,
      log: state.logPath,
    },
    job: state.review && state.review.job,
    summary: state.review && state.review.summary,
    tracks: (state.review && state.review.tracks) || null,
    diagnostics: state.diagnostics,
  };
}

function windowPayload() {
  const w = state.reviewWindow;
  if (!w) return null;
  const ids = new Set(w.events || []);
  for (const g of (w.byEvent || [])) ids.add(g.event);
  const conflictIds = new Set(w.conflicts || []);
  const chosen = chosenFixes().filter(f => f.windowId === w.id);
  const chosenSuggestions = chosen.map(f => (w.suggestions || [])[f.suggestionIndex]).filter(Boolean);
  return {
    ...basePayload(),
    windowIndex: (state.review.windows || []).indexOf(w) + 1,
    windowCount: (state.review.windows || []).length,
    window: w,
    status: windowStatus(w),
    playback: playbackSettings(),
    playing: state.playingWindow === w.id,
    suggestions: chosenSuggestions,
    suggestion: chosenSuggestions[0] || null,
    conflicts: (state.review.conflicts || []).filter(c => conflictIds.has(c.id)),
    events: (state.review.events || []).filter(e => ids.has(e.id)),
  };
}

function allPayload() {
  return {
    ...basePayload(),
    notes: { ...state.notes },
    windows: state.review.windows || [],
    conflicts: state.review.conflicts || [],
    events: state.review.events || [],
  };
}

async function copyPayload(payload, label) {
  if (!payload) { setStatus('nothing to copy', true); return; }
  const text = JSON.stringify(payload, null, 2);
  try {
    if (S.copyText) await S.copyText(text);
    else await navigator.clipboard.writeText(text);
    setStatus(`copied ${label} (${Math.round(text.length / 1024)} KB) — paste it with your comment.`);
  } catch (e) {
    setStatus('copy failed: ' + e.message, true);
  }
}

btnCopyWindow.addEventListener('click', () => copyPayload(windowPayload(), 'window state'));
btnCopyAll.addEventListener('click', () => {
  if (!state.review) { setStatus('no review data to copy', true); return; }
  copyPayload(allPayload(), 'all review state');
});

// Pick a time where the merged subs appear: halfway through (or 0.5s into) a
// random dialogue line, so both tracks' text is rendered in the paused frame.
function fmtTime(s) {
  const sec = Math.max(0, Math.floor(s || 0));
  return `${Math.floor(sec / 60)}:${String(sec % 60).padStart(2, '0')}`;
}

function pickEventTime(events) {
  if (!events || !events.length) return 0;
  const cands = events.filter(e => e.end > e.start && e.start > 0);
  if (!cands.length) return 0;
  const good = cands.filter(e => (e.end - e.start) >= 1.0);
  const pool = good.length ? good : cands;
  const e = pool[Math.floor(Math.random() * pool.length)];
  const dur = e.end - e.start;
  return +(e.start + Math.min(0.3, dur / 2)).toFixed(2);
}

if (S.onPlaybackEnded) {
  window.addEventListener('resize', updatePlaybackStickyOffset);

S.onPlaybackEnded(() => {
    state.playingWindow = null;
    state.pausedWindow = null;
    updatePlaybackButtons();
    const w = state.reviewWindow;
    if (w) updateWindowProgress({ time: w.end, start: w.start, end: w.end });
    setStatus('playback ended');
  });
}

if (S.onPlaybackPosition) {
  S.onPlaybackPosition((pos) => updateWindowProgress(pos));
}

if (S.onPreviewDrag) {
  S.onPreviewDrag(handlePreviewDrag);
}

if (S.onPreviewBase) {
  S.onPreviewBase(handlePreviewBase);
}

// Registered once so repeated merges do not stack listeners.
S.onMergeProgress((p) => {
  if (!state.merging) return;
  const overall = Math.max(0, Math.min(100, Math.round((p.overall || 0) * 100)));
  const message = p.message || (p.stage === 'extract'
    ? `extracting ${p.role || ''} subtitles…`
    : p.stage === 'done' ? 'review ready' : 'loading…');
  const signature = `${p.stage || ''}|${overall}|${message}`;
  progwrap.classList.remove('hidden');
  progbar.style.width = overall + '%';
  progbar.classList.toggle('busy', !!p.indeterminate);
  if (signature === state.mergeProgress) return;
  state.mergeProgress = signature;
  setStatus(message);
});

// exposed for browser-based smoke tests (harmless in production)
window.__loadVideo = loadVideo;
window.__state = state;
