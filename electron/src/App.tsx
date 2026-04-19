import { useState, useCallback, useEffect, useRef } from 'react';
import { SessionList } from './components/SessionList';
import { ChatView } from './components/ChatView';
import { ConfigPanel } from './components/ConfigPanel';
import type { Session, ChatMessage, ProjectInfo, StepStats } from './types';

// Known pipeline phases and the stderr patterns that start them.
const PHASE_PATTERNS: [RegExp, string][] = [
  [/^sensing tokens:/, 'Sense'],
  [/^sensing/, 'Sense'],
  [/^recall-mem:/, 'Recall'],
  [/^recall-git:/, 'Recall'],
  [/^recall:/, 'Recall'],
  [/^reflecting/, 'Reflecting'],
  [/^reconcile-/, 'Reconcile'],
  [/^reconcile:/, 'Reconcile'],
  [/^predict-/, 'Predict'],
  [/^predict:/, 'Predict'],
  [/^filtered /, 'Reconcile'],
  [/^prepare-/, 'Prepare'],
  [/^prepare:/, 'Prepare'],
  [/^executing/, 'Claude'],
  [/^▸ /, 'Claude'],         // streaming tool status
  [/^transcript stored/, 'Done'],
  [/^files touched/, 'Done'],
];

function detectPhase(line: string): string | null {
  for (const [re, phase] of PHASE_PATTERNS) {
    if (re.test(line)) return phase;
  }
  return null;
}

// Strip the phase keyword prefix from content since it's already shown in the role label.
// e.g. "sensing [gpt-4.1-mini]: some text" → "some text"
//      "recall: 11 memories ..." → "11 memories ..."
//      "reflecting [gpt-4.1-mini]..." → "..."
function stripPhasePrefix(line: string): string {
  return line
    .replace(/^(?:sensing tokens|sensing|recall-mem|recall-git|recall|reflecting|reconcile-conflict|reconcile-notes|reconcile|predict-file|predict-approach|predict-outcome|predict-risk|predict|prepare-hit|prepare-stale|prepare|executing|filtered|transcript stored|files touched)\s*/, '')
    .replace(/^\[[^\]]*\]\s*:?\s*/, '') // strip [model]: prefix
    .replace(/^:\s*/, '');              // strip leftover colon
}

// Extract model name from brackets, e.g. "sensing [haiku]: ..." → "haiku"
function extractModel(line: string): string | null {
  const m = line.match(/\[([^\]]+)\]/);
  return m ? m[1] : null;
}

// Stats line format: "stats: <step> in=N out=N ms=N [turns=N] [cost=$X]"
// Returns the step name and parsed stats, or null if not a stats line.
function parseStatsLine(line: string): { step: string; stats: StepStats } | null {
  const m = line.match(/^stats:\s+(\w+)\s+(.+)$/);
  if (!m) return null;
  const step = m[1];
  const stats: StepStats = {};
  for (const pair of m[2].split(/\s+/)) {
    const eq = pair.indexOf('=');
    if (eq === -1) continue;
    const k = pair.slice(0, eq);
    const v = pair.slice(eq + 1);
    switch (k) {
      case 'in': stats.in = parseInt(v, 10); break;
      case 'out': stats.out = parseInt(v, 10); break;
      case 'ms': stats.ms = parseInt(v, 10); break;
      case 'turns': stats.turns = parseInt(v, 10); break;
      case 'cost': stats.cost = v; break;
    }
  }
  return { step, stats };
}

// Map a stats step name to the phase label(s) it should annotate.
// Returns an ordered list of phase names to search in the messages list.
function statsStepToPhases(step: string): string[] {
  switch (step) {
    case 'sense':    return ['Sense'];
    case 'reflect':  return ['Predict', 'Reconcile'];
    case 'prepare':  return ['Prepare'];
    case 'execute':  return ['Claude'];
    case 'remember': return ['Claude', 'Done'];
    default:         return [];
  }
}

