import { useState, type FormEvent } from 'react'
import { login, register } from '../api'
import type { User } from '../types'

export function Auth({ onAuth }: { onAuth: (token: string, user: User) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const isRegister = mode === 'register'
  // Client-side hint only — the server stays the source of truth (min 6 chars).
  const passwordTooShort = isRegister && password.length > 0 && password.length < 6
  const canSubmit = username.trim().length > 0 && password.length > 0 && !passwordTooShort && !busy

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      const fn = isRegister ? register : login
      const { token, user } = await fn(username.trim(), password)
      onAuth(token, user)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'something went wrong')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="auth">
      <form className="auth-card" onSubmit={submit} noValidate>
        <div className="auth-brand">Opencord</div>
        <h1>{isRegister ? 'Create an account' : 'Welcome back!'}</h1>
        <p className="tagline">
          {isRegister
            ? 'Open-source chat you own — free, forever.'
            : "We're glad to see you again."}
        </p>

        <div className="auth-field">
          <label htmlFor="auth-username">Username</label>
          <input
            id="auth-username"
            placeholder="username"
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
          />
        </div>

        <div className="auth-field">
          <label htmlFor="auth-password">Password</label>
          <div className="auth-password">
            <input
              id="auth-password"
              type={showPassword ? 'text' : 'password'}
              placeholder="password"
              autoComplete={isRegister ? 'new-password' : 'current-password'}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <button
              type="button"
              className="link auth-reveal"
              aria-label={showPassword ? 'Hide password' : 'Show password'}
              aria-pressed={showPassword}
              onClick={() => setShowPassword((s) => !s)}
            >
              {showPassword ? 'Hide' : 'Show'}
            </button>
          </div>
          {passwordTooShort && (
            <span className="auth-hint">Password must be at least 6 characters.</span>
          )}
        </div>

        {error && (
          <div className="error" role="alert">
            {error}
          </div>
        )}

        <button
          className="auth-submit"
          aria-label={isRegister ? 'Create account' : 'Log in'}
          disabled={!canSubmit}
        >
          {busy ? (
            <span className="auth-spinner" aria-hidden="true" />
          ) : isRegister ? (
            'Create account'
          ) : (
            'Log in'
          )}
        </button>

        <button
          type="button"
          className="link auth-switch"
          onClick={() => {
            setMode(isRegister ? 'login' : 'register')
            setError('')
          }}
        >
          {isRegister ? 'Have an account? Log in' : 'No account? Register'}
        </button>
      </form>
    </div>
  )
}
