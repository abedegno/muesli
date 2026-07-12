import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { EmptyState } from './EmptyState'
import { MonogramAvatar } from './MonogramAvatar'
import { Skeleton } from '@/components/ui/Skeleton'
import type { CompanyWithPeople, Person } from '../../shared/types'

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function ErrorBlock({ message, subject }: { message: string; subject: string }) {
  return (
    <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
      <p className="font-medium">Could not load {subject}</p>
      <p className="mt-1 break-words">{message}</p>
    </div>
  )
}

function PersonRow({ person }: { person: Person }) {
  return (
    <li>
      <Link
        to={`/people/${person.id}`}
        className="block rounded-[var(--radius)] border border-border px-3 py-2 transition-colors hover:bg-muted"
      >
        <p className="truncate text-sm font-medium">{person.display_name || 'Untitled person'}</p>
        <p className="truncate text-xs text-muted-foreground">{person.primary_email}</p>
      </Link>
    </li>
  )
}

export function CompanyDetailScreen() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [company, setCompany] = useState<CompanyWithPeople | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setCompany(null)
    setError(null)

    void muesli.getCompany(id)
      .then((loadedCompany) => {
        if (cancelled) return
        setCompany(loadedCompany)
      })
      .catch((err) => {
        if (cancelled) return
        setError(errorMessage(err))
        setCompany(null)
      })

    return () => {
      cancelled = true
    }
  }, [id])

  const loading = company === null && error === null
  const isEmpty = company !== null && company.people.length === 0 && error === null

  return (
    <div className="mx-auto max-w-3xl p-6">
      <div className="mb-4 flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={() => navigate('/people')} className="px-2">
          <ArrowLeft size={14} />
          Back to People
        </Button>
      </div>

      {loading ? (
        <div className="flex flex-col gap-2">
          <div className="mb-2 h-7 w-52"><Skeleton className="h-full w-full" /></div>
          <div className="h-5 w-64"><Skeleton className="h-full w-full" /></div>
          <div className="mt-2 flex flex-col gap-2">
            {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
          </div>
        </div>
      ) : error ? (
        <ErrorBlock message={error} subject="company" />
      ) : company ? (
        <div className="space-y-6">
          <section className="flex items-start gap-3">
            <MonogramAvatar id={company.id} label={company.name || company.domain} />
            <div className="min-w-0 flex-1 space-y-2">
              <h1 className="font-serif text-xl font-semibold">{company.name || 'Untitled company'}</h1>
              <p className="truncate text-sm text-muted-foreground">{company.domain}</p>
            </div>
          </section>

          {isEmpty ? (
            <EmptyState title="No people yet" hint="No people are associated with this company." />
          ) : (
            <section>
              <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">People</h2>
              <ul className="flex flex-col gap-2">
                {company.people.map((person) => <PersonRow key={person.id} person={person} />)}
              </ul>
            </section>
          )}
        </div>
      ) : null}
    </div>
  )
}
