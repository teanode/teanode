import React, { useCallback } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useAppContext } from '../../context';
import JobArea from '../../components/JobArea';

/** /jobs/new — create form. */
export default function JobsNewPage() {
  const { backend } = useAppContext();
  const navigate = useNavigate();

  const handleCreate = useCallback(
    (...args: Parameters<typeof backend.createJob>) => {
      return backend.createJob(...args).then(() => {
        navigate({ to: '/jobs' });
      });
    },
    [backend.createJob, navigate]
  );

  return (
    <JobArea
      job={null}
      creating={true}
      models={backend.models}
      agents={backend.agents}
      conversations={backend.conversations}
      onLoad={backend.loadJobs}
      onCreate={handleCreate}
      onUpdate={backend.updateJob}
      onDelete={async () => {}}
      onTrigger={backend.triggerJob}
      onCancelCreate={() => navigate({ to: '/jobs' })}
      onViewConversation={(key) => {
        const defaultAgentId = backend.agents.length > 0 ? backend.agents[0].id : 'main';
        navigate({ to: '/conversations/$agentId/$conversationId', params: { agentId: defaultAgentId, conversationId: key } });
      }}
    />
  );
}
