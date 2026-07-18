import { createHmac, randomBytes, timingSafeEqual } from 'node:crypto'

const TOKEN_TTL_SECONDS = 60
const COOKIE_TTL_SECONDS = 15 * 60
const COOKIE_PREFIX = 'gowa_admin_auth_'

interface MagicAdminTokenPayload {
  instanceKey: string
  exp: number
  nonce: string
}

function getSecret(): string {
  return process.env.ADMIN_LINK_SECRET || process.env.ADMIN_PASSWORD || 'gowa-manager-runtime-admin-link-secret'
}

function base64UrlEncode(value: string): string {
  return Buffer.from(value).toString('base64url')
}

function base64UrlDecode(value: string): string {
  return Buffer.from(value, 'base64url').toString('utf8')
}

function sign(payload: string): string {
  return createHmac('sha256', getSecret()).update(payload).digest('base64url')
}

function safeEqual(a: string, b: string): boolean {
  const left = Buffer.from(a)
  const right = Buffer.from(b)
  return left.length === right.length && timingSafeEqual(left, right)
}

function parseCookies(cookieHeader: string | undefined): Record<string, string> {
  if (!cookieHeader) return {}

  return cookieHeader.split(';').reduce<Record<string, string>>((cookies, part) => {
    const [rawName, ...rawValueParts] = part.trim().split('=')
    if (!rawName || rawValueParts.length === 0) return cookies
    cookies[rawName] = decodeURIComponent(rawValueParts.join('='))
    return cookies
  }, {})
}

function isHttps(requestUrl: string): boolean {
  try {
    return new URL(requestUrl).protocol === 'https:'
  } catch {
    return false
  }
}

export function getMagicAdminCookieName(instanceKey: string): string {
  return `${COOKIE_PREFIX}${instanceKey}`
}

export function createMagicAdminToken(instanceKey: string, now = Date.now()): { token: string; expiresAt: Date } {
  const expiresAt = new Date(now + TOKEN_TTL_SECONDS * 1000)
  const payload: MagicAdminTokenPayload = {
    instanceKey,
    exp: expiresAt.getTime(),
    nonce: randomBytes(12).toString('base64url'),
  }
  const encodedPayload = base64UrlEncode(JSON.stringify(payload))
  return {
    token: `${encodedPayload}.${sign(encodedPayload)}`,
    expiresAt,
  }
}

export function validateMagicAdminToken(token: string | undefined, instanceKey: string, now = Date.now()): boolean {
  if (!token) return false

  const [encodedPayload, signature] = token.split('.')
  if (!encodedPayload || !signature || !safeEqual(signature, sign(encodedPayload))) return false

  try {
    const payload = JSON.parse(base64UrlDecode(encodedPayload)) as MagicAdminTokenPayload
    return payload.instanceKey === instanceKey && typeof payload.exp === 'number' && payload.exp > now
  } catch {
    return false
  }
}

export function createMagicAdminCookie(instanceKey: string, token: string, requestUrl: string, maxAgeSeconds = COOKIE_TTL_SECONDS): string {
  const secure = isHttps(requestUrl) ? '; Secure' : ''
  return `${getMagicAdminCookieName(instanceKey)}=${encodeURIComponent(token)}; Max-Age=${maxAgeSeconds}; Path=/app/${instanceKey}; HttpOnly; SameSite=Lax${secure}`
}

export function clearMagicAdminCookie(instanceKey: string, requestUrl: string): string {
  const secure = isHttps(requestUrl) ? '; Secure' : ''
  return `${getMagicAdminCookieName(instanceKey)}=; Max-Age=0; Path=/app/${instanceKey}; HttpOnly; SameSite=Lax${secure}`
}

export function hasValidMagicAdminCookie(cookieHeader: string | undefined, instanceKey: string, now = Date.now()): boolean {
  const cookies = parseCookies(cookieHeader)
  return validateMagicAdminToken(cookies[getMagicAdminCookieName(instanceKey)], instanceKey, now)
}
