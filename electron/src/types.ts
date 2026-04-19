export interface ProjectInfo {
  path: string;
  name: string;
  active_count: number;
  total_count: number;
  memory_count: number;
}

export interface HebAPI {
  listProjects: () => Promise<ProjectInfo[]>;
  browseProject: () => Promise<string | null>;
  initProject: (dir: string) => Promise<ProjectInfo[]>;
  listSessions: (limit?: number) => Promise<string>;
  sessionDetail: (id: string) => Promise<string>;
  sessionRead: (id: string, step: string) => Promise<string>;
  chatSave: (sessionId: string, messages: Array<{ role: string; content: string; phase?: string }>) => Promise<boolean>;
  chatList: (sessionId: string) => Promise<ChatLogEntry[]>;
  trash: (id: string) => Promise<boolean>;
  configGet: (key: string) => Promise<string>;
  configSet: (key: string, value: string) => Promise<boolean>;
  run: (prompt: string, projectDir?: string) => Promise<{ stdout: string; stderr: string }>;
  resume: (sessionId: string, prompt: string, projectDir?: string) => Promise<{ stdout: string; stderr: string }>;
  remember: (sessionId: string, projectDir?: string) => Promise<{ stdout: string; stderr: string }>;
  onProgress: (callback: (data: string) => void) => () => void;
}

declare global {
  interface Window {
    heb: HebAPI;
  }
}

export interface Session {
  id: string;
  status: 'active' | 'complete' | 'trashed';
  project: string;
  steps: string;
  age: string;
}

export interface StepStats {
  in?: number;
  out?: number;
  ms?: number;
  turns?: number;
  cost?: string; // e.g. "$0.0123"
}

export interface ChatMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
  phase?: string;
  timestamp?: number;
  stats?: StepStats;
}

export interface ChatLogEntry {
  id: number;
  session_id: string;
  role: string;
  content: string;
  phase?: string;
  created_at: number;
}

export type ConfigKey = 'provider' | 'anthropic-key' | 'openai-key' | 'learn.model' | 'verbosity';
