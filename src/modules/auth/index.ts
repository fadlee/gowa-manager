import { Elysia } from 'elysia'
import { basicAuth } from '../../middlewares/auth'
import { getConfig } from '../../cli'

// Get credentials from environment variables
const config = getConfig()
const ADMIN_USERNAME = config.adminUsername || process.env.ADMIN_USERNAME || 'admin'
const ADMIN_PASSWORD = config.adminPassword || process.env.ADMIN_PASSWORD || 'password'

export const authModule = new Elysia({ prefix: '/api/auth' })
  .post('/login', ({ set }) => {
    // In a real implementation, you might want to return a token
    // For basic auth, the client should store credentials themselves
    set.status = 200
    return {
      success: true,
      message: 'Login successful',
      user: ADMIN_USERNAME
    }
  }, {
    beforeHandle: basicAuth(ADMIN_USERNAME, ADMIN_PASSWORD)
  })
  .post('/logout', () => {
    // For basic auth, logout is handled client-side by clearing credentials
    return {
      success: true,
      message: 'Logout successful'
    }
  })
