import { describe, expect, it } from "vitest"
import { responseMessage } from "./response-message"

function reply(body: string, status = 400, statusText = "Bad Request") {
  return new Response(body, { status, statusText })
}

describe("responseMessage", () => {
  it("shows the error field of a JSON answer, not the braces", async () => {
    expect(await responseMessage(reply(JSON.stringify({ error: "needs a project" })))).toBe("needs a project")
  })

  it("falls back to the status text when the body is empty", async () => {
    expect(await responseMessage(reply("", 500, "Internal Server Error"))).toBe("Internal Server Error")
  })

  it("returns a plain-text body as it is", async () => {
    expect(await responseMessage(reply("minutes must be 0..1440"))).toBe("minutes must be 0..1440")
  })

  it("keeps the words of an HTML answer and drops the markup", async () => {
    expect(await responseMessage(reply("<p>Workspace <b>locked</b></p>"))).toBe("Workspace locked")
  })
})
