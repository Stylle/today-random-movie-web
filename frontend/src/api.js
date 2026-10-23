export async function api(path, options = {}) {
  const headers = new Headers(options.headers || {})
  if (options.body && !(options.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, { credentials: 'same-origin', ...options, headers })
  const type = response.headers.get('content-type') || ''
  const data = type.includes('application/json') ? await response.json() : await response.text()
  if (!response.ok) {
    const error = new Error(data?.detail || data || `HTTP ${response.status}`)
    error.status = response.status
    throw error
  }
  return data
}

export const jsonBody = value => JSON.stringify(value)
