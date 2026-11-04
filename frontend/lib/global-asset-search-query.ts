import type { AssetType } from "@/types/search.types"

export const GLOBAL_ASSET_SEARCH_DEFAULT_PAGE_SIZE = 10
export const GLOBAL_ASSET_SEARCH_MAX_PAGE_SIZE = 100
export const GLOBAL_ASSET_SEARCH_MAX_QUERY_BYTES = 2048
export const GLOBAL_ASSET_SEARCH_MAX_CONDITIONS = 10
export const GLOBAL_ASSET_SEARCH_MIN_CONTAINS_RUNES = 2
/** url, host, and title share this contains minimum. responseHeaders uses its own. */
export const GLOBAL_ASSET_SEARCH_MIN_URL_CONTAINS_RUNES = GLOBAL_ASSET_SEARCH_MIN_CONTAINS_RUNES
/** Two-character patterns cannot use a pg_trgm GIN index, so header contains starts at 3. */
export const GLOBAL_ASSET_SEARCH_MIN_RESPONSE_HEADER_CONTAINS_RUNES = 3

export type GlobalAssetSearchField = "url" | "host" | "title" | "statusCode" | "tech" | "hasScreenshot" | "responseHeaders" | "organization"
export type GlobalAssetSearchOperator = "=" | "=="
export type GlobalAssetSearchCombinator = "and" | "or"

export type GlobalAssetSearchDiagnosticCode =
  | "required"
  | "queryTooLong"
  | "invalidField"
  | "unsupportedField"
  | "invalidOperator"
  | "invalidStatusCode"
  | "invalidBoolean"
  | "containsValueTooShort"
  | "missingQuotedValue"
  | "invalidQuotedValue"
  | "unterminatedQuotedValue"
  | "invalidConnector"
  | "missingCondition"
  | "tooManyConditions"
  | "scopeNotCombinable"
  | "unknown"

export const GLOBAL_ASSET_SEARCH_FIELDS = ["url", "host", "title", "statusCode", "tech", "hasScreenshot", "responseHeaders", "organization"] as const satisfies readonly GlobalAssetSearchField[]

export const GLOBAL_ASSET_SEARCH_FIELD_LABEL_KEYS = {
  url: "fields.url",
  host: "fields.host",
  title: "fields.title",
  statusCode: "fields.status",
  tech: "fields.tech",
  hasScreenshot: "fields.hasScreenshot",
  responseHeaders: "fields.responseHeaders",
  organization: "fields.organization",
} as const satisfies Record<GlobalAssetSearchField, string>

export const GLOBAL_ASSET_SEARCH_OPERATORS = [
  { value: "=", labelKey: "syntax.contains" },
  { value: "==", labelKey: "syntax.exact" },
  { value: "&&", labelKey: "syntax.all" },
  { value: "||", labelKey: "syntax.any" },
] as const

export type GlobalAssetSearchGuidanceCategory = "basics" | "scenarios"

export interface GlobalAssetSearchGuidanceEntry {
  id: string
  category: GlobalAssetSearchGuidanceCategory
  query: string
  labelKey: string
  descriptionKey: string
  featured?: boolean
}

export const GLOBAL_ASSET_SEARCH_GUIDANCE_CATEGORIES = [
  { id: "basics", titleKey: "syntax.categories.basics" },
  { id: "scenarios", titleKey: "syntax.categories.scenarios" },
] as const

export interface GlobalAssetSearchCondition {
  field: GlobalAssetSearchField
  operator: GlobalAssetSearchOperator
  value: string | number
}

export type GlobalAssetSearchQuery =
  | { mode: "plainUrl"; value: string }
  | { mode: "structured"; combinator: GlobalAssetSearchCombinator; conditions: GlobalAssetSearchCondition[] }

export class GlobalAssetSearchQueryError extends Error {
  readonly code: GlobalAssetSearchDiagnosticCode

  constructor(message: string, code: GlobalAssetSearchDiagnosticCode = "unknown") {
    super(message)
    this.name = "GlobalAssetSearchQueryError"
    this.code = code
  }
}

