import React, { useCallback } from 'react';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useAppContext } from '../../context';
import CronArea from '../../components/CronArea';

/** /crons/$jobId — detail view. */
export default function CronDetailPage() {
  const { jobId } = useParams({ strict: false }) as { jobId: string };
  const { chat, cronJobs } = useAppContext();
  const navigate = useNavigate();

  const job = cronJobs.jobs.find((item) => item.id === jobId) || null;

  const handleDelete = useCallback(
    (id: string) => {
      return cronJobs.deleteJob(id).then(() => {
        navigate({ to: '/crons' });
      });
    },
    [cronJobs.deleteJob, navigate]
  );

  const defaultAgentId = chat.agents.length > 0 ? chat.agents[0].id : 'main';

  return (
    <CronArea
      job={job}
      creating={false}
      models={chat.models}
      agents={chat.agents}
      onLoad={cronJobs.loadJobs}
      onCreate={(params) => cronJobs.createJob(params)}
      onUpdate={cronJobs.updateJob}
      onDelete={handleDelete}
      onTrigger={cronJobs.triggerJob}
      onCancelCreate={() => navigate({ to: '/crons' })}
      onViewSession={(key) =>
        navigate({ to: '/chat/$agentId/$sessionKey', params: { agentId: defaultAgentId, sessionKey: key } })
      }
    />
  );
}
