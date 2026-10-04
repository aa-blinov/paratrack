import type { Session } from "./types"

export function sessionElapsedSeconds(session: Session, nowMs: number): number {
  const [hours, minutes, seconds] = session.Clock.split(":").map(Number)
  const base = (hours || 0) * 3600 + (minutes || 0) * 60 + (seconds || 0)
  if (session.Paused || !session.ResumeISO) return base
  const resumeMs = Date.parse(session.ResumeISO)
  if (!Number.isFinite(resumeMs)) return base
  return base + Math.max(0, Math.floor((nowMs - resumeMs) / 1000))
}

export function clockLabel(totalSeconds: number): string {
  const hours = Math.floor(Math.max(0, totalSeconds) / 3600)
  const minutes = Math.floor((Math.max(0, totalSeconds) % 3600) / 60)
  const seconds = Math.max(0, totalSeconds) % 60
  return [hours, minutes, seconds].map(value => String(value).padStart(2, "0")).join(":")
}

export function durationLabel(totalSeconds: number, language: string): string {
  const value = Math.max(0, Math.floor(totalSeconds))
  const hours = Math.floor(value / 3600)
  const minutes = Math.floor((value % 3600) / 60)
  const seconds = value % 60
  const day = language === "ru" ? "д" : "d"
  const hour = language === "ru" ? "ч" : "h"
  const minute = language === "ru" ? "м" : "m"
  const second = language === "ru" ? "с" : "s"
  const parts: string[] = []
  if (hours >= 24) parts.push(`${Math.floor(hours / 24)}${day}`)
  const remainderHours = hours % 24
  if (remainderHours) parts.push(`${remainderHours}${hour}`)
  if (minutes) parts.push(`${minutes}${minute}`)
  if (!parts.length || (!hours && seconds)) parts.push(`${seconds}${second}`)
  return parts.slice(0, 2).join(" ")
}