const GLOBAL_ASSET_SEARCH_DIAGNOSTIC_CODES = new Set<GlobalAssetSearchDiagnosticCode>([
  "required",
  "queryTooLong",
  "invalidField",
  "unsupportedField",
  "invalidOperator",
  "invalidStatusCode",
  "invalidBoolean",
  "containsValueTooShort",
  "missingQuotedValue",
  "invalidQuotedValue",
  "unterminatedQuotedValue",
  "invalidConnector",
  "missingCondition",
  "tooManyConditions",
  "scopeNotCombinable",
  "unknown",
])

export function getGlobalAssetSearchDiagnosticCode(error: unknown): GlobalAssetSearchDiagnosticCode {
  if (error instanceof GlobalAssetSearchQueryError && GLOBAL_ASSET_SEARCH_DIAGNOSTIC_CODES.has(error.code)) {
    return error.code
  }
  return "unknown"
}

export function getGlobalAssetSearchDiagnosticMessageKeys(code: GlobalAssetSearchDiagnosticCode) {
  return {
    message: `diagnostics.${code}.message`,
    fix: `diagnostics.${code}.fix`,
  } as const
}

const textEncoder = new TextEncoder()
const allowedFields = new Set<GlobalAssetSearchField>(GLOBAL_ASSET_SEARCH_FIELDS)

const RAW_GLOBAL_ASSET_SEARCH_GUIDANCE: readonly GlobalAssetSearchGuidanceEntry[] = [
  { id: "url-contains", category: "basics", query: 'url="admin"', labelKey: "syntax.exampleLabels.urlContains", descriptionKey: "syntax.exampleDescriptions.urlContains", featured: true },
  { id: "host-contains", category: "basics", query: 'host="api"', labelKey: "syntax.exampleLabels.hostContains", descriptionKey: "syntax.exampleDescriptions.hostContains", featured: true },
  { id: "title-contains", category: "basics", query: 'title="Login"', labelKey: "syntax.exampleLabels.titleContains", descriptionKey: "syntax.exampleDescriptions.titleContains", featured: true },
  { id: "status-exact", category: "basics", query: 'statusCode=="200"', labelKey: "syntax.exampleLabels.statusExact", descriptionKey: "syntax.exampleDescriptions.statusExact", featured: true },
  { id: "tech-exact-nginx", category: "basics", query: 'tech="nginx"', labelKey: "syntax.exampleLabels.techExact", descriptionKey: "syntax.exampleDescriptions.techExact", featured: true },
  { id: "has-screenshot", category: "basics", query: 'hasScreenshot=="true"', labelKey: "syntax.exampleLabels.hasScreenshot", descriptionKey: "syntax.exampleDescriptions.hasScreenshot" },
  { id: "response-headers-contains", category: "basics", query: 'responseHeaders="rememberMe=deleteMe"', labelKey: "syntax.exampleLabels.responseHeaders", descriptionKey: "syntax.exampleDescriptions.responseHeaders" },
  { id: "organization-contains", category: "basics", query: 'organization="腾讯"', labelKey: "syntax.exampleLabels.organizationContains", descriptionKey: "syntax.exampleDescriptions.organizationContains", featured: true },
  { id: "organization-exact", category: "basics", query: 'organization=="Tencent"', labelKey: "syntax.exampleLabels.organizationExact", descriptionKey: "syntax.exampleDescriptions.organizationExact" },
  { id: "organization-unbound", category: "basics", query: 'organization==""', labelKey: "syntax.exampleLabels.organizationUnbound", descriptionKey: "syntax.exampleDescriptions.organizationUnbound" },
  { id: "url-exact", category: "basics", query: 'url=="https://example.com"', labelKey: "syntax.exampleLabels.urlExact", descriptionKey: "syntax.exampleDescriptions.urlExact" },
  { id: "title-exact", category: "basics", query: 'title=="Admin Login"', labelKey: "syntax.exampleLabels.titleExact", descriptionKey: "syntax.exampleDescriptions.titleExact" },
  { id: "status-exact-404", category: "basics", query: 'statusCode="404"', labelKey: "syntax.exampleLabels.statusExact", descriptionKey: "syntax.exampleDescriptions.statusErrorExact" },
  { id: "tech-exact", category: "basics", query: 'tech=="React"', labelKey: "syntax.exampleLabels.techExact", descriptionKey: "syntax.exampleDescriptions.techExact" },
  { id: "host-status", category: "scenarios", query: 'host="api" && statusCode=="200"', labelKey: "syntax.exampleLabels.hostStatus", descriptionKey: "syntax.exampleDescriptions.hostStatus", featured: true },
  { id: "host-tech", category: "scenarios", query: 'host="api" && tech="nginx"', labelKey: "syntax.exampleLabels.hostTech", descriptionKey: "syntax.exampleDescriptions.hostTech" },
  { id: "title-status", category: "scenarios", query: 'title="Login" && statusCode=="200"', labelKey: "syntax.exampleLabels.titleStatus", descriptionKey: "syntax.exampleDescriptions.titleStatus" },
  { id: "url-tech", category: "scenarios", query: 'url="admin" && tech="nginx"', labelKey: "syntax.exampleLabels.urlTech", descriptionKey: "syntax.exampleDescriptions.urlTech" },
  { id: "url-or-host", category: "scenarios", query: 'url="admin" || host="api"', labelKey: "syntax.exampleLabels.urlOrHost", descriptionKey: "syntax.exampleDescriptions.urlOrHost" },
]

