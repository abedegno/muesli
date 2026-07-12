import { useEffect, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { Building2, Users } from 'lucide-react'
import { muesli } from '@/api'
import { EmptyState } from './EmptyState'
import { Input } from '@/components/ui/Input'
import { Skeleton } from '@/components/ui/Skeleton'
import type { CompanyWithCount, PersonWithCompany } from '../../shared/types'

type Tab = 'people' | 'companies'

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

function TabButton({
  active,
  onClick,
  icon,
  label,
}: {
  active: boolean
  onClick: () => void
  icon: ReactNode
  label: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={[
        'inline-flex items-center gap-2 rounded-[var(--radius)] border px-3 py-1.5 text-sm transition-colors',
        active
          ? 'border-primary bg-primary/10 text-foreground'
          : 'border-border text-muted-foreground hover:bg-muted hover:text-foreground',
      ].join(' ')}
    >
      {icon}
      {label}
    </button>
  )
}

function PeopleRow({ person, onOpen }: { person: PersonWithCompany; onOpen: () => void }) {
  return (
    <li className="flex items-start justify-between gap-4 rounded-[var(--radius)] border border-border px-3 py-2">
      <button type="button" onClick={onOpen} className="min-w-0 flex-1 text-left">
        <p className="truncate text-sm font-medium">{person.display_name || 'Untitled person'}</p>
        <p className="truncate text-xs text-muted-foreground">{person.primary_email}</p>
      </button>
      <p className="shrink-0 truncate text-sm text-muted-foreground">{person.company?.name ?? '—'}</p>
    </li>
  )
}

function CompanyRow({ company, onOpen }: { company: CompanyWithCount; onOpen: () => void }) {
  return (
    <li className="flex items-start justify-between gap-4 rounded-[var(--radius)] border border-border px-3 py-2">
      <button type="button" onClick={onOpen} className="min-w-0 flex-1 text-left">
        <p className="truncate text-sm font-medium">{company.name || 'Untitled company'}</p>
        <p className="truncate text-xs text-muted-foreground">{company.domain}</p>
      </button>
      <p className="shrink-0 text-sm text-muted-foreground">{company.people_count}</p>
    </li>
  )
}

export function PeopleScreen() {
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useState<Tab>('people')
  const [query, setQuery] = useState('')
  const [people, setPeople] = useState<PersonWithCompany[] | null>(null)
  const [companies, setCompanies] = useState<CompanyWithCount[] | null>(null)
  const [peopleError, setPeopleError] = useState<string | null>(null)
  const [companiesError, setCompaniesError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    void muesli.listPeople()
      .then((items) => {
        if (cancelled) return
        setPeople(items)
        setPeopleError(null)
      })
      .catch((err) => {
        if (cancelled) return
        setPeople([])
        setPeopleError(errorMessage(err))
      })

    void muesli.listCompanies()
      .then((items) => {
        if (cancelled) return
        setCompanies(items)
        setCompaniesError(null)
      })
      .catch((err) => {
        if (cancelled) return
        setCompanies([])
        setCompaniesError(errorMessage(err))
      })

    return () => {
      cancelled = true
    }
  }, [])

  const normalizedQuery = query.trim().toLowerCase()
  const isPeopleLoading = people === null && peopleError === null
  const isCompaniesLoading = companies === null && companiesError === null
  const activeError = activeTab === 'people' ? peopleError : companiesError
  const activeItems = activeTab === 'people' ? people : companies
  const activeLoading = activeTab === 'people' ? isPeopleLoading : isCompaniesLoading
  const filteredPeople = normalizedQuery
    ? people?.filter((person) => {
        const displayName = person.display_name.toLowerCase()
        const primaryEmail = person.primary_email.toLowerCase()
        return displayName.includes(normalizedQuery) || primaryEmail.includes(normalizedQuery)
      }) ?? null
    : people
  const filteredCompanies = normalizedQuery
    ? companies?.filter((company) => {
        const name = company.name.toLowerCase()
        const domain = company.domain.toLowerCase()
        return name.includes(normalizedQuery) || domain.includes(normalizedQuery)
      }) ?? null
    : companies
  const activeFilteredItems = activeTab === 'people' ? filteredPeople : filteredCompanies
  const isEmpty = activeItems !== null && activeItems.length === 0 && activeError === null
  const hasNoMatches = normalizedQuery.length > 0 && activeFilteredItems !== null && activeFilteredItems.length === 0 && activeError === null

  return (
    <div className="mx-auto max-w-3xl p-6">
      <div className="mb-4">
        <h1 className="mb-1 font-serif text-xl font-semibold">People</h1>
        <p className="text-sm text-muted-foreground">Browse people and companies from your workspace.</p>
      </div>

      <label htmlFor="people-search" className="mb-4 block">
        <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-muted-foreground">Search</span>
        <Input
          id="people-search"
          aria-label="Search people and companies"
          placeholder="Search people or companies"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </label>

      <div className="mb-4 flex items-center gap-2">
        <TabButton active={activeTab === 'people'} onClick={() => setActiveTab('people')} icon={<Users size={14} />} label="People" />
        <TabButton active={activeTab === 'companies'} onClick={() => setActiveTab('companies')} icon={<Building2 size={14} />} label="Companies" />
      </div>

      {activeLoading ? (
        <div className="flex flex-col gap-2">
          <div className="mb-2 h-7 w-40"><Skeleton className="h-full w-full" /></div>
          {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
        </div>
      ) : activeError ? (
        <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
          <p className="font-medium">Could not load {activeTab}</p>
          <p className="mt-1 break-words">{activeError}</p>
        </div>
      ) : hasNoMatches ? (
        <EmptyState
          title="No matches"
          hint={activeTab === 'people'
            ? 'Try a different search for people or email addresses.'
            : 'Try a different search for company names or domains.'}
        />
      ) : isEmpty ? (
        <EmptyState
          title={activeTab === 'people' ? 'No people yet' : 'No companies yet'}
          hint={activeTab === 'people'
            ? 'People synced from your server will appear here.'
            : 'Companies synced from your server will appear here.'}
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {activeTab === 'people'
            ? filteredPeople?.map((person) => <PeopleRow key={person.id} person={person} onOpen={() => navigate(`/people/${person.id}`)} />)
            : filteredCompanies?.map((company) => <CompanyRow key={company.id} company={company} onOpen={() => navigate(`/companies/${company.id}`)} />)}
        </ul>
      )}
    </div>
  )
}
