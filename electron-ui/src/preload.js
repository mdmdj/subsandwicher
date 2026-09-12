const { contextBridge, ipcRenderer, webUtils } = require('electron');

contextBridge.exposeInMainWorld('subsandwicher', {
  probeVideo: (videoPath) => ipcRenderer.invoke('probe-video', videoPath),
  mergeSubtitles: (payload) => ipcRenderer.invoke('merge-subtitles', payload),
  onMergeProgress: (cb) => { ipcRenderer.on('merge-progress', (_e, p) => cb(p)); },
  launchPreview: (payload) => ipcRenderer.invoke('launch-preview', payload),
  nextFrame: (dir) => ipcRenderer.invoke('review-next-frame', dir),
  jumpRandomFrame: (payload) => ipcRenderer.invoke('jump-random-frame', payload),
  confirmSave: (payload) => ipcRenderer.invoke('confirm-save', payload),
  getLogPath: () => ipcRenderer.invoke('get-log-path'),
  fitWindow: () => ipcRenderer.invoke('fit-window'),
  openLogsFolder: () => ipcRenderer.invoke('open-logs-folder'),
  pathForFile: (file) => {
    try { return webUtils.getPathForFile(file); } catch (e) { return null; }
  },
});
