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
import { CherryStudio } from '@lobehub/icons'
import { Link } from '@tanstack/react-router'
import { ArrowRight, BookOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { BrandBackdrop, BrandPulse } from '@/components/brand'
import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'

import { Stats } from './stats'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

// Stylized three-dots indicator representing "More"
const MoreIcon = () => (
  <svg
    className='text-muted-foreground/60 group-hover:text-foreground size-6 shrink-0 transition-colors'
    viewBox='0 0 24 24'
    fill='none'
    xmlns='http://www.w3.org/2000/svg'
  >
    <circle cx='6' cy='12' r='2' fill='currentColor' />
    <circle cx='12' cy='12' r='2' fill='currentColor' />
    <circle cx='18' cy='12' r='2' fill='currentColor' />
  </svg>
)

const SECONDARY_ACTION_CLASS =
  'group border-hairline-strong dark:border-hairline-strong bg-glass-strong hover:bg-glass dark:bg-glass-strong dark:hover:bg-glass inline-flex h-12 items-center gap-1.5 rounded-full px-6 text-sm font-medium'

const APP_PILL_CLASS =
  'brand-surface brand-sheen group text-foreground/85 hover:text-foreground flex items-center gap-3 rounded-full px-5 py-2.5 text-sm font-medium transition-colors'

export function Hero(props: HeroProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsUrl =
    (status?.docs_link as string | undefined) || 'https://docs.newapi.pro'

  const renderDocsButton = () => {
    const isExternal = docsUrl.startsWith('http')
    if (isExternal) {
      return (
        <Button
          variant='outline'
          className={SECONDARY_ACTION_CLASS}
          render={
            <a href={docsUrl} target='_blank' rel='noopener noreferrer' />
          }
        >
          <BookOpen className='text-muted-foreground/80 group-hover:text-foreground size-4 transition-colors duration-200' />
          <span>{t('Docs')}</span>
        </Button>
      )
    }
    return (
      <Button
        variant='outline'
        className={SECONDARY_ACTION_CLASS}
        render={<Link to={docsUrl} />}
      >
        <BookOpen className='text-muted-foreground/80 group-hover:text-foreground size-4 transition-colors duration-200' />
        <span>{t('Docs')}</span>
      </Button>
    )
  }

  return (
    <section className='relative isolate z-10 overflow-hidden px-6 pt-28 md:pt-36 lg:pt-40'>
      <BrandBackdrop variant='hero-center' rings='sonar' flutes />

      {/* Centred headline stack */}
      <div className='mx-auto flex max-w-4xl flex-col items-center text-center'>
        <div
          className='landing-animate-fade-up brand-chip mb-6 opacity-0'
          style={{ animationDelay: '0ms' }}
        >
          <BrandPulse />
          <span>{t('AI Application Infrastructure Foundation')}</span>
        </div>

        <h1
          className='landing-animate-fade-up brand-display text-[clamp(2.5rem,6.2vw,4.75rem)]'
          style={{ animationDelay: '60ms' }}
        >
          {t('Unified API Gateway for')}
          <br />
          <span className='brand-text-aurora brand-text-aurora--flow'>
            {t('Vast Range of AI Models')}
          </span>
        </h1>
        <p
          className='landing-animate-fade-up text-muted-foreground mt-6 max-w-2xl text-base leading-relaxed text-pretty wrap-break-word break-keep opacity-0 md:text-lg'
          style={{ animationDelay: '120ms' }}
        >
          {t(
            'Access a vast selection of models via a standard, unified API protocol. Power AI applications, manage digital assets, and connect the Future.'
          )}
        </p>

        <div
          className='landing-animate-fade-up mt-10 flex flex-wrap items-center justify-center gap-3 opacity-0'
          style={{ animationDelay: '180ms' }}
        >
          {props.isAuthenticated ? (
            <>
              <Button
                className='brand-cta group h-12 rounded-full px-7 text-sm font-semibold'
                render={<Link to='/dashboard' />}
              >
                {t('Go to Dashboard')}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
              {renderDocsButton()}
            </>
          ) : (
            <>
              <Button
                className='brand-cta group h-12 rounded-full px-7 text-sm font-semibold'
                render={<Link to='/sign-up' />}
              >
                {t('Get Started')}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
              <Button
                variant='outline'
                className={SECONDARY_ACTION_CLASS}
                render={<Link to='/pricing' />}
              >
                {t('View Pricing')}
              </Button>
              {renderDocsButton()}
            </>
          )}
        </div>
      </div>

      {/* The hero settles onto a lit horizon; stats and apps sit on it */}
      <div
        className='landing-animate-fade-up relative mx-auto mt-20 max-w-6xl opacity-0 md:mt-28'
        style={{ animationDelay: '240ms' }}
      >
        <div aria-hidden='true' className='brand-horizon -inset-x-[50vw]' />
        <div className='relative pt-12 md:pt-16'>
          <Stats />
        </div>

        <div className='relative flex flex-col items-center pt-14 pb-20 text-center md:pt-16 md:pb-24'>
          <span className='brand-eyebrow'>{t('Supported Applications')}</span>
          <p className='text-muted-foreground mt-2 max-w-md text-xs leading-relaxed text-pretty wrap-break-word break-keep'>
            {t(
              'Supports one-click configuration and perfectly adapts to NewAPI multi-protocol configuration.'
            )}
          </p>
          <div className='mt-6 flex flex-wrap items-center justify-center gap-3'>
            {/* Cherry Studio */}
            <a
              href='https://cherry-ai.com'
              target='_blank'
              rel='noopener noreferrer'
              className={APP_PILL_CLASS}
            >
              <CherryStudio.Color size={24} className='shrink-0' />
              <span>Cherry Studio</span>
            </a>

            {/* CC Switch */}
            <a
              href='https://ccswitch.io'
              target='_blank'
              rel='noopener noreferrer'
              className={APP_PILL_CLASS}
            >
              <img
                src='https://ccswitch.io/favicon.png'
                alt='CC Switch'
                className='size-6 shrink-0 rounded-md object-contain'
                onError={(e) => {
                  // Fallback to a styled text avatar if the remote favicon fails to load in sandbox or local environments
                  e.currentTarget.style.display = 'none'
                  const fallback = e.currentTarget.nextSibling as HTMLElement
                  if (fallback) fallback.style.display = 'flex'
                }}
              />
              <span
                style={{ display: 'none' }}
                className='bg-primary/10 text-primary size-6 shrink-0 items-center justify-center rounded-md text-[10px] font-bold'
              >
                CC
              </span>
              <span>CC Switch</span>
            </a>

            {/* "更多" */}
            <div className='brand-surface brand-sheen group text-muted-foreground hover:text-foreground flex cursor-default items-center gap-2.5 rounded-full px-5 py-2.5 text-sm font-medium transition-colors'>
              <MoreIcon />
              <span>{t('More Apps')}</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
