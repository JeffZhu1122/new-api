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
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { BrandBackdrop } from '@/components/brand'
import { Button } from '@/components/ui/button'

interface CTAProps {
  className?: string
  isAuthenticated?: boolean
}

export function CTA(props: CTAProps) {
  const { t } = useTranslation()

  if (props.isAuthenticated) {
    return null
  }

  return (
    // Full-bleed closing section: the aurora opens up across the page width
    // instead of sitting inside a card.
    <section className='relative isolate z-10 overflow-hidden px-4 py-28 sm:px-6 md:py-40'>
      <BrandBackdrop
        variant='band'
        rings='static'
        flutes='refract'
        className='[mask-image:linear-gradient(to_bottom,transparent,#000_22%,#000_78%,transparent)] [-webkit-mask-image:linear-gradient(to_bottom,transparent,#000_22%,#000_78%,transparent)]'
      />
      <div aria-hidden='true' className='brand-rule absolute inset-x-0 top-0' />
      <div className='relative mx-auto max-w-6xl'>
        <AnimateInView
          className='mx-auto max-w-3xl text-center'
          animation='scale-in'
        >
          <h2 className='brand-display text-4xl md:text-6xl'>
            {t('Ready to simplify')}
            <br />
            <span className='brand-text-aurora'>
              {t('your AI integration?')}
            </span>
          </h2>
          <p className='text-muted-foreground mx-auto mt-6 max-w-lg text-sm leading-relaxed md:text-base'>
            {t(
              'Deploy your own gateway and start routing requests through your configured upstream services.'
            )}
          </p>
          <div className='mt-10 flex flex-wrap items-center justify-center gap-3'>
            <Button
              className='brand-cta group h-12 rounded-full px-6'
              render={<Link to='/sign-up' />}
            >
              {t('Get Started')}
              <ArrowRight className='ml-1 size-3.5 transition-transform duration-200 group-hover:translate-x-0.5' />
            </Button>
            <Button
              variant='outline'
              className='border-hairline-strong dark:border-hairline-strong bg-glass-strong hover:bg-glass dark:bg-glass-strong dark:hover:bg-glass h-12 rounded-full px-6'
              render={<Link to='/pricing' />}
            >
              {t('View Pricing')}
            </Button>
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
