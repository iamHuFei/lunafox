"use client"

import { semanticIcons } from "@/components/icons"
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select"
import type { GlobalAssetSearchOrgScope } from "@/lib/global-asset-search-query"
import type { SearchPageState } from "./search-page-state"

/** Sentinel option values; the org: prefix keeps names from colliding with them. */
const UNBOUND_SCOPE_VALUE = "unbound"
const ALL_SCOPE_VALUE = "all"
const ORGANIZATION_SCOPE_PREFIX = "org:"

function organizationScopeValue(name: string): string {
  return `${ORGANIZATION_SCOPE_PREFIX}${name}`
}

/**
 * Organization scope picker integrated into the left edge of the search bar.
 * The scope rides inside the submitted query as an organization condition, so
 * listings, pagination tokens, and exports share one code path.
 */
export function SearchOrgScopeSelect({ state }: { state: SearchPageState }) {
  const selectedValue = state.orgScope.mode === "organization"
    ? organizationScopeValue(state.orgScope.name)
    : state.orgScope.mode === "unbound" ? UNBOUND_SCOPE_VALUE : ALL_SCOPE_VALUE

  // Organization names are unique among active organizations; the dedupe only
  // guards against a page boundary delivering the same name twice.
  const listedOrganizations: string[] = []
  for (const organization of state.orgOptions) {
    if (!listedOrganizations.includes(organization.name)) {
      listedOrganizations.push(organization.name)
    }
  }
  // Keep the selected organization selectable even when it lives past the
  // first options page, so the trigger never shows a dangling scope.
  if (
    state.orgScope.mode === "organization" &&
    !listedOrganizations.includes(state.orgScope.name)
  ) {
    listedOrganizations.push(state.orgScope.name)
  }

  const triggerLabel = state.orgScope.mode === "organization"
    ? state.orgScope.name
    : state.orgScope.mode === "unbound" ? state.t("orgScope.unbound") : state.t("orgScope.all")

  const handleValueChange = (value: string) => {
    if (value === UNBOUND_SCOPE_VALUE) {
      state.handleOrgScopeChange({ mode: "unbound" })
      return
    }
    if (value === ALL_SCOPE_VALUE) {
      state.handleOrgScopeChange({ mode: "all" })
      return
    }
    if (value.startsWith(ORGANIZATION_SCOPE_PREFIX)) {
      const nextScope: GlobalAssetSearchOrgScope = { mode: "organization", name: value.slice(ORGANIZATION_SCOPE_PREFIX.length) }
      state.handleOrgScopeChange(nextScope)
    }
  }

  return (
    <Select value={selectedValue} onValueChange={handleValueChange}>
      <SelectTrigger
        aria-label={state.t("orgScope.label")}
        className="radius-none w-40 border-0 border-r bg-muted/30 px-4 text-sm shadow-none hover:bg-muted/50 focus-visible:z-10 focus-visible:ring-0"
      >
        <semanticIcons.concept.organization className="h-4 w-4 text-muted-foreground" />
        <span className="truncate">{triggerLabel}</span>
      </SelectTrigger>
      <SelectContent width="content-fit">
        <SelectItem value={UNBOUND_SCOPE_VALUE}>
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <semanticIcons.concept.target className="h-4 w-4 shrink-0 text-muted-foreground" />
            <span className="truncate">{state.t("orgScope.unbound")}</span>
          </span>
        </SelectItem>
        <SelectItem value={ALL_SCOPE_VALUE}>
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <semanticIcons.concept.organization className="h-4 w-4 shrink-0 text-muted-foreground" />
            <span className="truncate">{state.t("orgScope.all")}</span>
          </span>
        </SelectItem>
        {listedOrganizations.map((name) => (
          <SelectItem key={name} value={organizationScopeValue(name)}>
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <semanticIcons.concept.organization className="h-4 w-4 shrink-0 text-muted-foreground" />
              <span className="truncate">{name}</span>
            </span>
          </SelectItem>
        ))}
        {state.orgOptionsLoading && listedOrganizations.length === 0 ? (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">{state.t("orgScope.loading")}</div>
        ) : null}
        {state.orgOptionsHasMore ? (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">{state.t("orgScope.more")}</div>
        ) : null}
      </SelectContent>
    </Select>
  )
}
