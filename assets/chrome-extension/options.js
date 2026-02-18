const DEFAULT_URL = 'http://127.0.0.1:8833'

function normalizeUrl(value) {
  const trimmed = (value || '').trim().replace(/\/+$/, '')
  if (!trimmed) return DEFAULT_URL
  return trimmed
}

function setStatus(kind, message) {
  const status = document.getElementById('status')
  if (!status) return
  status.dataset.kind = kind || ''
  status.textContent = message || ''
}

async function checkRelayReachable(baseUrl) {
  const url = `${baseUrl}/api/v1/health`
  const ctrl = new AbortController()
  const t = setTimeout(() => ctrl.abort(), 900)
  try {
    const res = await fetch(url, { method: 'HEAD', signal: ctrl.signal })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    setStatus('ok', `Relay reachable at ${baseUrl}`)
  } catch {
    setStatus(
      'error',
      `Relay not reachable at ${baseUrl}. Make sure the TeaNode gateway is running at that address.`,
    )
  } finally {
    clearTimeout(t)
  }
}

async function load() {
  const stored = await chrome.storage.local.get(['relayUrl', 'relayToken'])
  const url = normalizeUrl(stored.relayUrl)
  document.getElementById('url').value = url
  document.getElementById('token').value = stored.relayToken || ''
  await checkRelayReachable(url)
}

async function save() {
  const input = document.getElementById('url')
  const url = normalizeUrl(input.value)
  await chrome.storage.local.set({ relayUrl: url })
  input.value = url
  await checkRelayReachable(url)
}

async function saveToken() {
  const input = document.getElementById('token')
  const token = (input.value || '').trim()
  await chrome.storage.local.set({ relayToken: token })
}

document.getElementById('save').addEventListener('click', () => void save())
document.getElementById('save-token').addEventListener('click', () => void saveToken())
void load()
