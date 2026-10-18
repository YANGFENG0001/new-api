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
import { afterEach, beforeAll, describe, expect, it } from 'vitest'

import i18n, { resources } from '@/i18n/config'
import {
  createServerError,
  getServerErrorMessage,
  getServerMessage,
} from '@/lib/server-error-message'

const originalLanguage = i18n.language
beforeAll(() => {
  for (const [language, resource] of Object.entries(resources)) {
    i18n.addResourceBundle(
      language,
      'translation',
      resource.translation,
      true,
      true
    )
  }
  i18n.options.debug = false
})
afterEach(async () => {
  await i18n.changeLanguage(originalLanguage)
})

describe('user management server error translations', () => {
  it.each(Object.keys(resources) as (keyof typeof resources)[])(
    'when %s receives a legacy API failure, it translates the fixed message and wrapped error',
    async (language) => {
      await i18n.changeLanguage(language)
      const translations: Record<string, string> =
        resources[language].translation
      for (const message of [
        'Batch job does not exist',
        'invalid batch request: quota must be positive',
        'cannot control your own account',
      ]) {
        const payload = { success: false, message }
        expect(getServerMessage(payload)).toBe(translations[message])
        expect(
          getServerErrorMessage({ response: { status: 400, data: payload } })
        ).toBe(translations[message])
        expect(getServerErrorMessage(createServerError(payload))).toBe(
          translations[message]
        )
      }
    }
  )

  it.each(Object.keys(resources) as (keyof typeof resources)[])(
    'when %s receives a parameterized validation failure, it translates the explanation and keeps the supplied value',
    async (language) => {
      await i18n.changeLanguage(language)
      const translations: Record<string, string> =
        resources[language].translation
      for (const [reason, key, parameter] of [
        [
          'invalid timezone "Mars/Base"',
          'invalid timezone {{value}}',
          '"Mars/Base"',
        ],
        [
          'duplicate schedule rule id "weekend"',
          'duplicate schedule rule id {{value}}',
          '"weekend"',
        ],
        [
          'invalid schedule date "2026-99-01"',
          'invalid schedule date {{value}}',
          '"2026-99-01"',
        ],
        [
          'invalid start_date "2026-99-01"',
          'invalid start_date {{value}}',
          '"2026-99-01"',
        ],
        [
          'invalid end_date "2026-99-01"',
          'invalid end_date {{value}}',
          '"2026-99-01"',
        ],
        [
          'invalid start time "25:00"',
          'invalid start time {{value}}',
          '"25:00"',
        ],
        ['invalid end time "25:00"', 'invalid end time {{value}}', '"25:00"'],
        [
          'models cannot be both allowed and blocked: model-a, model-b',
          'models cannot be both allowed and blocked: {{models}}',
          'model-a, model-b',
        ],
      ]) {
        const message = `invalid user control input: ${reason}`
        const expected = `${
          translations['invalid user control input']
        }: ${translations[key].replace(
          /{{(?:value|models)}}/,
          () => parameter
        )}`
        expect(getServerErrorMessage({ message })).toBe(expected)
        expect(i18n.t(message)).toBe(expected)
      }
      expect(
        getServerErrorMessage({
          message: 'control target does not exist: user 17',
        })
      ).toBe(
        translations['control target does not exist: user {{id}}'].replace(
          '{{id}}',
          '17'
        )
      )
      expect(i18n.t('control target does not exist: user 17')).toBe(
        translations['control target does not exist: user {{id}}'].replace(
          '{{id}}',
          '17'
        )
      )
    }
  )

  it('when a server error contains unknown administrator text, interpolation tokens and nesting remain literal', async () => {
    await i18n.changeLanguage('zhCN')
    const message = 'custom provider: {{secret}} $t(Delete)'
    expect(getServerErrorMessage({ message })).toBe(message)
    expect(getServerErrorMessage(new Error(message))).toBe(message)
    expect(getServerErrorMessage({ message: 'User #{{id}}: {{reason}}' })).toBe(
      resources.zhCN.translation['User #{{id}}: {{reason}}']
    )
  })

  it('when ordinary keys are missing, default values, interpolation and nesting keep their normal behavior', async () => {
    await i18n.changeLanguage('zhCN')
    expect(
      i18n.t('missing.control.label', {
        defaultValue: 'Fallback {{count}}',
        count: 3,
      })
    ).toBe('Fallback 3')
    expect(i18n.t('Unknown {{count}}', { count: 3 })).toBe('Unknown 3')
    expect(i18n.t('Unknown $t(Close)')).toBe(
      `Unknown ${resources.zhCN.translation.Close}`
    )
    expect(
      i18n.t('control target does not exist: user 17', { lng: 'fr' })
    ).toBe(
      resources.fr.translation[
        'control target does not exist: user {{id}}'
      ].replace('{{id}}', '17')
    )
  })
})
