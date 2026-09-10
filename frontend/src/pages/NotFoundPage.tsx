import { Link } from 'react-router'
import { Button } from '@/components/ui/button'

export default function NotFoundPage({ message = 'This page does not exist.' }: { message?: string }) {
  return (
    <div className="flex flex-col items-center gap-4 py-24 text-center">
      <h1 className="text-2xl font-semibold">Not found</h1>
      <p className="text-muted-foreground">{message}</p>
      <Button asChild variant="outline">
        <Link to="/">Back to albums</Link>
      </Button>
    </div>
  )
}
