import type { PoolConfig, PoolTiming } from './types'

export function validPoolConfig(c: PoolConfig): boolean {
  const validTiming = (v: PoolTiming) =>
    Number.isInteger(v.probability) &&
    v.probability >= 0 &&
    v.probability <= 10000 &&
    Number.isInteger(v.hold_min) &&
    Number.isInteger(v.hold_max) &&
    v.hold_min >= 1 &&
    v.hold_max >= v.hold_min &&
    v.hold_max <= 1440
  return (
    validTiming(c) &&
    c.members.length > 0 &&
    c.members.length <= 64 &&
    new Set(c.members.map((m) => m.policy_id)).size === c.members.length &&
    c.members.every(
      (m) =>
        m.policy_id > 0 &&
        Number.isInteger(m.probability) &&
        m.probability >= 0 &&
        m.probability <= 10000
    ) &&
    c.members.reduce((sum, m) => sum + m.probability, 0) === 10000 &&
    Object.values(c.window_options ?? {}).every(validTiming)
  )
}

export function equalProbabilities(ids: number[]): PoolConfig['members'] {
  return ids.map((policy_id, i) => ({
    policy_id,
    probability:
      Math.floor(10000 / ids.length) + (i < 10000 % ids.length ? 1 : 0),
  }))
}
