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
import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'

import { act, cleanup, render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { afterEach, describe, expect, it } from 'vitest'

import { ScheduleEditor } from '@/features/user-control/schedule-editor'
import { resources } from '@/i18n/config'
import { STATIC_I18N_KEYS } from '@/i18n/static-keys'

const webRoot = path.resolve(import.meta.dirname, '../../..')
const featurePaths = [
  'src/features/user-control',
  'src/features/users/batch',
  'src/features/users/components/batch-action-dialog.tsx',
  'src/features/users/components/batch-jobs-sheet.tsx',
  'src/features/users/components/data-table-bulk-actions.tsx',
  'src/features/system-settings/security/user-control-section.tsx',
  'src/i18n/user-management-errors.ts',
]
const sourceFiles = featurePaths.flatMap((relativePath) => {
  const absolutePath = path.join(webRoot, relativePath)
  if (/\.tsx?$/.test(relativePath)) return [absolutePath]
  return readdirSync(absolutePath, { recursive: true })
    .map(String)
    .filter((name) => /\.tsx?$/.test(name) && !name.includes('__tests__'))
    .map((name) => path.join(absolutePath, name))
})
const featureKeys = new Set<string>()
for (const file of sourceFiles) {
  const source = readFileSync(file, 'utf8')
  for (const match of source.matchAll(
    /\bt\(\s*(['"])((?:\\.|(?!\1)[^\\])*)\1/g
  )) {
    featureKeys.add(
      match[2].replaceAll(
        /\\u([\da-f]{4})|\\(['"\\])/gi,
        (_, hex: string | undefined, escaped: string) =>
          hex ? String.fromCharCode(Number.parseInt(hex, 16)) : escaped
      )
    )
  }
  for (const match of source.matchAll(/\b(?:key|labelKey):\s*'([^']+)'/g)) {
    featureKeys.add(match[1])
  }
}
const permissionSource = readFileSync(
  path.join(webRoot, '../service/authz/resources_user_management.go'),
  'utf8'
)
const permissionKeys = [
  ...permissionSource.matchAll(/(?:LabelKey|DescriptionKey):\s*"([^"]+)"/g),
].map((match) => match[1])
for (const key of permissionKeys) featureKeys.add(key)
const actionSource = readFileSync(
  path.join(webRoot, 'src/features/users/batch/types.ts'),
  'utf8'
).split('export interface')[0]
const actionKeys = [...actionSource.matchAll(/\|\s*'([^']+)'/g)].map(
  (match) => match[1]
)
for (const key of actionKeys) featureKeys.add(key)
const stateKeys = [
  'pending',
  'running',
  'completed',
  'partial',
  'failed',
  'cancelled',
  'succeeded',
  'skipped',
]
for (const key of stateKeys) featureKeys.add(key)
const backendKeys = new Set<string>()
const auditKeys = new Set<string>()
const errorTemplateKeys = [
  ...readFileSync(
    path.join(webRoot, 'src/i18n/user-management-errors.ts'),
    'utf8'
  ).matchAll(/\bkey: '([^']+)'/g),
].map((match) => match[1])
const errorPrefixes: Record<string, string> = {
  ErrInvalidInput: 'invalid user control input',
  ErrInvalidRequest: 'invalid batch request',
  ErrIdempotencyConflict: 'idempotency key conflict',
}
for (const file of [
  'controller/user_batch.go',
  'controller/user_control.go',
  'service/userbatch/service.go',
  'service/usercontrol/admin.go',
  'service/usercontrol/validation.go',
  'model/user_quota_adjustment.go',
]) {
  const source = readFileSync(path.join(webRoot, '..', file), 'utf8')
  for (const match of source.matchAll(
    /errors.New\("([^"]+)"\)|(?:batchFail|userControlFail)\(c, [^,]+, "([^"]+)"\)|(?:SkipReason|ErrorMessage|Reason)\s*[:=]\s*"([^"]+)"|return after, "[^"]+", "([^"]+)"|Warnings = append\([^,]+, "([^"]+)"\)/g
  )) {
    const key = match.slice(1).find((value) => value !== undefined)
    if (key) backendKeys.add(key)
  }
  for (const match of source.matchAll(
    /fmt.Errorf\("%w: ([^"%]+)", (ErrInvalidRequest|ErrIdempotencyConflict|ErrInvalidInput)\)/g
  )) {
    backendKeys.add(`${errorPrefixes[match[2]]}: ${match[1]}`)
  }
  for (const match of source.matchAll(/auditUserControl\(c, "([^"]+)"/g)) {
    auditKeys.add(`user_control.${match[1]}`)
  }
}
for (const key of [
  ...backendKeys,
  ...auditKeys,
  'user override',
  'merge',
  'replace',
  'user',
  'users',
  'Close',
  'Bulk action started',
]) {
  featureKeys.add(key)
}
const cognates = new Set([
  'Action',
  'Audit',
  'Description',
  'Mode',
  'Plan',
  'Quota',
  'Sources',
  'Total',
])

