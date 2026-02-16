import React, { useCallback } from 'react';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useAppContext } from '../../context';
import JobArea from '../../components/JobArea';

/** /jobs/$jobId — detail view. */
export default function JobDetailPage() {
  const { jobId } = useParams({ strict: false }) as { jobId: string };
  const { backend } = useAppContext();
  const navigate = useNavigate();

  const job = backend.jobs.find((item) => item.id === jobId) || null;

  const handleDelete = useCallback(
    (id: string) => {
      return backend.deleteJob(id).then(() => {
        navigate({ to: '/jobs' });
      });
    },
    [backend.deleteJob, navigate]
  );

  const defaultAgentId = backend.agents.length > 0 ? backend.agents[0].id : 'main';

  return (
    <JobArea
      job={job}
      creating={false}
      models={backend.models}
      agents={backend.agents}
      conversations={backend.conversations}
      onLoad={backend.loadJobs}
      onCreate={(params) => backend.createJob(params)}
      onUpdate={backend.updateJob}
      onDelete={handleDelete}
      onTrigger={backend.triggerJob}
      onCancelCreate={() => navigate({ to: '/jobs' })}
      onViewConversation={(key) =>
        navigate({ to: '/conversations/$agentId/$conversationId', params: { agentId: defaultAgentId, conversationId: key } })
      }
    />
  );
}
