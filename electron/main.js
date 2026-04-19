const { app, BrowserWindow, ipcMain, dialog, Menu } = require('electron');
const path = require('path');
const { spawn, execFile } = require('child_process');

const HEB_BIN = process.env.HEB_BIN || 'heb';

// Run a heb command and return stdout as a string.
function hebExec(args, { stdin, cwd } = {}) {
  return new Promise((resolve, reject) => {
    const proc = execFile(HEB_BIN, args, {
      cwd: cwd || process.env.HEB_PROJECT || process.cwd(),
      maxBuffer: 10 * 1024 * 1024,
      timeout: 300_000,
    }, (err, stdout, stderr) => {
      if (err) {
        reject(new Error(`heb ${args.join(' ')} failed: ${stderr || err.message}`));
      } else {
        resolve({ stdout: stdout.trim(), stderr: stderr.trim() });
      }
    });
    if (stdin) {
      proc.stdin.write(stdin);
      proc.stdin.end();
    }
  });
}

// Track active child processes so we can kill them on quit.
const activeProcs = new Set();

// Run a long-running heb command, streaming stderr progress back via IPC.
function hebSpawn(args, { cwd, onStdout, onStderr } = {}) {
  return new Promise((resolve, reject) => {
    const proc = spawn(HEB_BIN, args, {
      cwd: cwd || process.env.HEB_PROJECT || process.cwd(),
    });

    activeProcs.add(proc);

    let stdout = '';
    let stderr = '';

    proc.stdout.on('data', (data) => {
      const chunk = data.toString();
      stdout += chunk;
      if (onStdout) onStdout(chunk);
    });

    proc.stderr.on('data', (data) => {
      const chunk = data.toString();
      stderr += chunk;
      if (onStderr) onStderr(chunk);
    });

    proc.on('close', (code) => {
      activeProcs.delete(proc);
      if (code !== 0) {
        reject(new Error(`heb exited ${code}: ${stderr}`));
      } else {
        resolve({ stdout: stdout.trim(), stderr: stderr.trim() });
      }
    });

    proc.on('error', (err) => {
      activeProcs.delete(proc);
      reject(err);
    });
  });
}

// Kill all active child processes and their trees.
function killActiveProcs() {
  for (const proc of activeProcs) {
    try {
      // On Windows, taskkill /T kills the entire process tree
      if (process.platform === 'win32') {
        spawn('taskkill', ['/pid', String(proc.pid), '/T', '/F'], { stdio: 'ignore' });
      } else {
        process.kill(-proc.pid, 'SIGTERM');
      }
    } catch {
      // Already dead
    }
  }
  activeProcs.clear();
}

function createWindow() {
  const win = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 800,
    minHeight: 600,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
    titleBarStyle: 'hidden',
    titleBarOverlay: {
      color: '#cec5b4',
      symbolColor: '#2b2d54',
      height: 48,
    },
    autoHideMenuBar: true,
    title: 'heb',
  });

  Menu.setApplicationMenu(null);

  // Try dev server first (npm run start), fall back to built dist/
  const devURL = 'http://localhost:9000';
  const distFile = path.join(__dirname, 'dist', 'index.html');

  if (process.env.HEB_DEV === '1') {
    win.loadURL(devURL);
  } else {
    win.loadFile(distFile);
  }

  return win;
}

app.whenReady().then(() => {
  const win = createWindow();

  // --- IPC Handlers ---

  // List known projects
  ipcMain.handle('heb:projects', async () => {
    const { stdout } = await hebExec(['projects']);
    return JSON.parse(stdout);
  });

  // Browse for a project directory
  ipcMain.handle('heb:browse-project', async () => {
    const result = await dialog.showOpenDialog(win, {
      properties: ['openDirectory'],
      title: 'Select project directory',
    });
    if (result.canceled || result.filePaths.length === 0) return null;
    return result.filePaths[0];
  });

  // Init heb for a directory (registers the project)
  ipcMain.handle('heb:init-project', async (_event, projectDir) => {
    await hebExec(['init'], { cwd: projectDir });
    // Return refreshed project list
    const { stdout } = await hebExec(['projects']);
    return JSON.parse(stdout);
  });

  // List sessions
  ipcMain.handle('heb:sessions', async (_event, limit = 20) => {
    const { stdout } = await hebExec(['session', 'list']);
    return stdout;
  });

  // Get config value
  ipcMain.handle('heb:config-get', async (_event, key) => {
    const { stdout } = await hebExec(['config', 'get', key]);
    return stdout;
  });

  // Set config value
  ipcMain.handle('heb:config-set', async (_event, key, value) => {
    await hebExec(['config', 'set', key, value]);
    return true;
  });

  // Start new session (full pipeline: sense → retrieve → reflect → execute)
  ipcMain.handle('heb:run', async (event, prompt, projectDir) => {
    if (!projectDir) throw new Error('No project selected');
    const result = await hebSpawn(prompt.split(/\s+/), {
      cwd: projectDir,
      onStderr: (chunk) => {
        event.sender.send('heb:progress', chunk);
      },
    });
    return result;
  });

  // Resume session with new prompt
  ipcMain.handle('heb:resume', async (event, sessionId, prompt, projectDir) => {
    if (!projectDir) throw new Error('No project selected');
    const args = ['resume'];
    if (sessionId) args.push(sessionId);
    args.push(...prompt.split(/\s+/));
    const result = await hebSpawn(args, {
      cwd: projectDir,
      onStderr: (chunk) => {
        event.sender.send('heb:progress', chunk);
      },
    });
    return result;
  });

  // Remember (learn + consolidate + close)
  ipcMain.handle('heb:remember', async (event, sessionId, projectDir) => {
    if (!projectDir) throw new Error('No project selected');
    const args = ['remember'];
    if (sessionId) args.push(sessionId);
    const result = await hebSpawn(args, {
      cwd: projectDir,
      onStderr: (chunk) => {
        event.sender.send('heb:progress', chunk);
      },
    });
    return result;
  });

  // Trash session (discard without learning)
  ipcMain.handle('heb:trash', async (_event, sessionId) => {
    await hebExec(['session', 'trash', sessionId]);
    return true;
  });

  // Session detail (resume view)
  ipcMain.handle('heb:session-resume', async (_event, sessionId) => {
    const { stdout } = await hebExec(['session', 'resume', sessionId]);
    return stdout;
  });

  // Persist all GUI chat messages for a session (clear + rewrite)
  ipcMain.handle('heb:chat-save', async (_event, sessionId, messages) => {
    const payload = JSON.stringify(messages);
    await hebExec(['session', 'chat-save', sessionId], { stdin: payload });
    return true;
  });

  // List all GUI chat messages for a session
  ipcMain.handle('heb:chat-list', async (_event, sessionId) => {
    try {
      const { stdout } = await hebExec(['session', 'chat-list', sessionId]);
      return JSON.parse(stdout || '[]');
    } catch {
      return [];
    }
  });

  // Read session transcript (contract) — returns null if missing
  ipcMain.handle('heb:session-read', async (_event, sessionId, step) => {
    try {
      const { stdout } = await hebExec(['session', 'read', sessionId, step]);
      return stdout;
    } catch {
      return null;
    }
  });

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('window-all-closed', () => {
  killActiveProcs();
  if (process.platform !== 'darwin') app.quit();
});

app.on('before-quit', () => {
  killActiveProcs();
});
