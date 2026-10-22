import { requestConfirmation } from "@/components/confirmation-dialog"
import { translate as t } from "@/i18n"

// askPlanMove asks before a reassignment carries the old assignee's plan on the
// task's project along to the new one (TASK-03). The plan moves only when the person
// says so, and only where the server says the person may move it.
export async function askPlanMove(taskID: number, from: number, to: number, lang: string): Promise<boolean> {
  if (!from || !to || from === to) return false
  const response = await fetch(`/api/tasks/${taskID}/plan-move?to=${to}`, { credentials: "same-origin", headers: { Accept: "application/json" } })
  if (!response.ok) return false
  const preview = (await response.json()) as { minutes: number; hours: string; can_move: boolean }
  if (preview.minutes <= 0 || !preview.can_move) return false
  return requestConfirmation(t(lang, "tasks.planMoveAsk", preview.hours), { title: t(lang, "tasks.planMoveTitle"), action: t(lang, "tasks.planMoveAction") })
}
