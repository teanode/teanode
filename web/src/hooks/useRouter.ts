import { useState, useEffect, useCallback } from 'react';

export type Route =
  | { page: 'chat'; sessionKey: string | null }
  | { page: 'crons'; jobId: string | null; creating: boolean };

function parseHash(hash: string): Route {
  const path = hash.replace(/^#\/?/, '');
  const parts = path.split('/').filter(Boolean);

  if (parts[0] === 'crons') {
    if (parts[1] === 'new') return { page: 'crons', jobId: null, creating: true };
    if (parts[1]) return { page: 'crons', jobId: parts[1], creating: false };
    return { page: 'crons', jobId: null, creating: false };
  }

  // Default: chat
  if (parts[0] === 'chat' && parts[1]) return { page: 'chat', sessionKey: parts[1] };
  return { page: 'chat', sessionKey: null };
}

export function useRouter() {
  const [route, setRoute] = useState<Route>(() => parseHash(window.location.hash));

  useEffect(() => {
    function onHashChange() {
      setRoute(parseHash(window.location.hash));
    }
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  }, []);

  const navigate = useCallback((path: string) => {
    window.location.hash = path;
  }, []);

  return { route, navigate };
}
