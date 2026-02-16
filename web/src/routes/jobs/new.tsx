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
      return backend.createJob(...args).then((createdJob) => {
        navigate({ to: '/jobs/$jobId', params: { jobId: createdJob.id } });
        return createdJob;
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
      onLoad={backend.loadJobs}
      onCreate={handleCreate}
      onDelete={async () => {}}
      onUpdate={backend.updateJob}
      onTrigger={backend.triggerJob}
      onCancelCreate={() => navigate({ to: '/jobs' })}
      onViewAgentConversation={(agentId) => {
        const agent = backend.agents.find((candidate) => candidate.id === agentId);
        const conversationId = agent?.activeConversationId;
        if (conversationId) {
          navigate({ to: '/conversations/$agentId/$conversationId', params: { agentId, conversationId } });
        } else {
          navigate({ to: '/conversations/$agentId', params: { agentId } });
        }
      }}
    />
  );
}
