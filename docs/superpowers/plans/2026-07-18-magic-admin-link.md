# Magic Admin Link Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users open GOWA admin through the proxy without manually entering instance Basic Auth credentials.

**Architecture:** Add a manager-protected admin-link endpoint that returns `/app/{key}/?autologin=<signed-token>`. The proxy validates that token, sets a short-lived HTTP-only cookie scoped to the instance proxy path, redirects without the token, and injects the instance's first Basic Auth header into proxied HTTP requests when that cookie is present.

**Tech Stack:** Bun, Elysia, TypeScript, React, TanStack Query-style API client patterns, Bun test runner.

---

## File Structure

- Create `src/modules/proxy/magic-auth.ts`: token signing/validation, cookie parsing, cookie names, and helper functions for magic auth.
- Modify `src/modules/proxy/auth-utils.ts`: add HTTP auth injection helper that respects incoming `Authorization` and validates magic cookie.
- Modify `src/modules/proxy/index.ts`: handle `autologin` query param before forwarding and call HTTP auth injection before `ProxyService.forwardRequest`.
- Modify `src/modules/proxy/auth-utils.test.ts`: cover magic cookie injection behavior.
- Modify `src/modules/proxy/routes.test.ts`: cover autologin redirect/cookie behavior and route order.
- Modify `src/modules/instances/model.ts`: add admin-link response schema.
- Modify `src/modules/instances/index.ts`: add `POST /api/instances/:id/admin-link`.
- Modify `src/modules/instances/routes.test.ts`: cover admin-link response with and without instance Basic Auth.
- Modify `client/src/types/index.ts`: add `AdminLinkResponse` type.
- Modify `client/src/lib/api.ts`: add `createAdminLink(id)`.
- Modify admin button callers: `client/src/components/InstanceCard.tsx`, `client/src/pages/InstanceDetailPage.tsx`, and `client/src/pages/DashboardPage.tsx`.

---

### Task 1: Magic auth utilities

**Files:**
- Create: `src/modules/proxy/magic-auth.ts`
- Modify: `src/modules/proxy/auth-utils.ts`
- Test: `src/modules/proxy/auth-utils.test.ts`

- [ ] Add tests for extracting the first Basic Auth header, not overriding explicit authorization, injecting auth with a valid cookie, and rejecting invalid/expired cookies.
- [ ] Implement `createMagicAdminToken(instanceKey, now?)`, `validateMagicAdminToken(token, instanceKey, now?)`, `getMagicAdminCookieName(instanceKey)`, `createMagicAdminCookie(instanceKey, requestUrl, maxAgeSeconds?)`, `clearMagicAdminCookie(instanceKey, requestUrl)`, and `hasValidMagicAdminCookie(cookieHeader, instanceKey, now?)`.
- [ ] Implement `applyInstanceHttpAuthHeader(headers, instance)` in `auth-utils.ts` so it only injects when no `authorization` exists and a valid magic cookie exists.
- [ ] Run `bun test src/modules/proxy/auth-utils.test.ts`.

### Task 2: Proxy autologin redirect and HTTP injection

**Files:**
- Modify: `src/modules/proxy/index.ts`
- Test: `src/modules/proxy/routes.test.ts`

- [ ] Add proxy route tests for `/app/:key?autologin=<token>` setting a cookie and redirecting to the same path without `autologin`.
- [ ] Add proxy route test for invalid autologin clearing cookie or returning 401.
- [ ] Add proxy route test proving proxied request with magic cookie forwards injected `Authorization`.
- [ ] Update `handleProxyRequest` to detect `autologin`, validate/set cookie/redirect before availability forwarding.
- [ ] Update header preparation in `handleProxyRequest` to apply HTTP magic auth injection before forwarding.
- [ ] Run `bun test src/modules/proxy/routes.test.ts src/modules/proxy/auth-utils.test.ts`.

### Task 3: Admin-link API route

**Files:**
- Modify: `src/modules/instances/model.ts`
- Modify: `src/modules/instances/index.ts`
- Test: `src/modules/instances/routes.test.ts`

- [ ] Add response schema `adminLinkResponse` with `url` and optional `expiresAt`.
- [ ] Add `POST /api/instances/:id/admin-link` route after devices or get route, requiring existing manager guard through module mounting.
- [ ] Route returns `/app/{key}/` with no `autologin` when no Basic Auth pair exists.
- [ ] Route returns `/app/{key}/?autologin=<token>` and ISO `expiresAt` when Basic Auth exists.
- [ ] Add route tests for missing instance, no basic auth, and basic auth.
- [ ] Run `bun test src/modules/instances/routes.test.ts`.

### Task 4: Frontend admin buttons

**Files:**
- Modify: `client/src/types/index.ts`
- Modify: `client/src/lib/api.ts`
- Modify: `client/src/components/InstanceCard.tsx`
- Modify: `client/src/pages/InstanceDetailPage.tsx`
- Modify: `client/src/pages/DashboardPage.tsx`

- [ ] Add `AdminLinkResponse` type.
- [ ] Add `apiClient.createAdminLink(id)`.
- [ ] Replace direct admin open in `InstanceCard` with async admin link creation and fallback to `getProxyUrl` on error.
- [ ] Replace direct admin open in `InstanceDetailPage` with async admin link creation and fallback.
- [ ] Replace dashboard Admin button direct `openUrl(proxyUrl)` with async admin link creation and fallback toast.
- [ ] Keep Copy URL and QR URL behavior unchanged.
- [ ] Run `bun run build:tsc`.

### Task 5: Final verification

**Files:**
- Verify all touched files.

- [ ] Run `bun test src/modules/proxy/auth-utils.test.ts src/modules/proxy/routes.test.ts src/modules/instances/routes.test.ts`.
- [ ] Run `bun run build:tsc`.
- [ ] Run `bun test`.
- [ ] Inspect `git diff` and ensure unrelated `src/version.ts` remains untouched/uncommitted.
