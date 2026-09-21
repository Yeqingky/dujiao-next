const DEFAULT_AUTH_RETURN_PATH = '/me/orders'

// OIDC callbacks must only return to an internal SPA path. External URLs and
// protocol-relative URLs would turn a successful login into an open redirect.
export const normalizeAuthReturnPath = (
  value: unknown,
  fallback = DEFAULT_AUTH_RETURN_PATH,
): string => {
  const path = typeof value === 'string' ? value.trim() : ''
  if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\')) {
    return fallback
  }
  return path
}
