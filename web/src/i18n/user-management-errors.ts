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
/** Legacy user-management endpoints send rendered text instead of message_key. */
export function translateUserManagementError(
  message: string,
  t: (key: string) => string
): string | undefined {
  const target = /^control target does not exist: user (\d+)$/.exec(message)
  if (target) {
    return t('control target does not exist: user {{id}}').replace(
      '{{id}}',
      () => target[1]
    )
  }
  const validation = /^invalid user control input: (.+)$/s.exec(message)
  if (validation) {
    const patterns = [
      {
        pattern: /^request template cannot set (model|group|stream)$/,
        key: 'request template cannot set {{value}}',
      },
      {
        pattern: /^invalid timezone (".*")$/s,
        key: 'invalid timezone {{value}}',
      },
      {
        pattern: /^duplicate schedule rule id (".*")$/s,
        key: 'duplicate schedule rule id {{value}}',
      },
      {
        pattern: /^invalid schedule date (".*")$/s,
        key: 'invalid schedule date {{value}}',
      },
      {
        pattern: /^invalid start_date (".*")$/s,
        key: 'invalid start_date {{value}}',
      },
      {
        pattern: /^invalid end_date (".*")$/s,
        key: 'invalid end_date {{value}}',
      },
      {
        pattern: /^invalid start time (".*")$/s,
        key: 'invalid start time {{value}}',
      },
      {
        pattern: /^invalid end time (".*")$/s,
        key: 'invalid end time {{value}}',
      },
      {
        pattern: /^models cannot be both allowed and blocked: (.+)$/s,
        key: 'models cannot be both allowed and blocked: {{models}}',
      },
    ]
    for (const entry of patterns) {
      const match = entry.pattern.exec(validation[1])
      if (!match) continue
      const reason = t(entry.key).replace(
        /{{(?:value|models)}}/,
        () => match[1]
      )
      return `${t('invalid user control input')}: ${reason}`
    }
    const translated = t(validation[1])
    if (translated !== validation[1]) {
      return `${t('invalid user control input')}: ${translated}`
    }
  }
  return undefined
}