afterEach(cleanup)

describe('user management translations', () => {
  it('when all seven locale files are loaded, their complete key sets match', () => {
    const englishKeys = Object.keys(resources.en.translation).sort()
    for (const resource of Object.values(resources)) {
      expect(Object.keys(resource.translation).sort()).toEqual(englishKeys)
    }
  })
  it.each(Object.keys(resources) as (keyof typeof resources)[])(
    'when %s is selected, all feature labels and permission descriptions have local translations',
    (language) => {
      const translations: Record<string, string> =
        resources[language].translation
      for (const key of featureKeys) {
        expect(Object.hasOwn(translations, key), `${language}: ${key}`).toBe(
          true
        )
        expect(translations[key]?.trim(), `${language}: ${key}`).toBeTruthy()
        expect(
          translations[key]?.match(/{{[^}]+}}/g)?.sort() ?? [],
          key
        ).toEqual(key.match(/{{[^}]+}}/g)?.sort() ?? [])
        const english: Record<string, string> = resources.en.translation
        if (language !== 'en' && !(language === 'fr' && cognates.has(key))) {
          expect(translations[key], `${language}: ${key}`).not.toBe(
            english[key]
          )
        }
        if (language === 'zhCN' || language === 'zhTW') {
          expect(
            translations[key].replaceAll(/{{[^}]+}}/g, ''),
            `${language}: ${key}`
          ).toMatch(/[\u3400-\u9fff]/)
        }
      }
    }
  )

  it('when permission labels or action states arrive dynamically, their keys are registered', () => {
    for (const key of [
      ...permissionKeys,
      ...actionKeys,
      ...stateKeys,
      ...backendKeys,
      ...auditKeys,
      ...errorTemplateKeys,
      'user override',
      'merge',
      'replace',
      'user',
      'users',
      'Close',
      'Bulk action started',
    ]) {
      expect(STATIC_I18N_KEYS as readonly string[], key).toContain(key)
    }
  })

  it('when the language changes, the schedule editor updates weekdays and the full rule explanation', async () => {
    const instance = createInstance()
    await instance.use(initReactI18next).init({
      resources,
      lng: 'en',
      fallbackLng: false,
      nsSeparator: false,
      interpolation: { escapeValue: false },
    })
    render(
      <I18nextProvider i18n={instance}>
        <ScheduleEditor
          value={{
            timezone: 'Asia/Shanghai',
            rules: [{ id: 'weekday', name: '', enabled: true, limits: {} }],
          }}
          onChange={() => {}}
        />
      </I18nextProvider>
    )
    const explanationKey = [...featureKeys].find((key) =>
      key.startsWith('Rules are evaluated independently.')
    )
    if (!explanationKey) {
      throw new Error('Schedule explanation key was not found')
    }
    for (const language of [
      'zhCN',
      'zhTW',
      'fr',
      'ja',
      'ru',
      'vi',
      'en',
    ] as const) {
      await act(async () => {
        await instance.changeLanguage(language)
      })
      expect(
        screen.getByRole('button', {
          name: resources[language].translation.Mon,
        })
      ).toBeTruthy()
      expect(
        screen.getByRole('button', {
          name: resources[language].translation['Add time window'],
        })
      ).toBeTruthy()
      const translations: Record<string, string> =
        resources[language].translation
      expect(screen.getByText(translations[explanationKey])).toBeTruthy()
      expect(screen.queryByText(/{{[^}]+}}/)).toBeNull()
    }
  })
})
