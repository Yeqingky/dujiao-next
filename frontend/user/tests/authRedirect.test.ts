import test from 'node:test'
import assert from 'node:assert/strict'
import { normalizeAuthReturnPath } from '../src/utils/authRedirect.ts'

test('OIDC return paths accept only internal SPA paths', () => {
  assert.equal(normalizeAuthReturnPath('/me/orders?tab=all'), '/me/orders?tab=all')
  assert.equal(normalizeAuthReturnPath('https://evil.example/steal'), '/me/orders')
  assert.equal(normalizeAuthReturnPath('//evil.example/steal'), '/me/orders')
  assert.equal(normalizeAuthReturnPath('/\\evil.example'), '/me/orders')
  assert.equal(normalizeAuthReturnPath('https://evil.example/steal', '/auth/login'), '/auth/login')
})
