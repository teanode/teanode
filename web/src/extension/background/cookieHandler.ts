/** Cookie operations using chrome.cookies API (runs in background SW). */

export async function listCookies(args: {
  url?: string;
  domain?: string;
  name?: string;
}): Promise<chrome.cookies.Cookie[]> {
  const query: { url?: string; domain?: string; name?: string } = {};
  if (args.url) query.url = args.url;
  if (args.domain) query.domain = args.domain;
  if (args.name) query.name = args.name;
  const cookies = await chrome.cookies.getAll(query);
  // Cap at 200 entries.
  return cookies.slice(0, 200);
}

export async function getCookie(args: {
  url: string;
  name: string;
}): Promise<chrome.cookies.Cookie | null> {
  const result = await chrome.cookies.get({ url: args.url, name: args.name });
  return result ?? null;
}

export async function setCookie(args: {
  url: string;
  name: string;
  value: string;
  domain?: string;
  path?: string;
  secure?: boolean;
  httpOnly?: boolean;
  sameSite?: chrome.cookies.SameSiteStatus;
  expirationDate?: number;
}): Promise<chrome.cookies.Cookie | null> {
  const result = await chrome.cookies.set(args);
  return result ?? null;
}

export async function deleteCookie(args: {
  url: string;
  name: string;
}): Promise<{ url: string; name: string } | null> {
  const result = await chrome.cookies.remove(args);
  return result ?? null;
}
