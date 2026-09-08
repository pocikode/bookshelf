/**
 * The personal server authenticates with an opaque HttpOnly session cookie and
 * never issues a JWT. Upstream, however, treats `AuthContext.token` as a
 * Supabase access token and decodes it without guarding: every helper in
 * `utils/access.ts` calls `jwtDecode(token)`, reached on the library route via
 * `useQuotaStats`, which only checks that the token is truthy.
 *
 * Storing a bare sentinel such as `'personal-session'` therefore threw
 * `Invalid token specified: missing part #2` — `jwtDecode` splits on `.` and
 * finds no payload segment. The `|| {}` fallbacks upstream do not help because
 * `jwtDecode` throws rather than returning a falsy value.
 *
 * So the placeholder is shaped like an unsigned JWT: a real payload segment
 * that decodes to the free-plan claims those helpers expect. It is never sent
 * to the personal server (cookies carry auth) and never verified anywhere, so
 * the empty signature is fine — it exists purely to satisfy the decoders.
 *
 * Decodes to `{ plan: 'free', storage_usage_bytes: 0, storage_purchased_bytes: 0 }`.
 */
export const PERSONAL_SESSION_TOKEN =
  'eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.' +
  'eyJwbGFuIjoiZnJlZSIsInN0b3JhZ2VfdXNhZ2VfYnl0ZXMiOjAsInN0b3JhZ2VfcHVyY2hhc2VkX2J5dGVzIjowfQ.';
