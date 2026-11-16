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
  groupSel: {}, // window id -> { event id -> true } selection for group move
  selected: null, // { windowId, eventId } picked in the mpv preview
  drag: null, // active preview drag state
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
};

const sel = (s) => document.querySelector(s);
const S = window.subsandwicher;

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
  state.groupSel = {};
  state.fixWarnings = {};
  state.recheck = [];
  state.selected = null;
  state.drag = null;
  state.statuses = {};
  state.windowScroll = {};
  state.playingWindow = null;
  state.pausedWindow = null;
  state.langsCollapsed = false;
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
    state.groupSel = {};
    state.fixWarnings = {};
    state.recheck = [];
    state.selected = null;
    state.drag = null;
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
    setStatus(`${streams.length} subtitle stream(s) found — pick primary and secondary, then "Continue".`);
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

function langName(code) {
  const c = (code || '').toLowerCase();
  return LANG_LABELS[c] || (c ? c : 'no tag');
}

function streamLabel(s) {
  if (!s) return '';
  const lang = (s.tags && s.tags.language) || '';
  const title = (s.tags && s.tags.title) || 'untitled';
  const tag = s.bitmapped ? ' [image-based — not usable]' : '';
  return `#${s.index}  ${title} — ${langName(lang)}${tag}`;
}

