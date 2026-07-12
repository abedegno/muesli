import { useEffect, useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Building2, Users } from 'lucide-react'
import { muesli } from '@/api'
import { EmptyState } from './EmptyState'
import { MonogramAvatar } from './MonogramAvatar'
import { Input } from '@/components/ui/Input'
import { Skeleton } from '@/components/ui/Skeleton'
import type { CompanyWithCount, PersonWithCompany } from '../../shared/types'

type Tab = 'people' | 'companies'
type SortMode = 'name' | 'recent'

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

function sortByName(a: string, b: string): number {
  return a.localeCompare(b, undefined, { sensitivity: 'base' })
}

function personSortLabel(person: PersonWithCompany): string {
  return person.display_name || person.primary_email || 'Untitled person'
}

function companySortLabel(company: CompanyWithCount): string {
  return company.name || company.domain || 'Untitled company'
}

function sortPeople(items: PersonWithCompany[], sortMode: SortMode): PersonWithCompany[] {
  return [...items].sort((a, b) => {
    if (sortMode === 'recent') {
      const recent = new Date(b.first_seen_at).getTime() - new Date(a.first_seen_at).getTime()
      if (recent !== 0) return recent
    }

    return sortByName(personSortLabel(a), personSortLabel(b))
  })
}

function sortCompanies(items: CompanyWithCount[], sortMode: SortMode): CompanyWithCount[] {
  return [...items].sort((a, b) => {
    if (sortMode === 'recent') {
      const recent = new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      if (recent !== 0) return recent
    }

    return sortByName(companySortLabel(a), companySortLabel(b))
  })
}

function PeopleRow({ person, onOpen }: { person: PersonWithCompany; onOpen: () => void }) {
  return (
    <li className="flex items-center justify-between gap-4 rounded-[var(--radius)] border border-border px-3 py-2">
      <button type="button" onClick={onOpen} className="flex min-w-0 flex-1 items-center gap-3 text-left">
        <MonogramAvatar id={person.id} label={person.display_name || person.primary_email} className="h-8 w-8 text-sm" />
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{person.display_name || 'Untitled person'}</p>
          <p className="truncate text-xs text-muted-foreground">{person.primary_email}</p>
        </div>
      </button>
      {person.company ? (
        <Link
          to={`/companies/${person.company.id}`}
          className="shrink-0 truncate text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          {person.company.name || person.company.domain}
        </Link>
      ) : (
        <p className="shrink-0 truncate text-sm text-muted-foreground">—</p>
      )}
    </li>
  )
}

function CompanyRow({ company, onOpen }: { company: CompanyWithCount; onOpen: () => void }) {
  return (
    <li className="flex items-center justify-between gap-4 rounded-[var(--radius)] border border-border px-3 py-2">
      <button type="button" onClick={onOpen} className="flex min-w-0 flex-1 items-center gap-3 text-left">
        <MonogramAvatar id={company.id} label={company.name || company.domain} className="h-8 w-8 text-sm" />
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{company.name || 'Untitled company'}</p>
          <p className="truncate text-xs text-muted-foreground">{company.domain}</p>
        </div>
      </button>
      <p className="shrink-0 text-sm text-muted-foreground">{company.people_count}</p>
    </li>
  )
}

export function PeopleScreen() {
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useState<Tab>('people')
  const [sortMode, setSortMode] = useState<SortMode>('name')
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
  const activeLoading = activeTab === 'people' ? isPeopleLoading : isCompaniesLoading
  const sortedPeople = people ? sortPeople(people, sortMode) : null
  const sortedCompanies = companies ? sortCompanies(companies, sortMode) : null
  const filteredPeople = normalizedQuery
    ? sortedPeople?.filter((person) => {
        const displayName = person.display_name.toLowerCase()
        const primaryEmail = person.primary_email.toLowerCase()
        return displayName.includes(normalizedQuery) || primaryEmail.includes(normalizedQuery)
      }) ?? null
    : sortedPeople
  const filteredCompanies = normalizedQuery
    ? sortedCompanies?.filter((company) => {
        const name = company.name.toLowerCase()
        const domain = company.domain.toLowerCase()
        return name.includes(normalizedQuery) || domain.includes(normalizedQuery)
      }) ?? null
    : sortedCompanies
  const activeFilteredItems = activeTab === 'people' ? filteredPeople : filteredCompanies
  const activeItems = activeTab === 'people' ? people : companies
  const isEmpty = activeItems !== null && activeItems.length === 0 && activeError === null
  const hasNoMatches = normalizedQuery.length > 0 && activeFilteredItems !== null && activeFilteredItems.length === 0 && activeError === null
  const countsLabel = people !== null && companies !== null ? `${people.length} people · ${companies.length} companies` : null

  return (
    <div className="mx-auto max-w-3xl p-6">
      <div className="mb-4 flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="font-serif text-xl font-semibold">People</h1>
          <p className="text-sm text-muted-foreground">Browse people and companies from your workspace.</p>
          {countsLabel ? <p className="text-sm text-muted-foreground">{countsLabel}</p> : null}
        </div>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <span className="whitespace-nowrap text-xs font-medium uppercase tracking-wide">Sort</span>
          <select
            aria-label="Sort people and companies"
            value={sortMode}
            onChange={(e) => setSortMode(e.target.value as SortMode)}
            className="h-9 rounded-[var(--radius)] border border-input bg-background px-2 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <option value="name">Name</option>
            <option value="recent">Most recent</option>
          </select>
        </label>
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
