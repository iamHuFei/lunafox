import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ExpandableCell } from "../expandable-cell"

describe("ExpandableCell link mode", () => {
  it("renders an http url cell as a new-tab link that stops row-click propagation", () => {
    const onRowClick = vi.fn()
    const { container } = render(
      <div onClick={onRowClick}>
        <ExpandableCell value="https://ha.189.cn/netHall" textRoleName="tableCellPrimary" href="https://ha.189.cn/netHall" />
      </div>,
    )
    const link = screen.getByRole("link")
    expect(link).toHaveAttribute("href", "https://ha.189.cn/netHall")
    expect(link).toHaveAttribute("target", "_blank")
    expect(link).toHaveAttribute("rel", "noopener noreferrer")
    fireEvent.click(link)
    expect(onRowClick).not.toHaveBeenCalled()
    expect(container.textContent).toContain("https://ha.189.cn/netHall")
  })

  it("keeps plain rendering for empty or non-http hrefs", () => {
    const { rerender } = render(<ExpandableCell value="displayed" href="javascript:alert(1)" />)
    expect(screen.queryByRole("link")).toBeNull()
    rerender(<ExpandableCell value="displayed" href="" />)
    expect(screen.queryByRole("link")).toBeNull()
  })
})
