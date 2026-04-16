import { useState, useCallback, useEffect } from 'react';
import { SessionList } from './components/SessionList';
import { ChatView } from './components/ChatView';
import { ConfigPanel } from './components/ConfigPanel';
import type { Session, ChatMessage, ProjectInfo } from './types';

export function App() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [selectedSession, setSelectedSession] = useState<Session | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [showConfig, setShowConfig] = useState(false);
  const [loading, setLoading] = useState(false);
  const [progress, setProgress] = useState('');
  const [projects, setProjects] = useState<ProjectInfo[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>('');

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
    refreshProjects();
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
    setMessages([]);
    setProgress('');

    // Try to load transcript from sense contract
    const senseRaw = await window.heb.sessionRead(session.id, 'sense');
    if (senseRaw) {
      try {
        const sense = JSON.parse(senseRaw);
        if (sense.raw) {
          setMessages(prev => [...prev, { role: 'user', content: sense.raw }]);
        }
      } catch { /* malformed JSON */ }
    }

    // Load reflect summary if available
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

  const handleSend = useCallback(async (text: string) => {
    if (!text.trim() || loading) return;

    const isNewSession = !selectedSession || selectedSession.status === 'complete';
    setMessages(prev => [...prev, { role: 'user', content: text }]);
    setLoading(true);
    setProgress('');

    const cleanup = window.heb.onProgress((data) => {
      setProgress(prev => {
        const lines = (prev + data).split('\n');
        return lines.slice(-5).join('\n');
      });
    });

    try {
      let result;
      if (isNewSession) {
        result = await window.heb.run(text, selectedProject || undefined);
      } else {
        result = await window.heb.resume(selectedSession!.id, text, selectedProject || undefined);
      }

      setMessages(prev => [...prev, {
        role: 'assistant',
        content: result.stdout || '(no output)',
      }]);

      // Refresh session list to pick up new session
      await refreshSessions();
    } catch (err: any) {
      setMessages(prev => [...prev, {
        role: 'system',
        content: `Error: ${err.message}`,
      }]);
    } finally {
      cleanup();
      setLoading(false);
      setProgress('');
    }
  }, [selectedSession, loading, refreshSessions]);

  const handleRemember = useCallback(async () => {
    if (!selectedSession || loading) return;

    setLoading(true);
    setProgress('');

    const cleanup = window.heb.onProgress((data) => {
      setProgress(prev => {
        const lines = (prev + data).split('\n');
        return lines.slice(-5).join('\n');
      });
    });

    try {
      const result = await window.heb.remember(selectedSession.id, selectedProject || undefined);
      setMessages(prev => [...prev, {
        role: 'system',
        content: `Session completed.\n\n${result.stderr || ''}`,
      }]);
      await refreshSessions();
    } catch (err: any) {
      setMessages(prev => [...prev, {
        role: 'system',
        content: `Remember failed: ${err.message}`,
      }]);
    } finally {
      cleanup();
      setLoading(false);
      setProgress('');
    }
  }, [selectedSession, loading, refreshSessions]);

  const handleTrash = useCallback(async () => {
    if (!selectedSession || loading) return;

    try {
      await window.heb.trash(selectedSession.id);
      setSelectedSession(null);
      setMessages([]);
      setProgress('');
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
          setProgress('');
        }}
        onOpenConfig={() => setShowConfig(true)}
      />

      <ChatView
        session={selectedSession}
        messages={messages}
        loading={loading}
        progress={progress}
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
