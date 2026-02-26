const DEFAULT_URL = 'http://127.0.0.1:8833'

function normalizeUrl(value) {
  const trimmed = (value || '').trim().replace(/\/+$/, '')
  if (!trimmed) return DEFAULT_URL
  return trimmed
}

function setStatus(elementId, kind, message) {
  const status = document.getElementById(elementId)
  if (!status) return
  status.dataset.kind = kind || ''
  status.textContent = message || ''
}

async function getRelayToken() {
  const stored = await chrome.storage.local.get(['relayToken'])
  return (stored.relayToken || '').trim()
}

async function checkRelayReachable(baseUrl) {
  const token = await getRelayToken()
  const headers = {}
  if (token) headers['Authorization'] = `Bearer ${token}`
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 2000)
  try {
    const response = await fetch(`${baseUrl}/api/v1/health`, { method: 'HEAD', signal: controller.signal, headers })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    setStatus('relay-status', 'ok', `Relay reachable at ${baseUrl}`)
  } catch {
    setStatus(
      'relay-status',
      'error',
      `Relay not reachable at ${baseUrl}. Make sure the TeaNode gateway is running at that address.`,
    )
  } finally {
    clearTimeout(timeout)
  }
}

async function checkTokenValidity(baseUrl) {
  const token = await getRelayToken()
  if (!token) {
    setStatus('token-status', '', '')
    return
  }
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 2000)
  try {
    const response = await fetch(`${baseUrl}/api/v1/auth/status`, {
      signal: controller.signal,
      headers: { Authorization: `Bearer ${token}` },
    })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    const data = await response.json()
    if (data.authenticated) {
      setStatus('token-status', 'ok', 'Token is valid')
    } else {
      setStatus('token-status', 'error', 'Token is not valid')
    }
  } catch {
    setStatus('token-status', 'error', 'Could not verify token')
  } finally {
    clearTimeout(timeout)
  }
}

async function checkAll(baseUrl) {
  await checkRelayReachable(baseUrl)
  await checkTokenValidity(baseUrl)
}

async function load() {
  const stored = await chrome.storage.local.get(['relayUrl', 'relayToken'])
  const url = normalizeUrl(stored.relayUrl)
  document.getElementById('url').value = url
  document.getElementById('token').value = stored.relayToken || ''
  await checkAll(url)
}

async function save() {
  const input = document.getElementById('url')
  const url = normalizeUrl(input.value)
  await chrome.storage.local.set({ relayUrl: url })
  input.value = url
  await checkAll(url)
}

async function saveToken() {
  const input = document.getElementById('token')
  const token = (input.value || '').trim()
  await chrome.storage.local.set({ relayToken: token })
  const url = normalizeUrl(document.getElementById('url').value)
  await checkTokenValidity(url)
}

document.getElementById('save').addEventListener('click', () => void save())
document.getElementById('save-token').addEventListener('click', () => void saveToken())
void load()
