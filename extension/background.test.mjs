// The toolbar badge: a single running timer shows its time as h:mm, several
// show their count, paused ones and none show nothing.
//
// Run with: node --test extension/
import { test } from "node:test"
import assert from "node:assert/strict"
import { badgeColor, badgeText } from "./background.js"

test("badge shows the running timer's time, the count of several, or nothing", () => {
  const now = Date.parse("2026-11-28T12:00:00Z")
  const running = { Paused: false, ResumeISO: "2026-11-28T10:55:00Z", AccumulatedSeconds: 0 }
  assert.equal(badgeText([running], now), "1:05")
  assert.equal(badgeText([running, { ...running }], now), "2")
  assert.equal(badgeText([{ Paused: true, AccumulatedSeconds: 600 }], now), "")
  assert.equal(badgeText([], now), "")
})

test("badge turns amber near a limit and red past it", () => {
  assert.equal(badgeColor([{ Paused: false, LimitPercent: 50 }]), "#15803d")
  assert.equal(badgeColor([{ Paused: false, LimitPercent: 85 }]), "#b45309")
  assert.equal(badgeColor([{ Paused: false, LimitPercent: 100 }]), "#b91c1c")
  assert.equal(badgeColor([{ Paused: true, LimitPercent: 120 }]), "#15803d")
})
