const { contextBridge, ipcRenderer, webUtils } = require('electron');

contextBridge.exposeInMainWorld('subsandwicher', {
  probeVideo: (videoPath) => ipcRenderer.invoke('probe-video', videoPath),
  mergeSubtitles: (payload) => ipcRenderer.invoke('merge-subtitles', payload),
  onMergeProgress: (cb) => { ipcRenderer.on('merge-progress', (_e, p) => cb(p)); },
  onPlaybackEnded: (cb) => { ipcRenderer.on('playback-ended', () => cb()); },
  onPlaybackPosition: (cb) => { ipcRenderer.on('playback-position', (_e, p) => cb(p)); },
  onPreviewDrag: (cb) => { ipcRenderer.on('preview-drag', (_e, p) => cb(p)); },
  dragBoxes: (payload) => ipcRenderer.invoke('preview-drag-boxes', payload),
  cancelDrag: () => ipcRenderer.invoke('preview-drag-cancel'),
  launchPreview: (payload) => ipcRenderer.invoke('launch-preview', payload),
  nextFrame: (dir) => ipcRenderer.invoke('review-next-frame', dir),
  jumpRandomFrame: (payload) => ipcRenderer.invoke('jump-random-frame', payload),
  seekPreview: (payload) => ipcRenderer.invoke('seek-preview', payload),
  applyPreview: (payload) => ipcRenderer.invoke('apply-preview', payload),
  previewPlayback: (payload) => ipcRenderer.invoke('preview-playback', payload),
  confirmSave: (payload) => ipcRenderer.invoke('confirm-save', payload),
  getLogPath: () => ipcRenderer.invoke('get-log-path'),
  getAppMeta: () => ipcRenderer.invoke('get-app-meta'),
  eject: () => ipcRenderer.invoke('eject'),
  copyText: (text) => ipcRenderer.invoke('copy-text', text),
  fitWindow: () => ipcRenderer.invoke('fit-window'),
  openLogsFolder: () => ipcRenderer.invoke('open-logs-folder'),
  pathForFile: (file) => {
    try { return webUtils.getPathForFile(file); } catch (e) { return null; }
  },
});