export function parseGlobalAssetSearchQuery(raw: string): GlobalAssetSearchQuery {
  if (textEncoder.encode(raw).byteLength > GLOBAL_ASSET_SEARCH_MAX_QUERY_BYTES) {
    throw new GlobalAssetSearchQueryError(
      `q must not exceed ${GLOBAL_ASSET_SEARCH_MAX_QUERY_BYTES} UTF-8 bytes`,
      "queryTooLong"
    )
  }

  const query = raw.trim()
  if (!query) {
    throw new GlobalAssetSearchQueryError("q is required", "required")
  }

  if (isPlainObservedURL(query) || !looksStructured(query)) {
    assertContainsLength(query, "url", GLOBAL_ASSET_SEARCH_MIN_URL_CONTAINS_RUNES)
    return { mode: "plainUrl", value: query }
  }

  const parsed = new StrictQueryParser(query).parse()
  return { mode: "structured", combinator: parsed.combinator, conditions: parsed.conditions }
}

export function isGlobalAssetSearchQueryValid(raw: string): boolean {
  try {
    parseGlobalAssetSearchQuery(raw)
    return true
  } catch {
    return false
  }
}

export function normalizeGlobalAssetSearchPageSize(value: number | undefined): number {
  if (value === undefined) {
    return GLOBAL_ASSET_SEARCH_DEFAULT_PAGE_SIZE
  }
  if (!Number.isInteger(value) || value < 1 || value > GLOBAL_ASSET_SEARCH_MAX_PAGE_SIZE) {
    throw new GlobalAssetSearchQueryError(`pageSize must be between 1 and ${GLOBAL_ASSET_SEARCH_MAX_PAGE_SIZE}`)
  }
  return value
}

export function globalAssetSearchQueryFingerprint(query: GlobalAssetSearchQuery, assetType: AssetType, pageSize: number): string {
  const canonical = query.mode === "plainUrl"
    ? `plain:url=${JSON.stringify(query.value)}`
    : `structured:${query.conditions
      .map((condition) => `${condition.field}${condition.operator}${JSON.stringify(String(condition.value))}`)
      .sort()
      .join(query.combinator === "or" ? "||" : "&&")}`
  return `${assetType}:${pageSize}:${canonical}`
}

/**
 * Organization scope applied by the search page picker. `unbound` selects
 * targets that are linked to no active organization (`organization==""`),
 * `organization` scopes searches to one organization by display name, and
 * `all` leaves the query untouched.
 */