export function App() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [selectedSession, setSelectedSession] = useState<Session | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [showConfig, setShowConfig] = useState(false);
  const [loading, setLoading] = useState(false);
  const [projects, setProjects] = useState<ProjectInfo[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>('');
  const phaseRef = useRef<string | null>(null);
  const reflectModelRef = useRef<string | null>(null);
  const sessionIdRef = useRef<string | null>(null);
  const messagesRef = useRef<ChatMessage[]>([]);
  const messageCacheRef = useRef<Map<string, ChatMessage[]>>(new Map());

  // Keep ref in sync with state so we can read current messages in callbacks.
  // Also cache messages by session ID so switching sessions doesn't lose in-flight data.
  useEffect(() => {
    messagesRef.current = messages;
    const sid = selectedSession?.id || sessionIdRef.current;
    if (sid && messages.length > 0) {
      messageCacheRef.current.set(sid, messages);
    }
  }, [messages, selectedSession]);

  // Persist all current messages to the GUI chat log for a session (clear + rewrite)
  const persistMessages = useCallback(async (sid: string) => {
    const msgs = messagesRef.current;
    try {
      await window.heb.chatSave(sid, msgs.map(m => ({
        role: m.role,
        content: m.content,
        phase: m.phase,
      })));
    } catch (err) {
      console.error('Failed to persist chat log:', err);
    }
  }, []);

  const refreshProjects = useCallback(async () => {
    try {
      const p = await window.heb.listProjects();
      setProjects(p);
      if (p.length > 0 && !selectedProject) {
        setSelectedProject(p[0].path);
      }
    } catch (err) {
      console.error('Failed to list projects:', err);
    }
  }, [selectedProject]);

  // Load projects on mount
  useEffect(() => {
    window.heb.listProjects().then(p => {
      setProjects(p);
      if (p.length > 0) {
        setSelectedProject(p[0].path);
      }
    }).catch(err => console.error('Failed to list projects:', err));
  }, []);

  const handleAddProject = useCallback(async () => {
    const dir = await window.heb.browseProject();
    if (!dir) return;
    try {
      const updated = await window.heb.initProject(dir);
      setProjects(updated);
      // Select the newly added project — it'll be in the list
      const normalised = dir.replace(/\\/g, '/');
      const match = updated.find(p => p.path === normalised);
      if (match) {
        setSelectedProject(match.path);
      }
    } catch (err: any) {
      console.error('Failed to init project:', err);
    }
  }, []);

  const refreshSessions = useCallback(async () => {
    try {
      const raw = await window.heb.listSessions(20);
      const parsed = parseSessionList(raw);
      setSessions(parsed);
    } catch (err) {
      console.error('Failed to list sessions:', err);
    }
  }, []);

  const selectSession = useCallback(async (session: Session) => {
    setSelectedSession(session);

    // Restore from in-memory cache first (preserves in-flight messages)
    const cached = messageCacheRef.current.get(session.id);
    if (cached && cached.length > 0) {
      setMessages(cached);
      return;
    }

    setMessages([]);

    // Load persisted GUI chat log
    const entries = await window.heb.chatList(session.id);
    if (entries && entries.length > 0) {
      const restored: ChatMessage[] = entries.map(e => ({
        role: e.role as ChatMessage['role'],
        content: e.content,
        phase: e.phase || undefined,
        timestamp: e.created_at,
      }));
      setMessages(restored);
      return;
    }

    // Fallback for sessions created before chat log existed:
    // load sense + reflect contracts
    const senseRaw = await window.heb.sessionRead(session.id, 'sense');
    if (senseRaw) {
      try {
        const sense = JSON.parse(senseRaw);
        if (sense.raw) {
          setMessages(prev => [...prev, { role: 'user', content: sense.raw }]);
        }
      } catch { /* malformed JSON */ }
    }

    const reflectRaw = await window.heb.sessionRead(session.id, 'reflect');
    if (reflectRaw) {
      try {
        const reflect = JSON.parse(reflectRaw);
        const notes = reflect.notes || reflect.status || '';
        if (notes) {
          setMessages(prev => [...prev, {
            role: 'system',
            content: `Reflect: ${notes}`,
          }]);
        }
      } catch { /* malformed JSON */ }
    }
  }, []);

  // Creates a progress handler that parses stderr into phase-based chat messages.
  const createProgressHandler = useCallback(() => {
    const partial = { buf: '' };
    return window.heb.onProgress((data: string) => {
      partial.buf += data;
      const lines = partial.buf.split('\n');
      partial.buf = lines.pop() || '';

      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) continue;

        // Capture session ID from stderr (e.g. "session started: heb-abc123")
        const sidMatch = trimmed.match(/^session started: (\S+)/);
        if (sidMatch) {
          sessionIdRef.current = sidMatch[1];
        }

        // Stats lines annotate the most-recent matching phase message rather
        // than creating a new one.
        const statsEvent = parseStatsLine(trimmed);
        if (statsEvent) {
          const candidatePhases = statsStepToPhases(statsEvent.step);
          setMessages(prev => {
            const updated = [...prev];
            for (let i = updated.length - 1; i >= 0; i--) {
              const m = updated[i];
              if (m.role !== 'system' || !m.phase) continue;
              // phase may be "Claude · haiku" — match on the prefix before " · "
              const phaseName = m.phase.split(' · ')[0];
              if (candidatePhases.length === 0 || candidatePhases.includes(phaseName)) {
                updated[i] = { ...m, stats: statsEvent.stats };
                break;
              }
            }
            return updated;
          });
          continue;
        }

        const phase = detectPhase(trimmed);
        const model = extractModel(trimmed);

        // The "Reflecting" phase is a thin wrapper around the single LLM call
        // that produces both Reconcile and Predict. Don't surface it as its
        // own chat entry — capture its model and tag it onto Reconcile/Predict.
        if (phase === 'Reflecting') {
          if (model) reflectModelRef.current = model;
          continue;
        }

        const inheritedModel = (phase === 'Reconcile' || phase === 'Predict') && !model
          ? reflectModelRef.current
          : null;
        const effectiveModel = model || inheritedModel;
        const label = effectiveModel ? `${phase} · ${effectiveModel}` : phase;
        const cleaned = phase ? stripPhasePrefix(trimmed) : trimmed;
        // Detail lines append rather than replace
        const isDetailLine = /^(recall-(mem|git)|reconcile-(notes|conflict)|predict-(file|approach|outcome|risk)|prepare-(hit|stale)):/.test(trimmed)
          || /^▸ /.test(trimmed);
        if (phase && phase !== phaseRef.current) {
          // New phase — add a new system message
          phaseRef.current = phase;
          setMessages(prev => [...prev, { role: 'system', content: cleaned, phase: label || undefined }]);
        } else if (phase === phaseRef.current) {
          setMessages(prev => {
            const updated = [...prev];
            const last = updated[updated.length - 1];
            if (last && last.role === 'system') {
              if (isDetailLine) {
                // Append detail line
                updated[updated.length - 1] = { ...last, content: last.content + '\n' + cleaned };
              } else {
                // Replace (other phases update in place)
                updated[updated.length - 1] = { ...last, content: cleaned };
              }
            }
            return updated;
          });
        }
      }
    });
  }, []);

  // Resolve a session's project field to a real filesystem path.
  // The session may store either a path or a display name; look up the
  // projects list to find the real path, falling back to selectedProject.
  const resolveProjectDir = useCallback((sessionProject?: string): string => {
    if (sessionProject) {
      // If it already looks like an absolute path, use it directly
      if (sessionProject.includes('/') || sessionProject.includes('\\')) {
        return sessionProject;
      }
      // Try to match display name to a project path
      const match = projects.find(p => p.name === sessionProject);
      if (match) return match.path;
    }
    return selectedProject;
  }, [projects, selectedProject]);

  const handleSend = useCallback(async (text: string) => {
    if (!text.trim() || loading) return;

    const isNewSession = !selectedSession || selectedSession.status === 'complete';
    sessionIdRef.current = isNewSession ? null : selectedSession!.id;
    setMessages(prev => [...prev, { role: 'user', content: text }]);
    setLoading(true);
    phaseRef.current = null;
    reflectModelRef.current = null;

    const cleanup = createProgressHandler();

    // Poll session list while pipeline runs so new session appears quickly
    const pollInterval = setInterval(() => refreshSessions(), 2000);

    try {
      // Use the selected project for new sessions, or the session's own project for resumes
      const projectDir = isNewSession ? selectedProject : resolveProjectDir(selectedSession!.project);

      if (!projectDir) {
        setMessages(prev => [...prev, { role: 'system', content: 'Error: No project selected. Pick a project from the dropdown.' }]);
        setLoading(false);
        clearInterval(pollInterval);
        cleanup();
        return;
      }

      let result;
      if (isNewSession) {
        result = await window.heb.run(text, projectDir);
      } else {
        result = await window.heb.resume(selectedSession!.id, text, projectDir);
      }

      setMessages(prev => [...prev, {
        role: 'assistant',
        content: result.stdout || '(no output)',
      }]);

      await refreshSessions();

      // For new sessions, update selectedSession so the UI knows we're in an active session.
      // Store projectDir (the real path) so remember/resume can use it as cwd.
      const sid = sessionIdRef.current;
      if (sid && isNewSession) {
        setSelectedSession({ id: sid, status: 'active', project: projectDir, steps: '', age: '' });
      }

      // Persist chat log now that we have the session ID and final messages
      if (sid) {
        // Small delay to let final setMessages flush
        setTimeout(() => persistMessages(sid), 100);
      }
    } catch (err: any) {
      setMessages(prev => [...prev, {
        role: 'system',
        content: `Error: ${err.message}`,
      }]);
    } finally {
      clearInterval(pollInterval);
      cleanup();
      setLoading(false);
    }
  }, [selectedSession, selectedProject, loading, refreshSessions, createProgressHandler, persistMessages, resolveProjectDir]);

  const handleRemember = useCallback(async () => {
    if (!selectedSession || loading) return;

    setLoading(true);
    phaseRef.current = null;
    reflectModelRef.current = null;

    const cleanup = createProgressHandler();

    try {
      const result = await window.heb.remember(selectedSession.id, resolveProjectDir(selectedSession.project));
      setMessages(prev => [...prev, {
        role: 'system',
        content: `Session completed.\n\n${result.stderr || ''}`,
      }]);
      await refreshSessions();
      // Persist updated chat log
      setTimeout(() => persistMessages(selectedSession.id), 100);
    } catch (err: any) {
      setMessages(prev => [...prev, {
        role: 'system',
        content: `Remember failed: ${err.message}`,
      }]);
    } finally {
      cleanup();
      setLoading(false);
    }
  }, [selectedSession, loading, refreshSessions, persistMessages, resolveProjectDir]);

  const handleTrash = useCallback(async () => {
    if (!selectedSession || loading) return;

    try {
      await window.heb.trash(selectedSession.id);
      setSelectedSession(null);
      setMessages([]);
      await refreshSessions();
    } catch (err: any) {
      setMessages(prev => [...prev, {
        role: 'system',
        content: `Trash failed: ${err.message}`,
      }]);
    }
  }, [selectedSession, loading, refreshSessions]);

  return (
    <div className="app">
      <SessionList
        sessions={sessions}
        selected={selectedSession}
        processingId={loading && selectedSession ? selectedSession.id : null}
        onSelect={selectSession}
        onRefresh={refreshSessions}
        onNewSession={() => {
          setSelectedSession(null);
          setMessages([]);
        }}
        onOpenConfig={() => setShowConfig(true)}
      />

      <ChatView
        session={selectedSession}
        messages={messages}
        loading={loading}
        projects={projects}
        selectedProject={selectedProject}
        onProjectChange={setSelectedProject}
        onAddProject={handleAddProject}
        onSend={handleSend}
        onRemember={handleRemember}
        onTrash={handleTrash}
      />

      {showConfig && (
        <ConfigPanel onClose={() => setShowConfig(false)} />
      )}
    </div>
  );
}

// Parse heb session list output into structured data.
// Format: "STATUS    ID                            PROJECT       STEPS  (AGE ago)"
function parseSessionList(raw: string): Session[] {
  const lines = raw.split('\n').filter(l =>
    l.trim() && !l.startsWith('SESSION') && !l.startsWith('──')
  );

  return lines.map(line => {
    const parts = line.trim().split(/\s{2,}/);
    const status = (parts[0]?.toLowerCase() || 'active') as Session['status'];
    const id = parts[1] || '';
    const project = parts[2] || '';
    const rest = parts.slice(3).join('  ');
    const ageMatch = rest.match(/\((.+?) ago\)/);
    const age = ageMatch?.[1] || '';
    const steps = rest.replace(/\(.*?\)/, '').trim();

    return { id, status, project, steps, age };
  }).filter(s => s.id);
}
