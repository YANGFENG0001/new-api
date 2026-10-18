import { z } from 'zod'

import type { ApplicationTarget } from './types'

export function parseTargets(value?: string): ApplicationTarget[] {
  if (!value) return []
  // Invalid server data must fail visibly, never silently turn into no limits.
  const parsed: unknown = JSON.parse(value)
  return z
    .array(
      z.object({
        kind: z.enum(['policy', 'pool']),
        id: z.number().int().positive(),
      })
    )
    .parse(parsed ?? [])
}
