import { useState, useCallback } from 'react';
import type { CronJob, CronJobCreateParams, CronJobUpdateParams, CronsListResult } from '../types';

export function useCronJobs(sendRpc: <T = unknown>(method: string, params: unknown) => Promise<T>) {
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [loading, setLoading] = useState(false);

  const loadJobs = useCallback(() => {
    setLoading(true);
    sendRpc<CronsListResult>('crons.list', {})
      .then((res) => setJobs(res.jobs || []))
      .catch((e) => console.error('crons.list:', e))
      .finally(() => setLoading(false));
  }, [sendRpc]);

  const createJob = useCallback(
    (params: CronJobCreateParams) => {
      return sendRpc<{ job: CronJob }>('crons.create', params)
        .then(() => { loadJobs(); })
        .catch((e) => { console.error('crons.create:', e); throw e; });
    },
    [sendRpc, loadJobs]
  );

  const updateJob = useCallback(
    (params: CronJobUpdateParams) => {
      return sendRpc<{ job: CronJob }>('crons.update', params)
        .then(() => { loadJobs(); })
        .catch((e) => { console.error('crons.update:', e); throw e; });
    },
    [sendRpc, loadJobs]
  );

  const deleteJob = useCallback(
    (id: string) => {
      return sendRpc('crons.delete', { id })
        .then(() => { loadJobs(); })
        .catch((e) => { console.error('crons.delete:', e); throw e; });
    },
    [sendRpc, loadJobs]
  );

  const triggerJob = useCallback(
    (id: string): Promise<void> => {
      return sendRpc('crons.trigger', { id })
        .then(() => {})
        .catch((e) => { console.error('crons.trigger:', e); throw e; });
    },
    [sendRpc]
  );

  return { jobs, loading, loadJobs, createJob, updateJob, deleteJob, triggerJob };
}