export type GlobalAssetSearchOrgScope =
  | { mode: "unbound" }
  | { mode: "all" }
  | { mode: "organization"; name: string }

export type ComposedGlobalAssetSearchQuery =
  | { status: "none" }
  | { status: "query"; query: string; takeover: boolean }

export function formatGlobalAssetSearchCondition(condition: GlobalAssetSearchCondition): string {
  return `${condition.field}${condition.operator}${JSON.stringify(String(condition.value))}`
}

function globalAssetSearchOrgCondition(scope: GlobalAssetSearchOrgScope): string {
  if (scope.mode === "unbound") {
    return 'organization==""'
  }
  if (scope.mode === "organization") {
    return `organization==${JSON.stringify(scope.name)}`
  }
  return ""
}

/**
 * Merge the draft text with the picker scope into the submitted query. The
 * scope always rides inside `q` so page tokens, exports, and shared links
 * keep working without any new API surface. A draft that already carries its
 * own organization condition wins (`takeover`); the caller then resets the
 * picker to `all` instead of silently re-scoping the user's query.
 */
export function composeGlobalAssetSearchQuery(text: string, scope: GlobalAssetSearchOrgScope): ComposedGlobalAssetSearchQuery {
  const trimmed = text.trim()
  const scopeCondition = globalAssetSearchOrgCondition(scope)
  if (!scopeCondition) {
    return trimmed ? { status: "query", query: trimmed, takeover: false } : { status: "none" }
  }
  if (!trimmed) {
    return { status: "query", query: scopeCondition, takeover: false }
  }

  const parsed = parseGlobalAssetSearchQuery(trimmed)
  const orgConditions = parsed.mode === "structured"
    ? parsed.conditions.filter((condition) => condition.field === "organization")
    : []
  if (orgConditions.length > 0) {
    const carriesScope = orgConditions.some(
      (condition) => formatGlobalAssetSearchCondition(condition) === scopeCondition
    )
    // Re-submitting an already scoped query (asset type or page size change)
    // must stay idempotent instead of appending the scope twice.
    return { status: "query", query: trimmed, takeover: !carriesScope }
  }
  if (parsed.mode === "structured" && parsed.combinator === "or") {
    // The flat DSL cannot express (A || B) && scope; appending the scope would
    // either fail to parse or silently widen the filter, so reject up front.
    throw new GlobalAssetSearchQueryError(
      "organization scope cannot be combined with || connectors",
      "scopeNotCombinable"
    )
  }
  const draftConditions = parsed.mode === "plainUrl"
    ? `url=${JSON.stringify(parsed.value)}`
    : parsed.conditions.map(formatGlobalAssetSearchCondition).join(" && ")
  const composed = `${draftConditions} && ${scopeCondition}`
  // Re-parse so byte limits and the ten-condition cap surface their own
  // diagnostics instead of a malformed submission.
  parseGlobalAssetSearchQuery(composed)
  return { status: "query", query: composed, takeover: false }
}

/**
 * Split a submitted query back into picker scope plus draft text. Unknown or
 * ambiguous queries (or-combinators, contains matches, unknown names) stay
 * verbatim under the `all` scope so shared links never lose information.
 */
export function decomposeGlobalAssetSearchQuery(
  raw: string,
  knownOrgNames: readonly string[]
): { scope: GlobalAssetSearchOrgScope; text: string } {
  if (!raw.trim()) {
    return { scope: { mode: "unbound" }, text: "" }
  }
  let parsed: GlobalAssetSearchQuery
  try {
    parsed = parseGlobalAssetSearchQuery(raw)
  } catch {
    return { scope: { mode: "all" }, text: raw }
  }
  if (parsed.mode !== "structured" || parsed.combinator === "or") {
    return { scope: { mode: "all" }, text: raw }
  }
  const orgExact = parsed.conditions.filter(
    (condition) => condition.field === "organization" && condition.operator === "=="
  )
  if (orgExact.length !== 1) {
    return { scope: { mode: "all" }, text: raw }
  }
  const name = String(orgExact[0]!.value)
  if (name !== "" && !knownOrgNames.includes(name)) {
    return { scope: { mode: "all" }, text: raw }
  }
  const remaining = parsed.conditions.filter((condition) => condition !== orgExact[0])
  return {
    scope: name === "" ? { mode: "unbound" } : { mode: "organization", name },
    text: remaining.map(formatGlobalAssetSearchCondition).join(" && "),
  }
}

