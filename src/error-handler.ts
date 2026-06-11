type ErrorContext = {
  code: string | number
  error: unknown
  set: {
    status?: number | string
    headers: Record<string, string | number>
  }
}

export function handleGlobalError({ code, error, set }: ErrorContext): { error: string; success: false } {
  console.error('Server error:', error)

  if (code === 'VALIDATION') {
    set.status = 400
    return { error: 'Validation failed', success: false }
  }

  if (code === 'UNAUTHORIZED') {
    set.status = 401
    set.headers['WWW-Authenticate'] = 'Basic realm="GOWA Manager"'
    return { error: 'Unauthorized', success: false }
  }

  if (code === 'NOT_FOUND') {
    set.status = 404
    return { error: 'Route not found', success: false }
  }

  set.status = 500
  return { error: 'Internal server error', success: false }
}
