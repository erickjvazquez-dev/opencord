import { useEffect, useState } from 'react'
import { Auth } from './components/Auth'
import { Chat } from './components/Chat'
import type { User } from './types'

const TOKEN_KEY = 'opencord.token'
const USER_KEY = 'opencord.user'

export function App() {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem(TOKEN_KEY))
  const [user, setUser] = useState<User | null>(() => {
    const raw = localStorage.getItem(USER_KEY)
    return raw ? (JSON.parse(raw) as User) : null
  })

  useEffect(() => {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  }, [token])

  useEffect(() => {
    if (user) localStorage.setItem(USER_KEY, JSON.stringify(user))
    else localStorage.removeItem(USER_KEY)
  }, [user])

  if (!token || !user) {
    return <Auth onAuth={(t, u) => { setToken(t); setUser(u) }} />
  }
  return <Chat token={token} user={user} onLogout={() => { setToken(null); setUser(null) }} />
}