function buildStreamLists() {
  const makeList = (selId, role) => {
    const box = document.querySelector(selId);
    box.innerHTML = '';
    const wrap = box.closest('.streamlist');
    for (const s of state.streams) {
      if (s.bitmapped) continue;
      const label = document.createElement('label');
      label.innerHTML = `<input type="radio" name="${role}" value="${s.index}"> ` +
        `<span>#${s.index} ${escapeHtml((s.tags && s.tags.title) || 'untitled')} — ${escapeHtml(langName(s.tags?.language))}</span>`;
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
  state.groupSel = {};
  state.fixWarnings = {};
  state.recheck = [];
  state.selected = null;
  state.drag = null;
  state.statuses = {};
  state.windowScroll = {};
  state.playingWindow = null;
  state.pausedWindow = null;
  state.langsCollapsed = false;
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
    state.groupSel = {};
    state.fixWarnings = {};
    state.recheck = [];
    state.selected = null;
    state.drag = null;
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
    if (res && res.ok) setStatus(`previewing window ${w.id} at ${fmtClock(w.peak)}`);
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
      const fix = { windowId: Number(windowId), suggestionIndex: suggestionId };
      const edit = state.edits[`${windowId}:${eventId}`];
      if (edit && edit.dirty && edit.suggestionId === suggestionId && edit.override) {
        fix.override = edit.override;
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
    `<div class="dec">${boxLabel}${ev.moving ? ' · moving' : ''}</div>` +
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
    'You can also click/drag a subtitle in the preview window: Shift locks an axis, Alt moves finer, arrow keys nudge, R resets.</div>';

  const groupBar = document.createElement('div');
  groupBar.className = 'group-move-bar';
  groupBar.innerHTML =
    '<span class="group-move-title">Group move</span>' +
    '<span class="group-count">tick events below</span>' +
    '<label>dx <input class="group-dx" type="number" step="1" value="0"></label>' +
    '<label>dy <input class="group-dy" type="number" step="1" value="0"></label>' +
    '<button type="button" class="small group-apply" disabled>Move selected</button>' +
    '<button type="button" class="small minor group-clear" disabled>Clear selection</button>' +
    '<span class="sug-sub">Tick one or more event headings, then move them together.</span>';
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
  windowSuggestions.appendChild(groupBar);

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
    const topId = g.ids.find(id => suggestions[id] && suggestions[id].kind === 'top');
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
    head.innerHTML =
      `<div class="fix-event-title">${escapeHtml(title)}</div>` +
      `<div class="fix-event-sub">#${g.event} ${ev ? escapeHtml(ev.role) : ''}${escapeHtml(seekTxt)} ${swatchesFor(ev)}` +
      `${topId !== undefined ? ' <span class="toolbar-spacer"></span>' : ''}</div>`;
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
    if (topId !== undefined) {
      const topBtn = document.createElement('button');
      const topChosen = chosenForWindow[g.event] === topId;
      topBtn.type = 'button';
      topBtn.className = 'small minor move-top-btn' + (topChosen ? ' active' : '');
      topBtn.textContent = topChosen ? 'On top ✓' : 'Move to top';
      topBtn.title = 'Draw this event above every other subtitle';
      topBtn.addEventListener('click', () => chooseFix(w, g.event, topChosen ? -1 : topId));
      head.querySelector('.fix-event-sub').appendChild(topBtn);
    }
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
        (s.autoApply ? '<span class="badge auto">auto</span> ' : '') +
        (s.manualReview ? '<span class="badge manual">manual</span> ' : '') +
        (s.recommended ? '<span class="badge">recommended</span> ' : '') +
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
  if (!g) return null;
  const chosenId = (state.chosen[w.id] || {})[eventId];
  if (Number.isInteger(chosenId)) {
    const s = (w.suggestions || [])[chosenId];
    if (!s) return null;
    if (s.kind !== 'nudge' && s.kind !== 'shrink') return null;
    return { id: chosenId, s };
  }
  let shrink = null;
  for (const id of g.suggestionIds || []) {
    const s = (w.suggestions || [])[id];
    if (s && s.kind === 'nudge') return { id, s };
    if (s && s.kind === 'shrink' && !shrink) shrink = { id, s };
  }
  return shrink;
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
    const key = editKey(w, eventId);
    const stored = state.edits[key];
    let edit;
    if (!hadChosen) {
      // No chosen candidate yet: move from the event's current/original
      // position, not from the candidate's own offset.
      const ev = reviewEvents().get(eventId);
      const baseBox = ev ? eventBaseBox(ev, ev.exactSeek) : null;
      if (!ev || !baseBox) { skipped++; continue; }
      if (base.s.at) {
        const origAnchor = ev.pos
          ? { x: Number(ev.pos.x), y: Number(ev.pos.y) }
          : anchorForBoxJS(baseBox, ev.align);
        edit = {
          suggestionId: base.id,
          kind: base.s.kind,
          dx: 0,
          dy: 0,
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
    edit.override = overrideForEdit(edit, base.s);
    chosen[eventId] = base.id;
    state.edits[key] = edit;
    moved++;
  }
  if (!moved) {
    setStatus(`${opts.source || 'move'}: no movable events selected`, true);
    return 0;
  }
  const scroll = windowDetail.scrollTop;
  renderWindowDetail(w);
  windowDetail.scrollTop = scroll;
  state.windowScroll[w.id] = scroll;
  applyPreviewTo(state.pausedWindow === w.id ? null : eventSeek(w, eventIds[0]));
  const verb = opts.source === 'drag' ? 'drag'
    : opts.source === 'nudge' ? 'nudge'
      : opts.source === 'group' ? 'group move' : 'move';
  setStatus(`${verb}: ${moved} event(s) by ${signed(dx)},${signed(dy)}` +
    (skipped ? `; ${skipped} skipped (non-positional fix)` : ''));
  return moved;
}

function applyGroupMove(w, dx, dy) {
  const events = Object.keys(state.groupSel[w.id] || {}).map(Number);
  moveEventsBy(w, events, dx, dy, { source: 'group' });
}

function previewPlayRes() {
  const job = (state.review && state.review.job) || {};
  return {
    w: Number(job.playResX) || 640,
    h: Number(job.playResY) || 360,
  };
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

function eventBaseBox(ev, time) {
  // Static events use the estimated box: it is the box the placement engine
  // and candidate anchors were derived from. Moving events need the exact box
  // at the paused seek because their lifetime bbox spans the whole path.
  if (!ev.moving) return ev.bbox || ev.exactBBox || null;
  if (ev.exactBBox && Number.isFinite(ev.exactSeek) && Math.abs(time - ev.exactSeek) <= 0.2) {
    return ev.exactBBox;
  }
  return ev.bbox || null;
}

// Current on-screen box for an event in the preview, including its chosen fix
// and any manual override. Used for hit-testing and the drag ghost.
function eventCurrentGeometry(w, eventId, ev, time) {
  const base = eventBaseBox(ev, time);
  if (!base) return null;
  const bw = base.x1 - base.x0;
  const bh = base.y1 - base.y0;
  const chosenId = (state.chosen[w.id] || {})[eventId];
  const s = Number.isInteger(chosenId) ? (w.suggestions || [])[chosenId] : null;
  if (!s || (s.kind !== 'nudge' && s.kind !== 'shrink')) {
    return { box: base };
  }
  const key = editKey(w, eventId);
  const stored = state.edits[key];
  const edit = stored && stored.suggestionId === chosenId ? stored : editBase(chosenId, s);
  const eff = effectiveEdit(edit, s);
  let scale = 1;
  if (s.kind === 'shrink') scale = Math.max(0.05, (Number(edit.fontScale) || 100) / 100);
  if (s.at) {
    const origAnchor = ev.pos
      ? { x: Number(ev.pos.x), y: Number(ev.pos.y) }
      : anchorForBoxJS(base, ev.align);
    const target = eff.at || origAnchor;
    return { box: boxFromAnchorJS(target, bw * scale, bh * scale, ev.align) };
  }
  return {
    box: {
      x0: base.x0 + eff.dx, y0: base.y0 + eff.dy,
      x1: base.x1 + eff.dx, y1: base.y1 + eff.dy,
    },
  };
}

function hitTestPreviewEvent(w, nx, ny, time) {
  if (!Number.isFinite(nx) || !Number.isFinite(ny) || !Number.isFinite(time)) return null;
  const play = previewPlayRes();
  const x = nx * play.w;
  const y = ny * play.h;
  const map = reviewEvents();
  let best = null;
  for (const id of windowEventIds(w)) {
    const ev = map.get(id);
    if (!ev) continue;
    const chosenId = Number.isInteger((state.chosen[w.id] || {})[id])
      ? (state.chosen[w.id] || {})[id]
      : null;
    const chosenS = chosenId != null ? (w.suggestions || [])[chosenId] : null;
    if (chosenS && chosenS.kind === 'hide') continue;
    let layer = Number(ev.layer) || 0;
    if (chosenS && chosenS.kind === 'top') layer = 1000000;
    else if (chosenS && chosenS.kind === 'raise') layer += 1;
    if (ev.moving) {
      if (!ev.exactBBox || !Number.isFinite(ev.exactSeek) || Math.abs(time - ev.exactSeek) > 0.2) continue;
    } else if (time < ev.start - 0.1 || time > ev.end + 0.1) {
      continue;
    }
    const geom = eventCurrentGeometry(w, id, ev, time);
    if (!geom) continue;
    const b = geom.box;
    const pad = 4;
    if (x >= b.x0 - pad && x <= b.x1 + pad && y >= b.y0 - pad && y <= b.y1 + pad) {
      // Higher layers draw on top; within a layer, later events win.
      const key = layer * 1000000 + id;
      if (!best || key > best.key) best = { eventId: id, box: b, key };
    }
  }
  return best;
}

function previewDragEventIds(w, hitId) {
  const sel = Object.keys(state.groupSel[w.id] || {}).map(Number);
  if (sel.length > 1 && sel.includes(hitId)) return sel;
  return [hitId];
}

function previewDragBoxes(w, ids, time) {
  const map = reviewEvents();
  const out = [];
  for (const id of ids) {
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
  const threshold = 6 * (drag.playH / 360);
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
        const geom = eventCurrentGeometry(w, id, ev, drag.time);
        if (!geom) continue;
        xTargets.push(geom.box.x0, geom.box.x1);
        yTargets.push(geom.box.y0, geom.box.y1);
      }
    }
  }
  const bestShift = (edges, targets) => {
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
  dx += bestShift([u.x0, u.x1], xTargets);
  dy += bestShift([u.y0, u.y1], yTargets);
  return { dx: Math.round(dx * 10) / 10, dy: Math.round(dy * 10) / 10 };
}

function highlightPreviewSelection(w) {
  const sel = state.selected;
  for (const el of windowSuggestions.querySelectorAll('.event-fixes[data-event]')) {
    const id = Number(el.dataset.event);
    el.classList.toggle('selected', !!(sel && sel.windowId === w.id && sel.eventId === id));
  }
}

function handlePreviewDrag(p) {
  if (!p || !state.reviewWindow || !state.merged) return;
  const w = state.reviewWindow;
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
    const hit = hitTestPreviewEvent(w, p.nx, p.ny, t);
    if (!hit) {
      state.selected = null;
      state.drag = null;
      highlightPreviewSelection(w);
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
    const boxes = previewDragBoxes(w, ids, t);
    state.drag = {
      windowId: w.id, eventIds: ids, hitEvent: hit.eventId, mode: p.mode, time: t,
      boxes, playW: play.w, playH: play.h,
    };
    state.selected = { windowId: w.id, eventId: hit.eventId };
    highlightPreviewSelection(w);
    const el = windowSuggestions.querySelector(`.event-fixes[data-event="${hit.eventId}"]`);
    if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest' });
    if (S.dragBoxes) S.dragBoxes({ playW: play.w, playH: play.h, boxes });
    setStatus(`selected #${hit.eventId}; drag to move` +
      (p.mode === 'axis' ? ', axis locked' : p.mode === 'fine' ? ', fine' : ''));
  } else if (p.phase === 'up') {
    const drag = state.drag;
    state.drag = null;
    if (!drag || drag.windowId !== w.id) return;
    let dx = Number(p.dx) || 0;
    let dy = Number(p.dy) || 0;
    if (Math.abs(dx) < 0.2 && Math.abs(dy) < 0.2) {
      setStatus(`selected #${drag.hitEvent}; drag to move, arrows to nudge`);
      return;
    }
    ({ dx, dy } = snapPreviewDrag(drag, dx, dy));
    moveEventsBy(w, drag.eventIds, dx, dy, { source: 'drag' });
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
