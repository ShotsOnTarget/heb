const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('heb', {
  // Projects
  listProjects: () => ipcRenderer.invoke('heb:projects'),
  browseProject: () => ipcRenderer.invoke('heb:browse-project'),
  initProject: (dir) => ipcRenderer.invoke('heb:init-project', dir),

  // Sessions
  listSessions: (limit) => ipcRenderer.invoke('heb:sessions', limit),
  sessionDetail: (id) => ipcRenderer.invoke('heb:session-resume', id),
  sessionRead: (id, step) => ipcRenderer.invoke('heb:session-read', id, step),

  // GUI chat log
  chatSave: (sessionId, messages) => ipcRenderer.invoke('heb:chat-save', sessionId, messages),
  chatList: (sessionId) => ipcRenderer.invoke('heb:chat-list', sessionId),

  trash: (sessionId) => ipcRenderer.invoke('heb:trash', sessionId),

  // Config
  configGet: (key) => ipcRenderer.invoke('heb:config-get', key),
  configSet: (key, value) => ipcRenderer.invoke('heb:config-set', key, value),

  // Pipeline
  run: (prompt, projectDir) => ipcRenderer.invoke('heb:run', prompt, projectDir),
  resume: (sessionId, prompt, projectDir) => ipcRenderer.invoke('heb:resume', sessionId, prompt, projectDir),
  remember: (sessionId, projectDir) => ipcRenderer.invoke('heb:remember', sessionId, projectDir),

  // Progress events
  onProgress: (callback) => {
    const handler = (_event, data) => callback(data);
    ipcRenderer.on('heb:progress', handler);
    return () => ipcRenderer.removeListener('heb:progress', handler);
  },
});
