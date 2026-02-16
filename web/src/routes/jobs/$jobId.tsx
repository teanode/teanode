import React from 'react';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useAppContext } from '../../context';
import JobArea from '../../components/JobArea';

/** /jobs/$jobId — detail view. */
export default function JobDetailPage() {
  const { jobId } = useParams({ strict: false }) as { jobId: string };
  const { backend } = useAppContext();
  const navigate = useNavigate();

  const job = backend.jobs.find((item) => item.id === jobId) || null;

  return (
    <JobArea
      job={job}
      creating={false}
      models={backend.models}
      agents={backend.agents}
      onLoad={backend.loadJobs}
      onCreate={(params) => backend.createJob(params)}
      onDelete={(id) => backend.deleteJob(id).then(() => { navigate({ to: '/jobs' }); })}
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
