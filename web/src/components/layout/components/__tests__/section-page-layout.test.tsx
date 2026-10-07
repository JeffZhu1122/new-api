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
import { render, screen, within } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { PageFooterPortal } from '../page-footer'
import { SectionPageLayout } from '../section-page-layout'

// brand.css styles the console page frame through these data-slot hooks
// (title tick, footer hairline); they are the styling contract.
const SLOT_HOOKS = [
  'section-page-header',
  'section-page-title',
  'section-page-actions',
  'section-page-content',
  'section-page-footer',
]

function getSlot(container: HTMLElement, slot: string): HTMLElement {
  const element = container.querySelector<HTMLElement>(`[data-slot='${slot}']`)
  if (!element) throw new Error(`Missing [data-slot='${slot}']`)
  return element
}

describe('SectionPageLayout', () => {
  test('with title, actions and content renders every style hook once', () => {
    const { container } = render(
      <SectionPageLayout>
        <SectionPageLayout.Title>API Keys</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <button type='button'>Create</button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <p>Key list</p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    for (const slot of SLOT_HOOKS) {
      expect(container.querySelectorAll(`[data-slot='${slot}']`)).toHaveLength(
        1
      )
    }
    expect(
      within(getSlot(container, 'section-page-actions')).getByRole('button', {
        name: 'Create',
      })
    ).toBeInTheDocument()
    expect(
      within(getSlot(container, 'section-page-content')).getByText('Key list')
    ).toBeInTheDocument()
  })

  test('with a title renders it as the only heading on the title hook', () => {
    const { container } = render(
      <SectionPageLayout>
        <SectionPageLayout.Title>Overview</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p>Body</p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    const headings = screen.getAllByRole('heading')
    expect(headings).toHaveLength(1)
    expect(headings[0]).toHaveTextContent('Overview')
    expect(headings[0]).toBe(getSlot(container, 'section-page-title'))
  })

  test('with a block-level title child keeps the brand tick out of the title flow', () => {
    // Channels passes a block-level flex span and system settings an
    // inline-flex max-w-full span. An in-flow ::before tick would push the
    // first onto a second line and the second past the truncating h2, so the
    // tick is absolutely positioned inside space reserved by start padding.
    const { container } = render(
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          <span className='flex min-w-0 items-center gap-2'>
            <span className='truncate'>Channels</span>
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p>Rows</p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    const title = getSlot(container, 'section-page-title')
    expect(title).toHaveClass(
      'relative',
      'truncate',
      'ps-[calc(0.5rem+3px)]',
      'before:absolute',
      'before:start-0'
    )
    expect(title).toHaveTextContent('Channels')
  })

  test('without an Actions slot renders no actions container', () => {
    const { container } = render(
      <SectionPageLayout>
        <SectionPageLayout.Title>Overview</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p>Body</p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    expect(
      container.querySelector("[data-slot='section-page-actions']")
    ).not.toBeInTheDocument()
  })

  test('without portaled footer content keeps the footer container empty', () => {
    const { container } = render(
      <SectionPageLayout>
        <SectionPageLayout.Title>Overview</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p>Body</p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    expect(getSlot(container, 'section-page-footer')).toBeEmptyDOMElement()
  })

  test('with PageFooterPortal content renders it inside the footer container', async () => {
    const { container } = render(
      <SectionPageLayout>
        <SectionPageLayout.Title>Usage Logs</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p>Rows</p>
          <PageFooterPortal>
            <button type='button'>Next page</button>
          </PageFooterPortal>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )

    const nextPage = await screen.findByRole('button', { name: 'Next page' })
    expect(getSlot(container, 'section-page-footer')).toContainElement(nextPage)
    expect(getSlot(container, 'section-page-content')).not.toContainElement(
      nextPage
    )
  })
})
