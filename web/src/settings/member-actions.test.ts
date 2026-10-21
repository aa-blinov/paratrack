import { describe, expect, it } from "vitest"
import { canRemoveMember } from "./member-actions"

const owner = { UserID: 1, Role: "owner" }
const admin = { UserID: 2, Role: "admin" }
const member = { UserID: 3, Role: "member" }

describe("remove button on the members list", () => {
  it("is offered to the owner for every other member", () => {
    expect(canRemoveMember(admin, { ID: 1 }, true)).toBe(true)
    expect(canRemoveMember(member, { ID: 1 }, true)).toBe(true)
  })

  it("is not offered to an admin for the owner, who the server refuses to remove", () => {
    expect(canRemoveMember(owner, { ID: 2 }, false)).toBe(false)
  })

  it("is offered to an admin for an ordinary member", () => {
    expect(canRemoveMember(member, { ID: 2 }, false)).toBe(true)
  })

  it("is never offered on the viewer's own row", () => {
    expect(canRemoveMember(owner, { ID: 1 }, true)).toBe(false)
    expect(canRemoveMember(member, { ID: 3 }, false)).toBe(false)
  })
})
