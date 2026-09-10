import { useState, type FormEvent } from 'react'
import { Lock } from 'lucide-react'
import { api, isApiError } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { photoCount } from '@/lib/format'

interface Props {
  slug: string
  name?: string
  count?: number
  onUnlocked: () => void
}

export default function PasswordGate({ slug, name, count, onUnlocked }: Props) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!password) return
    setBusy(true)
    setError(null)
    try {
      await api.unlock(slug, password)
      setPassword('')
      onUnlocked()
    } catch (err) {
      if (isApiError(err, 401)) setError('Wrong password.')
      else if (isApiError(err, 429)) setError('Too many attempts. Please wait a minute and try again.')
      else setError('Something went wrong. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto max-w-sm py-16">
      <div className="mb-6 flex flex-col items-center gap-3 text-center">
        <div className="rounded-full bg-muted p-3">
          <Lock className="size-6" aria-hidden="true" />
        </div>
        <h1 className="text-2xl font-semibold">{name ?? 'Protected album'}</h1>
        <p className="text-sm text-muted-foreground">
          {count !== undefined && <>{photoCount(count)} · </>}
          This album is password protected.
        </p>
      </div>
      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="album-password">Password</Label>
          <Input
            id="album-password"
            type="password"
            autoComplete="current-password"
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? 'album-password-error' : undefined}
          />
          {error && (
            <p id="album-password-error" role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
        </div>
        <Button type="submit" className="w-full" disabled={busy || !password}>
          {busy ? 'Unlocking…' : 'Unlock album'}
        </Button>
      </form>
    </div>
  )
}
