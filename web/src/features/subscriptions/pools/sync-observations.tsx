/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { SyncObservation } from './sync-api'
import { SyncUsageCard } from './sync-usage'

export function SyncObservations(props: { observations: SyncObservation[] }) {
  return (
    <div className='grid min-w-0 gap-3 md:grid-cols-2 2xl:grid-cols-3'>
      {props.observations.map((o) => (
        <SyncUsageCard
          key={o.key}
          name={o.name}
          utilization={o.utilization}
          windowEnd={o.window_end}
          sampledAt={o.sampled_at}
          fetchedAt={o.fetched_at}
          stale={o.stale}
          issue={o.pending}
        />
      ))}
    </div>
  )
}
