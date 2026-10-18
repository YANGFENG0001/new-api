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
import { act, render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { expect, it } from 'vitest'

import { resources } from '@/i18n/config'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from '../dialog'

it('when the language changes in an open dialog, both close buttons update their accessible names and the footer text', async () => {
  const instance = createInstance()
  await instance
    .use(initReactI18next)
    .init({ resources, lng: 'en', fallbackLng: false })
  render(
    <I18nextProvider i18n={instance}>
      <Dialog open>
        <DialogContent>
          <DialogTitle>Language settings</DialogTitle>
          <DialogDescription>Close button translations</DialogDescription>
          <DialogFooter showCloseButton />
        </DialogContent>
      </Dialog>
    </I18nextProvider>
  )
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
      screen.getAllByRole('button', {
        name: resources[language].translation.Close,
      })
    ).toHaveLength(2)
    expect(
      screen.getByText(resources[language].translation.Close, {
        selector: 'button',
      })
    ).toBeVisible()
    if (language !== 'en')
      expect(screen.queryByRole('button', { name: 'Close' })).toBeNull()
  }
})
