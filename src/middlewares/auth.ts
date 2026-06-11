import { timingSafeEqual } from 'node:crypto'

function timingSafeStringEqual(a: string, b: string): boolean {
  const aBuffer = Buffer.from(a)
  const bBuffer = Buffer.from(b)

  if (aBuffer.length !== bBuffer.length) {
    // Still compare equal-length buffers to reduce length-based timing differences.
    timingSafeEqual(aBuffer, aBuffer)
    timingSafeEqual(bBuffer, bBuffer)
    return false
  }

  return timingSafeEqual(aBuffer, bBuffer)
}

// Basic auth middleware
export const basicAuth = (username: string, password: string) => {
  return (context: any) => {
    const unauthorized = () => context.status(401)
    const authHeader = context.request.headers.get('authorization')
    if (!authHeader) {
      return unauthorized()
    }

    const [authType, encodedCredentials] = authHeader.split(' ')

    if (authType !== 'Basic' || !encodedCredentials) {
      return unauthorized()
    }

    let credentials: string
    try {
      credentials = atob(encodedCredentials)
    } catch {
      return unauthorized()
    }

    const separatorIndex = credentials.indexOf(':')
    if (separatorIndex === -1) {
      return unauthorized()
    }

    const providedUsername = credentials.slice(0, separatorIndex)
    const providedPassword = credentials.slice(separatorIndex + 1)

    if (!timingSafeStringEqual(providedUsername, username) || !timingSafeStringEqual(providedPassword, password)) {
      return unauthorized()
    }
  }
}
