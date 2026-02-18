import { useNavigate } from '@tanstack/react-router';
import LoginPage from '../components/LoginPage';

function sanitizeNextUrl(next: string | null): string {
  if (!next || !next.startsWith('/') || next.startsWith('//')) return '/';
  return next;
}

export default function SetupRoute() {
  const navigate = useNavigate();

  function handleSuccess() {
    const params = new URLSearchParams(window.location.search);
    const next = sanitizeNextUrl(params.get('next'));
    navigate({ to: next });
  }

  return <LoginPage mode="setup" onSuccess={handleSuccess} />;
}
