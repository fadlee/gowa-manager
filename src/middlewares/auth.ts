// Basic auth middleware
export const basicAuth = (username: string, password: string) => {
  return (context: any) => {
    const authHeader = context.request.headers.get('authorization')
    if (!authHeader) {
      return context.status(401)
    }

    const [authType, encodedCredentials] = authHeader.split(' ')

    if (authType !== 'Basic') {
      return context.status(401)
    }

    const credentials = atob(encodedCredentials)
    const [providedUsername, providedPassword] = credentials.split(':')

    if (providedUsername !== username || providedPassword !== password) {
      return context.status(401)
    }
  }
}
