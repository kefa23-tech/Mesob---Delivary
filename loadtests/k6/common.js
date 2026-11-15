export const API = (__ENV.API_BASE_URL || 'http://localhost:8080/api/v1').replace(/\/$/, '');
const tokensFile = __ENV.AUTH_TOKENS_FILE;
export const tokens = tokensFile ? JSON.parse(open(tokensFile)) : [];

export function authHeaders(index) {
  if (tokens.length === 0) throw new Error('AUTH_TOKENS_FILE must contain a JSON array of bearer tokens');
  return { Authorization: `Bearer ${tokens[index % tokens.length]}`, 'Content-Type': 'application/json' };
}

export function requireTokens(count) {
  if (tokens.length < count) throw new Error(`AUTH_TOKENS_FILE must contain at least ${count} distinct tokens`);
}

export function required(name) {
  if (!__ENV[name]) throw new Error(`${name} is required`);
  return __ENV[name];
}
