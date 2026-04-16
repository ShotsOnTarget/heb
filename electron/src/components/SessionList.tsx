import { useEffect } from 'react';
import type { Session } from '../types';

interface Props {
  sessions: Session[];
  selected: Session | null;
  processingId: string | null;
  onSelect: (session: Session) => void;
  onRefresh: () => void;
  onNewSession: () => void;
  onOpenConfig: () => void;
}

export function SessionList({ sessions, selected, processingId, onSelect, onRefresh, onNewSession, onOpenConfig }: Props) {
  useEffect(() => {
    onRefresh();
  }, [onRefresh]);

  // Sort: active first, then by ID descending (IDs are ISO timestamps)
  const sorted = [...sessions].sort((a, b) => {
    if (a.status === 'active' && b.status !== 'active') return -1;
    if (a.status !== 'active' && b.status === 'active') return 1;
    return b.id.localeCompare(a.id);
  });

  return (
    <div className="sidebar">
      <div className="sidebar-header">
        <h1>heb</h1>
        <div className="sidebar-actions">
          <button onClick={onNewSession} title="New session">+</button>
          <button onClick={onOpenConfig} title="Settings">&#9881;</button>
          <button onClick={onRefresh} title="Refresh">&#8635;</button>
        </div>
      </div>

      <div className="session-list">
        {sorted.length === 0 && (
          <div style={{ padding: '20px', textAlign: 'center', color: 'var(--text-muted)', fontSize: '13px' }}>
            No sessions yet.
            <br />
            Start typing to begin.
          </div>
        )}

        {sorted.map(session => (
          <div
            key={session.id}
            className={[
              'session-item',
              session.status,
              selected?.id === session.id ? 'selected' : '',
              processingId === session.id ? 'processing' : '',
            ].filter(Boolean).join(' ')}
            onClick={() => onSelect(session)}
          >
            <div className="session-id">{formatSessionId(session.id)}</div>
            <div className="session-meta">
              <span className="session-project">{session.project.split('/').pop() || session.project}</span>
              <span className={`session-status ${session.status}`}>
                {session.status}
              </span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function formatSessionId(id: string): string {
  // "2026-04-15T10:30:00Z" → "Apr 15, 10:30"
  try {
    const d = new Date(id);
    if (isNaN(d.getTime())) return id;
    const month = d.toLocaleString('en', { month: 'short' });
    const day = d.getDate();
    const time = d.toLocaleTimeString('en', { hour: '2-digit', minute: '2-digit', hour12: false });
    return `${month} ${day}, ${time}`;
  } catch {
    return id;
  }
}
