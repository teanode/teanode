import React, { useCallback } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useAppContext } from '../../context';
import CronArea from '../../components/CronArea';

/** /crons/new — create form. */
export default function CronsNewPage() {
  const { chat, cronJobs } = useAppContext();
  const navigate = useNavigate();

  const handleCreate = useCallback(
    (...args: Parameters<typeof cronJobs.createJob>) => {
      return cronJobs.createJob(...args).then(() => {
        navigate({ to: '/crons' });
      });
    },
    [cronJobs.createJob, navigate]
  );

  return (
    <CronArea
      job={null}
      creating={true}
      models={chat.models}
      agents={chat.agents}
      sessions={chat.sessions}
      onLoad={cronJobs.loadJobs}
      onCreate={handleCreate}
      onUpdate={cronJobs.updateJob}
      onDelete={async () => {}}
      onTrigger={cronJobs.triggerJob}
      onCancelCreate={() => navigate({ to: '/crons' })}
      onViewSession={(key) => {
        const defaultAgentId = chat.agents.length > 0 ? chat.agents[0].id : 'main';
        navigate({ to: '/chat/$agentId/$sessionKey', params: { agentId: defaultAgentId, sessionKey: key } });
      }}
    />
  );
}
