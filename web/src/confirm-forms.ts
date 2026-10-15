import { requestConfirmation } from "@/components/confirmation-dialog"

// A form with data-confirm asks first: deleting an invoice, a report or a
// workspace is not undone by a second click. One listener serves every page.
const confirmed = new WeakSet<HTMLFormElement>()

export function installConfirmForms() {
  document.addEventListener("submit", (event) => {
    const target = event.target
    if (!(target instanceof HTMLFormElement)) return
    const message = target.dataset.confirm
    if (!message) return
    if (confirmed.has(target)) {
      confirmed.delete(target)
      return
    }
    event.preventDefault()
    void requestConfirmation(message, { trigger: (event as SubmitEvent).submitter }).then((ok) => {
      if (!ok) return
      confirmed.add(target)
      target.requestSubmit()
    })
  }, true)
}
