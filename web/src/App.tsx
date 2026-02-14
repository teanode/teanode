import React, { useState, useRef, useCallback, useEffect } from 'react';
import type { Session } from './types';
import { useChat } from './hooks/useChat';
import { useCronJobs } from './hooks/useCronJobs';
import { useRouter } from './hooks/useRouter';
import Sidebar from './components/Sidebar';
import TopBar from './components/TopBar';
import ChatArea from './components/ChatArea';
import CronArea from './components/CronArea';

function getSessionTitle(sessions: Session[], key: string | null): string {
  if (!key) return '';
  const s = sessions.find((s) => s.key === key);
  if (s?.title) return s.title;
  return key.length > 40 ? key.substring(0, 16) + '...' + key.substring(key.length - 10) : key;
}

export default function App() {
  const { route, navigate } = useRouter();
  const {
    sessions,
    sessionKey,
    messages,
    isRunning,
    status,
    defaultModel,
    models,
    streamText,
    isStreaming,
    toolActivity,
    sendMessage,
    abortRun,
    switchSession,
    newSession,
    deleteSession,
    renameSession,
    sendRpc,
  } = useChat();

  const cronJobs = useCronJobs(sendRpc);

  const routeRef = useRef(route);
  routeRef.current = route;

  // Derive view state from route
  const activeView = route.page;
  const activeCronJobId = route.page === 'crons' ? route.jobId : null;
  const creatingCronJob = route.page === 'crons' && route.creating;

  // Sync route → chat state
  useEffect(() => {
    if (route.page !== 'chat') return;
    if (route.sessionKey) {
      if (route.sessionKey !== sessionKey) {
        switchSession(route.sessionKey);
      }
    } else if (sessionKey) {
      newSession();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [route]);

  // Sync sessionKey → route (when new session created via sendMessage)
  useEffect(() => {
    if (
      sessionKey &&
      routeRef.current.page === 'chat' &&
      routeRef.current.sessionKey !== sessionKey
    ) {
      navigate(`/chat/${sessionKey}`);
    }
  }, [sessionKey, navigate]);

  // Load cron jobs when navigating to crons page
  useEffect(() => {
    if (route.page === 'crons') {
      cronJobs.loadJobs();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [route.page]);

  const [sidebarOpen, setSidebarOpen] = useState(() => {
    return localStorage.getItem('teanode-sidebar') !== 'false';
  });
  const [showToolCalls, setShowToolCalls] = useState(() => {
    return localStorage.getItem('teanode-show-tools') !== 'false';
  });
  const [model, setModel] = useState(() => {
    return localStorage.getItem('teanode-model') || '';
  });

  const handleModelChange = useCallback((value: string) => {
    setModel(value);
    if (value) {
      localStorage.setItem('teanode-model', value);
    } else {
      localStorage.removeItem('teanode-model');
    }
  }, []);

  const toggleSidebar = useCallback(() => {
    setSidebarOpen((prev) => {
      const next = !prev;
      localStorage.setItem('teanode-sidebar', String(next));
      return next;
    });
  }, []);

  const toggleTools = useCallback(() => {
    setShowToolCalls((prev) => {
      const next = !prev;
      localStorage.setItem('teanode-show-tools', String(next));
      return next;
    });
  }, []);

  const handleSend = useCallback(
    (text: string) => {
      sendMessage(text, model || undefined);
    },
    [sendMessage, model]
  );

  const handleRename = useCallback(
    (title: string) => {
      if (sessionKey) {
        renameSession(sessionKey, title);
      }
    },
    [sessionKey, renameSession]
  );

  const handleDeleteSession = useCallback(
    (key: string) => {
      deleteSession(key);
      if (routeRef.current.page === 'chat' && routeRef.current.sessionKey === key) {
        navigate('/chat');
      }
    },
    [deleteSession, navigate]
  );

  const handleCreateCronJob = useCallback(
    (...args: Parameters<typeof cronJobs.createJob>) => {
      return cronJobs.createJob(...args).then(() => {
        navigate('/crons');
      });
    },
    [cronJobs.createJob, navigate]
  );

  const handleDeleteCronJob = useCallback(
    (id: string) => {
      return cronJobs.deleteJob(id).then(() => {
        if (routeRef.current.page === 'crons' && routeRef.current.jobId === id) {
          navigate('/crons');
        }
      });
    },
    [cronJobs.deleteJob, navigate]
  );

  const selectedCronJob = cronJobs.jobs.find((j) => j.id === activeCronJobId) || null;
  const title = getSessionTitle(sessions, sessionKey);

  return (
    <div className="flex h-screen">
      <Sidebar
        sessions={sessions}
        activeSessionKey={sessionKey}
        collapsed={!sidebarOpen}
        activeView={activeView}
        navigate={navigate}
        onDeleteSession={handleDeleteSession}
        cronJobs={cronJobs.jobs}
        activeCronJobId={activeCronJobId}
      />
      <div className="flex-1 flex flex-col min-w-0">
        <TopBar
          title={activeView === 'chat' ? title : (selectedCronJob?.name || 'Cron Jobs')}
          defaultModel={defaultModel}
          models={models}
          model={model}
          onModelChange={handleModelChange}
          onToggleSidebar={toggleSidebar}
          onRename={activeView === 'chat' ? handleRename : undefined}
        />
        {activeView === 'chat' ? (
          <ChatArea
            messages={messages}
            isRunning={isRunning}
            status={status}
            showToolCalls={showToolCalls}
            isStreaming={isStreaming}
            streamText={streamText}
            toolActivity={toolActivity}
            onSend={handleSend}
            onAbort={abortRun}
            onToggleTools={toggleTools}
          />
        ) : (
          <CronArea
            job={selectedCronJob}
            creating={creatingCronJob}
            models={models}
            onLoad={cronJobs.loadJobs}
            onCreate={handleCreateCronJob}
            onUpdate={cronJobs.updateJob}
            onDelete={handleDeleteCronJob}
            onTrigger={cronJobs.triggerJob}
            onCancelCreate={() => navigate('/crons')}
            onViewSession={(key) => navigate(`/chat/${key}`)}
          />
        )}
      </div>
    </div>
  );
}