function looksStructured(query: string): boolean {
  if (query.includes("&&") || query.includes("||") || query.includes("!=") || /[()]/.test(query)) {
    return true
  }
  return /(?:^|\s)[A-Za-z][A-Za-z0-9_]*\s*=/.test(query)
}

function isPlainObservedURL(query: string): boolean {
  return /^https?:\/\//iu.test(query)
}

function assertContainsLength(value: string, field: string, minimumRunes: number) {
  if (Array.from(value.trim()).length < minimumRunes) {
    throw new GlobalAssetSearchQueryError(
      `${field} contains values must have at least ${minimumRunes} Unicode characters`,
      "containsValueTooShort"
    )
  }
}

class StrictQueryParser {
  private position = 0

  constructor(private readonly input: string) {}

  parse(): { combinator: GlobalAssetSearchCombinator; conditions: GlobalAssetSearchCondition[] } {
    const conditions: GlobalAssetSearchCondition[] = []
    let combinator: GlobalAssetSearchCombinator = "and"
    let chosen = false
    this.skipWhitespace()

    while (!this.atEnd()) {
      conditions.push(this.parseCondition())
      if (conditions.length > GLOBAL_ASSET_SEARCH_MAX_CONDITIONS) {
        throw new GlobalAssetSearchQueryError(
          `at most ${GLOBAL_ASSET_SEARCH_MAX_CONDITIONS} conditions are allowed`,
          "tooManyConditions"
        )
      }
      this.skipWhitespace()
      if (this.atEnd()) break
      const next = this.consumeConnector()
      if (!next) {
        throw new GlobalAssetSearchQueryError(
          `expected &&, and, ||, or the word or at character ${this.position}`,
          "invalidConnector"
        )
      }
      if (!chosen) {
        combinator = next
        chosen = true
      } else if (next !== combinator) {
        throw new GlobalAssetSearchQueryError(
          `mixed and/or connectors at character ${this.position}`,
          "invalidConnector"
        )
      }
      this.skipWhitespace()
      if (this.atEnd()) {
        throw new GlobalAssetSearchQueryError("missing condition after connector", "missingCondition")
      }
    }

    if (conditions.length === 0) {
      throw new GlobalAssetSearchQueryError("q is required", "required")
    }
    return { combinator, conditions }
  }

  private parseCondition(): GlobalAssetSearchCondition {
    const field = this.parseField()
    this.skipWhitespace()
    const operator = this.parseOperator()
    this.skipWhitespace()
    const rawValue = this.parseQuotedValue()

    if (field === "statusCode") {
      if (!/^\d+$/.test(rawValue)) {
        throw new GlobalAssetSearchQueryError("statusCode must be an integer", "invalidStatusCode")
      }
      const value = Number(rawValue)
      if (!Number.isSafeInteger(value)) {
        throw new GlobalAssetSearchQueryError("statusCode must be an integer", "invalidStatusCode")
      }
      return { field, operator, value }
    }

    if (field === "hasScreenshot") {
      // Both operators share one exact semantics; anything outside lowercase
      // "true"/"false" is rejected instead of being coerced to a boolean.
      if (rawValue !== "true" && rawValue !== "false") {
        throw new GlobalAssetSearchQueryError('hasScreenshot must be "true" or "false"', "invalidBoolean")
      }
      return { field, operator, value: rawValue }
    }

    if (operator === "=" && (field === "url" || field === "host" || field === "title" || field === "organization")) {
      assertContainsLength(rawValue, field, GLOBAL_ASSET_SEARCH_MIN_CONTAINS_RUNES)
    }
    if (operator === "=" && field === "responseHeaders") {
      assertContainsLength(rawValue, field, GLOBAL_ASSET_SEARCH_MIN_RESPONSE_HEADER_CONTAINS_RUNES)
    }
    return { field, operator, value: rawValue }
  }

