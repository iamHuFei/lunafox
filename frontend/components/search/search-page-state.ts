"use client"

import * as React from "react"
import { useSearchParams } from "next/navigation"
import { useTranslations } from "next-intl"
import { readJsonStorage, writeJsonStorage } from "@/lib/browser-storage"
import {
  GLOBAL_ASSET_SEARCH_DEFAULT_PAGE_SIZE,
  composeGlobalAssetSearchQuery,
  decomposeGlobalAssetSearchQuery,
  getGlobalAssetSearchDiagnosticCode,
  getGlobalAssetSearchDiagnosticMessageKeys,
  isGlobalAssetSearchQueryValid,
  parseGlobalAssetSearchQuery,
  type GlobalAssetSearchDiagnosticCode,
  type GlobalAssetSearchOrgScope,
} from "@/lib/global-asset-search-query"
import { useAssetSearch } from "@/hooks/use-search"
import { useOrganizations } from "@/hooks/use-organizations"
import type { Organization } from "@/types/organization.types"
import type { AssetType, SearchParams, SearchState } from "@/types/search.types"

const RECENT_SEARCHES_KEY = "star_patrol_recent_searches"
const MAX_RECENT_SEARCHES = 5
/** The picker lists the first page of organizations; the tail surfaces a hint. */
const ORG_SCOPE_OPTIONS_PAGE_SIZE = 100

function getRecentSearches(): string[] {
  if (typeof window === "undefined") return []
  return readJsonStorage<string[]>(RECENT_SEARCHES_KEY, [])
}

function saveRecentSearch(query: string) {
  if (typeof window === "undefined" || !query.trim()) return
  const searches = getRecentSearches().filter((search) => search !== query)
  searches.unshift(query)
  writeJsonStorage(RECENT_SEARCHES_KEY, searches.slice(0, MAX_RECENT_SEARCHES))
}

function removeRecentSearch(query: string) {
  if (typeof window === "undefined") return
  writeJsonStorage(RECENT_SEARCHES_KEY, getRecentSearches().filter((search) => search !== query))
}

function getDiagnosticText(
  t: (key: string) => string,
  code: GlobalAssetSearchDiagnosticCode
) {
  const keys = getGlobalAssetSearchDiagnosticMessageKeys(code)
  return `${t(keys.message)} ${t(keys.fix)}`
}

function getQueryDiagnosticCode(query: string): GlobalAssetSearchDiagnosticCode | null {
  try {
    parseGlobalAssetSearchQuery(query)
    return null
  } catch (error) {
    return getGlobalAssetSearchDiagnosticCode(error)
  }
}

// scopeNotCombinable is a compose-level property of the draft plus the live
// scope, so refreshing it has to re-run the composition, not just the parser.
function getComposedQueryDiagnosticCode(query: string, scope: GlobalAssetSearchOrgScope): GlobalAssetSearchDiagnosticCode | null {
  try {
    composeGlobalAssetSearchQuery(query, scope)
    return null
  } catch (error) {
    return getGlobalAssetSearchDiagnosticCode(error)
  }
}

function isSameOrgScope(left: GlobalAssetSearchOrgScope, right: GlobalAssetSearchOrgScope): boolean {
  if (left.mode !== right.mode) return false
  if (left.mode === "organization" && right.mode === "organization") {
    return left.name === right.name
  }
  return true
}

