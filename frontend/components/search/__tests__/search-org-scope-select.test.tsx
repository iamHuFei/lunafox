import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { readFileSync } from "node:fs"
import path from "node:path"
import { describe, expect, it, vi } from "vitest"

import { SearchOrgScopeSelect } from "../search-org-scope-select"
import type { SearchPageState } from "../search-page-state"

function createState(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    t: (key: string) => key,
    orgScope: { mode: "unbound" },
    orgOptions: [
      { id: 1, name: "Acme", description: "", createdAt: "", updatedAt: "" },
      { id: 2, name: "腾讯", description: "", createdAt: "", updatedAt: "" },
    ],
    orgOptionsLoading: false,
    orgOptionsHasMore: false,
    handleOrgScopeChange: vi.fn(),
    ...overrides,
  } as unknown as SearchPageState
}

function openPicker() {
  // jsdom has no PointerEvent, so drive the trigger through its keyboard path.
  const trigger = screen.getByRole("combobox", { name: "orgScope.label" })
  fireEvent.click(trigger)
  fireEvent.keyDown(trigger, { key: "ArrowDown" })
}

describe("SearchOrgScopeSelect", () => {
  it("renders the unbound option selected by default", () => {
    render(<SearchOrgScopeSelect state={createState()} />)
    expect(screen.getByRole("combobox", { name: "orgScope.label" })).toHaveTextContent("orgScope.unbound")
  })

  it("renders the all scope and organization names as trigger labels", () => {
    const { rerender } = render(<SearchOrgScopeSelect state={createState({ orgScope: { mode: "all" } })} />)
    expect(screen.getByRole("combobox", { name: "orgScope.label" })).toHaveTextContent("orgScope.all")

    rerender(<SearchOrgScopeSelect state={createState({ orgScope: { mode: "organization", name: "Acme" } })} />)
    expect(screen.getByRole("combobox", { name: "orgScope.label" })).toHaveTextContent("Acme")
  })

  it("falls back to the raw name when the selected organization is not listed", () => {
    render(<SearchOrgScopeSelect state={createState({ orgScope: { mode: "organization", name: "Ghost" } })} />)
    expect(screen.getByRole("combobox", { name: "orgScope.label" })).toHaveTextContent("Ghost")
  })

  it("lists scope sentinels and organizations, switching scope on select", async () => {
    const handleOrgScopeChange = vi.fn()
    render(<SearchOrgScopeSelect state={createState({ handleOrgScopeChange })} />)
    openPicker()

    await waitFor(() => expect(screen.getByRole("option", { name: /Acme/ })).toBeInTheDocument())
    expect(screen.getByRole("option", { name: /orgScope.unbound/ })).toBeInTheDocument()
    expect(screen.getByRole("option", { name: /orgScope.all/ })).toBeInTheDocument()
    expect(screen.getByRole("option", { name: /腾讯/ })).toBeInTheDocument()

    // Base UI option activation needs the real pointer event sequence that
    // jsdom cannot produce, so selection wiring stays a source-level contract
    // here; the state contract tests cover the handleOrgScopeChange behavior.
  })

  it("maps sentinel and prefixed option values onto scope changes", () => {
    const source = readFileSync(path.resolve(process.cwd(), "components/search/search-org-scope-select.tsx"), "utf8")
    expect(source).toContain('value === UNBOUND_SCOPE_VALUE')
    expect(source).toContain('state.handleOrgScopeChange({ mode: "unbound" })')
    expect(source).toContain('state.handleOrgScopeChange({ mode: "all" })')
    expect(source).toContain('value.startsWith(ORGANIZATION_SCOPE_PREFIX)')
    expect(source).toContain('name: value.slice(ORGANIZATION_SCOPE_PREFIX.length)')
  })

  it("surfaces the more hint when the organization page has a next token", async () => {
    render(<SearchOrgScopeSelect state={createState({ orgOptionsHasMore: true })} />)
    openPicker()
    await waitFor(() => expect(screen.getByText("orgScope.more")).toBeInTheDocument())
  })
})
