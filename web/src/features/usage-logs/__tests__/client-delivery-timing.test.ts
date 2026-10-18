import { expect, test } from 'vitest'

import { getClientOutputTps } from '../lib/format'

test('measured client delivery rate takes precedence over upstream log timing', () => {
  expect(
    getClientOutputTps(
      { completion_tokens: 100, use_time: 1 },
      { client_output_tps: 5 }
    )
  ).toBe(5)
})

test('incomplete delivery has no throughput and must not use the upstream fallback', () => {
  expect(
    getClientOutputTps(
      { completion_tokens: 100, use_time: 1 },
      { client_output_tps: null }
    )
  ).toBeNull()
})

test('legacy logs retain their timing calculation while invalid rates remain unavailable', () => {
  expect(getClientOutputTps({ completion_tokens: 100, use_time: 5 }, {})).toBe(
    20
  )
  expect(
    getClientOutputTps({ completion_tokens: 100, use_time: 0 }, {})
  ).toBeNull()
  expect(
    getClientOutputTps(
      { completion_tokens: 100, use_time: 1 },
      { client_output_tps: -1 }
    )
  ).toBeNull()
})
