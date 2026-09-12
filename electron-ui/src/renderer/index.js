let state = {
  videoPath: null,
  streams: [],
  primaryIndex: null,
  secondaryIndex: null,
  merged: null, // {out, styles, lines, events}
};

const sel = (s) => document.querySelector(s);
const S = window.subsandwicher;

const dropzone = sel('#dropzone');
const actions = sel('#actions');
const status = sel('#status');
const logline = sel('#logline');
const btnPreview = sel('#btn-preview');
const progwrap = sel('#progwrap');
const progbar = sel('#progbar');
const btnAnother = sel('#btn-other-frame');
const btnSave = sel('#btn-save');

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

// ---------- log path display ----------
(async () => {
  const p = await S.getLogPath();
  logline.textContent = `logs: ${p} (click to open folder)`;
  logline.onclick = () => S.openLogsFolder();
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

async function loadVideo(p) {
  if (!p) { setStatus("couldn't resolve the dropped file path", true); return; }
  state.videoPath = p;
  sel('#filename').textContent = p;
  setStatus('probing streams…');
  try {
    const res = await S.probeVideo(p);
    state.streams = res.streams || [];
    state.merged = null;
    buildStreamLists();
    actions.classList.remove('hidden');
    try { await S.fitWindow(); } catch (e) { console.warn('fit-window failed', e); }
    setStatus(`${state.streams.length} subtitle stream(s) found — pick primary and secondary, then "Generate preview".`);
  } catch (e) {
    setStatus('probe failed: ' + e.message, true);
  }
}

// ---------- stream list building ----------
function streamLabel(s) {
  const lang = (s.tags && s.tags.language) || '';
  const title = (s.tags && s.tags.title) || 'untitled';
  const tag = s.bitmapped ? ' [image-based — not usable]' : '';
  return `#${s.index}  ${title} — ${langName(lang)}${tag}`;
}

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
        state[role === 'prim' ? 'primaryIndex' : 'secondaryIndex'] = s.index;
        for (const l of wrap.querySelectorAll('label')) l.classList.remove('checked');
        label.classList.add('checked');
        updateButtonStates();
      });
      box.appendChild(label);
    }
  };
  makeList('#primaryOptions', 'prim');
  makeList('#secondaryOptions', 'sec');
  updateButtonStates();
}

function updateButtonStates() {
  btnPreview.disabled = !(state.primaryIndex != null && state.secondaryIndex != null && state.primaryIndex !== state.secondaryIndex);
  btnAnother.disabled = true;
  btnSave.disabled = true;
}

// ---------- actions ----------
btnPreview.addEventListener('click', async () => {
  try {
    btnPreview.disabled = true;
    setStatus('extracting and merging subtitle streams…');
    let lastProgress = 0;
    S.onMergeProgress((p) => {
      // throttle to 10fps: update the bar only when it moved enough
      const overall = Math.min(100, Math.round((p.overall || 0) * 100));
      if (overall === lastProgress) return;
      lastProgress = overall;
      progwrap.classList.remove('hidden');
      progbar.style.width = overall + '%';
      const secs = typeof p.seconds === 'number' && p.seconds >= 0 ? p.seconds : null;
      setStatus(`extracting ${p.role} subs` +
        (secs != null ? ` — ${fmtTime(secs)} of ${fmtTime(p.total_seconds)}` : ''));
    });
    let stats;
    try {
      stats = await S.mergeSubtitles({
        videoPath: state.videoPath,
        primaryIndex: state.primaryIndex,
        secondaryIndex: state.secondaryIndex,
      });
    } finally {
      progwrap.classList.add('hidden');
    }
    state.merged = stats; // { out, styles, lines, events }
    setStatus(`merged: ${stats.styles} styles, ${stats.lines} lines → launching preview…`);
    const close = pickEventTime(stats.events);
    await S.launchPreview({ videoPath: state.videoPath, mergedPath: stats.out, seekTo: close });
    setStatus('preview open in mpv window. Hit "Another frame" for a different dialogue line, or save if it looks right.');
    btnAnother.disabled = false;
    btnSave.disabled = false;
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
    });
    if (res.ok) {
      setStatus(`saved: ${res.dest}`);
    } else {
      setStatus('save failed: ' + (res.error || 'unknown'), true);
      btnSave.disabled = false;
    }
  } catch (e) {
    setStatus('save failed: ' + e.message, true);
    btnSave.disabled = false;
  }
}

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

// exposed for browser-based smoke tests (harmless in production)
window.__loadVideo = loadVideo;