  private parseField(): GlobalAssetSearchField {
    if (this.atEnd() || !isIdentifierStart(this.input[this.position]!)) {
      throw new GlobalAssetSearchQueryError(
        `expected a field name at character ${this.position}`,
        "invalidField"
      )
    }
    const start = this.position
    this.position += 1
    while (!this.atEnd() && isIdentifierPart(this.input[this.position]!)) {
      this.position += 1
    }
    const field = this.input.slice(start, this.position) as GlobalAssetSearchField
    if (!allowedFields.has(field)) {
      throw new GlobalAssetSearchQueryError(`unsupported field ${field}`, "unsupportedField")
    }
    return field
  }

  private parseOperator(): GlobalAssetSearchOperator {
    if (this.consume("==")) return "=="
    if (this.consume("=")) return "="
    throw new GlobalAssetSearchQueryError(
      `expected = or == at character ${this.position}`,
      "invalidOperator"
    )
  }

  private parseQuotedValue(): string {
    if (this.atEnd() || this.input[this.position] !== "\"") {
      throw new GlobalAssetSearchQueryError(
        `expected a quoted value at character ${this.position}`,
        "missingQuotedValue"
      )
    }
    const start = this.position
    this.position += 1
    let escaped = false
    while (!this.atEnd()) {
      const character = this.input[this.position]!
      this.position += 1
      if (escaped) {
        escaped = false
        continue
      }
      if (character === "\\") {
        escaped = true
        continue
      }
      if (character === "\"") {
        try {
          return JSON.parse(this.input.slice(start, this.position)) as string
        } catch {
          throw new GlobalAssetSearchQueryError("invalid quoted value", "invalidQuotedValue")
        }
      }
    }
    throw new GlobalAssetSearchQueryError("unterminated quoted value", "unterminatedQuotedValue")
  }

  private skipWhitespace() {
    while (!this.atEnd() && /\s/.test(this.input[this.position]!)) {
      this.position += 1
    }
  }

  private consume(value: string): boolean {
    if (!this.input.startsWith(value, this.position)) return false
    this.position += value.length
    return true
  }

  // Words are recognized only as whole tokens, so android and origin stay field
  // names. Plain text that merely contains "and" or "or" never reaches this parser.
  private consumeConnector(): GlobalAssetSearchCombinator | null {
    if (this.consume("&&")) return "and"
    if (this.consume("||")) return "or"
    if (this.consumeWord("and")) return "and"
    if (this.consumeWord("or")) return "or"
    return null
  }

  private consumeWord(word: string): boolean {
    if (this.position + word.length > this.input.length) return false
    if (this.input.slice(this.position, this.position + word.length).toLowerCase() !== word) return false
    const next = this.input[this.position + word.length]
    if (next !== undefined && isIdentifierPart(next)) return false
    this.position += word.length
    return true
  }

  private atEnd(): boolean {
    return this.position >= this.input.length
  }
}

function isIdentifierStart(value: string): boolean {
  return /^[A-Za-z]$/.test(value)
}

function isIdentifierPart(value: string): boolean {
  return /^[A-Za-z0-9_]$/.test(value)
}

function validateGuidanceEntry(entry: GlobalAssetSearchGuidanceEntry): GlobalAssetSearchGuidanceEntry {
  parseGlobalAssetSearchQuery(entry.query)
  return entry
}

export const GLOBAL_ASSET_SEARCH_GUIDANCE = RAW_GLOBAL_ASSET_SEARCH_GUIDANCE.map(validateGuidanceEntry)
export const GLOBAL_ASSET_SEARCH_FEATURED_GUIDANCE = GLOBAL_ASSET_SEARCH_GUIDANCE.filter((entry) => entry.featured)
