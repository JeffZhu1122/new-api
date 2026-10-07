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
import { Settings, Zap, BarChart3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { BrandGlow } from '@/components/brand'

import { HeroTerminalDemo } from '../hero-terminal-demo'

export function HowItWorks() {
  const { t } = useTranslation()

  const steps = [
    {
      num: '1',
      title: t('Configure'),
      desc: t(
        'Add your API keys, set up channels and configure access permissions'
      ),
      icon: <Settings className='size-6' strokeWidth={1.5} />,
    },
    {
      num: '2',
      title: t('Connect'),
      desc: t(
        'Connect through OpenAI, Claude, Gemini, and other compatible API routes'
      ),
      icon: <Zap className='size-6' strokeWidth={1.5} />,
    },
    {
      num: '3',
      title: t('Monitor'),
      desc: t('Track usage, costs and performance with real-time analytics'),
      icon: <BarChart3 className='size-6' strokeWidth={1.5} />,
    },
  ]

  // Product walkthrough: the three steps run down a lit rail on the left
  // while the live API terminal stays in view on the right.
  return (
    <section className='relative z-10 px-6 pt-10 pb-24 md:pt-16 md:pb-32'>
      <div className='mx-auto grid max-w-6xl items-start gap-14 lg:grid-cols-12 lg:gap-12'>
        <div className='lg:col-span-5 lg:pt-6'>
          <AnimateInView>
            <p className='brand-eyebrow mb-3'>{t('How It Works')}</p>
            <h2 className='brand-display text-3xl md:text-[2.75rem]'>
              {t('Three steps to get started')}
            </h2>
          </AnimateInView>

          <div className='relative mt-12 space-y-10'>
            <div
              aria-hidden='true'
              className='brand-connector brand-connector--y start-6 top-6 bottom-6'
            />
            {steps.map((step, i) => (
              <AnimateInView
                key={step.num}
                delay={i * 150}
                animation='fade-up'
                className='relative flex items-start gap-5'
              >
                <div className='relative shrink-0'>
                  <div className='brand-icon-tile bg-card size-12 rounded-2xl'>
                    {step.icon}
                  </div>
                  <div className='bg-primary text-primary-foreground absolute -top-2 -right-2 flex size-5 items-center justify-center rounded-full font-mono text-[11px] font-bold shadow-[0_0_10px_var(--glow-color)]'>
                    {step.num}
                  </div>
                </div>
                <div className='pt-1'>
                  <h3 className='mb-1.5 text-base font-semibold'>
                    {step.title}
                  </h3>
                  <p className='text-muted-foreground max-w-sm text-sm leading-relaxed text-pretty wrap-break-word break-keep'>
                    {step.desc}
                  </p>
                </div>
              </AnimateInView>
            ))}
          </div>
        </div>

        <AnimateInView
          animation='fade-up'
          delay={150}
          className='relative lg:sticky lg:top-28 lg:col-span-7'
        >
          <BrandGlow />
          <HeroTerminalDemo />
        </AnimateInView>
      </div>
    </section>
  )
}