export function useSearchPageState() {
  const t = useTranslations("search")
  const urlSearchParams = useSearchParams()
  // Keep the draft as entered for shareable search links; the query parser
  // trims user-facing contains terms before building the search predicate.
  const initialUrlQuery = urlSearchParams.get("q") ?? ""
  // Organization names are unknown on the first render, so a shared URL
  // scoped by an organization name starts verbatim under the all scope; the
  // reconciliation effect upgrades the picker once the list resolves.
  const initialDecomposition = decomposeGlobalAssetSearchQuery(initialUrlQuery, [])
  const initialQuery = initialDecomposition.text
  const initialOrgScope = initialDecomposition.scope
  const initialUrlQueryIsValid = !initialUrlQuery || isGlobalAssetSearchQueryValid(initialUrlQuery)
  const initialQueryDiagnosticCode = initialUrlQuery && !initialUrlQueryIsValid
    ? getQueryDiagnosticCode(initialUrlQuery)
    : null
  // Without a URL query the page opens on the unbound-targets default, so the
  // scope condition itself becomes the submitted query.
  const initialAutoScopeQuery = (() => {
    if (initialOrgScope.mode === "all") return ""
    const composed = composeGlobalAssetSearchQuery("", initialOrgScope)
    return composed.status === "query" ? composed.query : ""
  })()
  const initialSubmittedQuery = initialUrlQuery
    ? (initialUrlQueryIsValid ? initialUrlQuery : "")
    : initialAutoScopeQuery
  const initialSearchParams: SearchParams | undefined = initialSubmittedQuery
    ? { q: initialSubmittedQuery, assetType: "website", pageSize: GLOBAL_ASSET_SEARCH_DEFAULT_PAGE_SIZE }
    : undefined
  const [searchState, setSearchState] = React.useState<SearchState>(() => (
    initialSearchParams ? "searching" : "initial"
  ))
  const [query, setQueryState] = React.useState(initialQuery)
  const [assetType, setAssetType] = React.useState<AssetType>("website")
  const [orgScope, setOrgScope] = React.useState<GlobalAssetSearchOrgScope>(initialOrgScope)
  const [submittedQuery, setSubmittedQuery] = React.useState(initialSubmittedQuery)
  const [pageSize, setPageSize] = React.useState(GLOBAL_ASSET_SEARCH_DEFAULT_PAGE_SIZE)
  const [pageTokens, setPageTokens] = React.useState<Array<string | undefined>>([undefined])
  const [pageIndex, setPageIndex] = React.useState(0)
  const [organizationSearchRevision, setOrganizationSearchRevision] = React.useState(0)
  const [recentSearches, setRecentSearches] = React.useState<string[]>([])
  const [queryDiagnosticCode, setQueryDiagnosticCode] = React.useState<GlobalAssetSearchDiagnosticCode | null>(initialQueryDiagnosticCode)
  const lastUrlQueryRef = React.useRef(initialUrlQuery)
  const userInteractedRef = React.useRef(false)
  const orgOptionsListReconciledRef = React.useRef(false)

  const orgOptionsQuery = useOrganizations({ pageSize: ORG_SCOPE_OPTIONS_PAGE_SIZE, orderBy: "displayName asc" })
  const orgOptions = React.useMemo<Organization[]>(
    () => orgOptionsQuery.data?.organizations ?? [],
    [orgOptionsQuery.data]
  )
  const orgOptionsHasMore = Boolean(orgOptionsQuery.data?.nextPageToken)
  const knownOrgNames = React.useMemo(
    () => orgOptions.map((organization) => organization.name),
    [orgOptions]
  )

  const setQuery = React.useCallback((nextQuery: React.SetStateAction<string>) => {
    setQueryState(nextQuery)
  }, [])

  const queryError = queryDiagnosticCode ? getDiagnosticText(t, queryDiagnosticCode) : null

  React.useEffect(() => {
    setRecentSearches(getRecentSearches())
  }, [])

  const beginSearch = React.useCallback((nextQuery: string, nextAssetType: AssetType, nextPageSize = pageSize, nextScope = orgScope) => {
    let composed: ReturnType<typeof composeGlobalAssetSearchQuery>
    try {
      composed = composeGlobalAssetSearchQuery(nextQuery, nextScope)
    } catch (error) {
      setQueryDiagnosticCode(getGlobalAssetSearchDiagnosticCode(error))
      return false
    }
    if (composed.status === "none") {
      setQueryDiagnosticCode("required")
      return false
    }

    userInteractedRef.current = true
    // composeGlobalAssetSearchQuery re-parses its output, so the submitted
    // query is guaranteed valid here and the parse only inspects the shape.
    const parsedSubmitted = parseGlobalAssetSearchQuery(composed.query)
    if (parsedSubmitted.mode === "structured" && parsedSubmitted.conditions.some((condition) => condition.field === "organization")) {
      setOrganizationSearchRevision((current) => current + 1)
    }
    // A draft that carries its own organization condition wins over the
    // picker; the picker resets to the all scope instead of re-scoping.
    if (composed.takeover) {
      setOrgScope({ mode: "all" })
    }

    setQueryState(nextQuery)
    setAssetType(nextAssetType)
    setSubmittedQuery(composed.query)
    setPageSize(nextPageSize)
    setPageTokens([undefined])
    setPageIndex(0)
    setQueryDiagnosticCode(null)
    setSearchState("searching")
    saveRecentSearch(nextQuery)
    setRecentSearches(getRecentSearches())
    return true
  }, [orgScope, pageSize])

  React.useEffect(() => {
    if (!queryDiagnosticCode) return
    const nextCode = queryDiagnosticCode === "scopeNotCombinable"
      ? getComposedQueryDiagnosticCode(query, orgScope)
      : getQueryDiagnosticCode(query)
    setQueryDiagnosticCode((current) => current === nextCode ? current : nextCode)
  }, [orgScope, query, queryDiagnosticCode])

  React.useEffect(() => {
    const nextQuery = urlSearchParams.get("q") ?? ""
    if (nextQuery === lastUrlQueryRef.current) return
    lastUrlQueryRef.current = nextQuery
    userInteractedRef.current = true
    setQueryState(nextQuery)

    if (!nextQuery) {
      if (orgScope.mode === "all") {
        setSubmittedQuery("")
        setPageTokens([undefined])
        setPageIndex(0)
        setSearchState("initial")
        setQueryDiagnosticCode(null)
        return
      }
      beginSearch("", "website", pageSize, orgScope)
      return
    }
    const nextCode = getQueryDiagnosticCode(nextQuery)
    if (nextCode) {
      setSubmittedQuery("")
      setPageTokens([undefined])
      setPageIndex(0)
      setSearchState("initial")
      setQueryDiagnosticCode(nextCode)
      return
    }
    const decomposition = decomposeGlobalAssetSearchQuery(nextQuery, knownOrgNames)
    setOrgScope(decomposition.scope)
    beginSearch(decomposition.text, "website", pageSize, decomposition.scope)
  }, [beginSearch, knownOrgNames, orgScope, pageSize, urlSearchParams])

  // A shared URL scoped by organization loads before the organization list
  // resolves; once the names first arrive, upgrade the picker and the draft
  // to the decomposed form without disturbing the already submitted query.
  // The latch keeps this to a single pass: re-running it on later draft edits
  // would roll the user's draft back to the URL decomposition.
  React.useEffect(() => {
    if (orgOptionsListReconciledRef.current || orgOptionsQuery.data === undefined) return
    orgOptionsListReconciledRef.current = true
    if (userInteractedRef.current) return
    const reconciled = decomposeGlobalAssetSearchQuery(initialUrlQuery, knownOrgNames)
    setOrgScope(reconciled.scope)
    setQueryState(reconciled.text)
  }, [initialUrlQuery, knownOrgNames, orgOptionsQuery.data])

  const activeSearchParams = React.useMemo<SearchParams | undefined>(() => {
    if (!submittedQuery) return undefined
    const pageToken = pageTokens[pageIndex]
    return {
      q: submittedQuery,
      assetType,
      pageSize,
      ...(pageToken ? { pageToken } : {}),
    }
  }, [assetType, pageIndex, pageSize, pageTokens, submittedQuery])

  const searchEnabled = searchState === "searching" || searchState === "results"
  const submittedQueryHasOrganization = React.useMemo(() => {
    if (!submittedQuery) return false
    try {
      const parsed = parseGlobalAssetSearchQuery(submittedQuery)
      return parsed.mode === "structured" && parsed.conditions.some((condition) => condition.field === "organization")
    } catch {
      return false
    }
  }, [submittedQuery])
  const searchOptions = submittedQueryHasOrganization && organizationSearchRevision > 0
    ? { enabled: searchEnabled, requestNonce: organizationSearchRevision }
    : { enabled: searchEnabled }
  const { data, isLoading, error, isFetching, refetch } = useAssetSearch(activeSearchParams, searchOptions)

  const tPagination = useTranslations("common.pagination")
  // The capped summary is display-only: navigation stays token-driven, so the
  // summary never authorizes a page transition.
  const paginationSummary = React.useMemo(() => {
    if (data?.totalSize === undefined) return undefined
    return data.totalSizeCapped
      ? tPagination("totalCapped", { count: data.totalSize })
      : tPagination("total", { count: data.totalSize })
  }, [data?.totalSize, data?.totalSizeCapped, tPagination])

  React.useEffect(() => {
    if (searchState === "searching" && !isLoading && (data || error)) {
      setSearchState("results")
    }
  }, [data, error, isLoading, searchState])

  const handleSearch = React.useCallback(() => {
    beginSearch(query, assetType)
  }, [assetType, beginSearch, query])

  const handleQuickTagClick = React.useCallback((tagQuery: string) => {
    setQuery(tagQuery)
  }, [setQuery])

  const handleRecentSearchClick = React.useCallback((recentQuery: string) => {
    setQuery(recentQuery)
  }, [setQuery])

  const handleRemoveRecentSearch = React.useCallback((event: React.MouseEvent, searchQuery: string) => {
    event.stopPropagation()
    removeRecentSearch(searchQuery)
    setRecentSearches(getRecentSearches())
  }, [])

  const handleOrgScopeChange = React.useCallback((nextScope: GlobalAssetSearchOrgScope) => {
    if (isSameOrgScope(nextScope, orgScope)) return
    userInteractedRef.current = true
    setOrgScope(nextScope)
    // Selecting the all scope with an empty draft returns to the guided
    // initial shell; every other scope lists its full result set directly.
    if (nextScope.mode === "all" && !query.trim()) {
      setSubmittedQuery("")
      setPageTokens([undefined])
      setPageIndex(0)
      setSearchState("initial")
      setQueryDiagnosticCode(null)
      return
    }
    beginSearch(query, assetType, pageSize, nextScope)
  }, [assetType, beginSearch, orgScope, pageSize, query])

  const handleAssetTypeChange = React.useCallback((value: AssetType) => {
    if (value === assetType) return
    if (submittedQuery) {
      beginSearch(query, value)
      return
    }
    setAssetType(value)
    setPageTokens([undefined])
    setPageIndex(0)
  }, [assetType, beginSearch, query, submittedQuery])

  const handlePageSizeChange = React.useCallback((nextPageSize: number) => {
    if (nextPageSize === pageSize) return
    if (submittedQuery) {
      beginSearch(query, assetType, nextPageSize)
      return
    }
    setPageSize(nextPageSize)
    setPageTokens([undefined])
    setPageIndex(0)
  }, [assetType, beginSearch, pageSize, query, submittedQuery])

  const handlePreviousPage = React.useCallback(() => {
    if (pageIndex === 0) return
    setPageIndex((current) => current - 1)
    setSearchState("searching")
  }, [pageIndex])

  const handleFirstPage = React.useCallback(() => {
    if (pageIndex === 0) return
    setPageTokens([undefined])
    setPageIndex(0)
    setSearchState("searching")
  }, [pageIndex])

  const handleNextPage = React.useCallback(() => {
    const nextPageToken = data?.nextPageToken
    if (!nextPageToken) return
    setPageTokens((current) => [...current.slice(0, pageIndex + 1), nextPageToken])
    setPageIndex((current) => current + 1)
    setSearchState("searching")
  }, [data?.nextPageToken, pageIndex])

  return {
    t,
    searchState,
    query,
    submittedQuery,
    assetType,
    orgScope,
    orgOptions,
    orgOptionsLoading: orgOptionsQuery.isLoading,
    orgOptionsHasMore,
    pageSize,
    recentSearches,
    queryError,
    queryDiagnosticCode,
    data,
    isLoading,
    isFetching,
    error,
    refetch,
    canFirstPage: pageIndex > 0,
    canPreviousPage: pageIndex > 0,
    canNextPage: Boolean(data?.nextPageToken),
    paginationSummary,
    setQuery,
    handleSearch,
    handleQuickTagClick,
    handleRecentSearchClick,
    handleRemoveRecentSearch,
    handleOrgScopeChange,
    handleAssetTypeChange,
    handlePageSizeChange,
    handlePreviousPage,
    handleFirstPage,
    handleNextPage,
  }
}

export type SearchPageState = ReturnType<typeof useSearchPageState>
