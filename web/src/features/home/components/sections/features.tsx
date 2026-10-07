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
import {
  Zap,
  Shield,
  Globe,
  Code,
  Gauge,
  DollarSign,
  Users,
  HeartHandshake,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'

interface FeaturesProps {
  className?: string
}

export function Features(_props: FeaturesProps) {
  const { t } = useTranslation()

  const features = [
    {
      id: 'fast',
      num: '01',
      title: t('Lightning Fast'),
      desc: t(
        'Optimized network architecture ensures millisecond response times'
      ),
      tone: '[--tile-color:var(--chart-1)]',
      icon: <Zap className='size-5' strokeWidth={1.75} />,
      visual: (
        <div className='mt-4 grid grid-cols-3 gap-2'>
          {['OpenAI', 'Claude', 'Gemini', 'DeepSeek', 'Qwen', 'Llama'].map(
            (name) => (
              <div
                key={name}
                className='border-hairline bg-muted/40 text-muted-foreground hover:text-foreground flex items-center justify-center rounded-lg border px-3 py-2 font-mono text-xs transition-colors duration-300 hover:border-[color-mix(in_oklch,var(--tile-color)_40%,transparent)]'
              >
                {name}
              </div>
            )
          )}
        </div>
      ),
    },
    {
      id: 'secure',
      num: '02',
      title: t('Secure & Reliable'),
      desc: t(
        'Enterprise-grade security with comprehensive permission management'
      ),
      tone: '[--tile-color:var(--success)]',
      icon: <Shield className='size-5' strokeWidth={1.75} />,
      visual: (
        <div className='mt-4 flex items-center justify-center'>
          <div className='relative'>
            <div className='flex size-16 items-center justify-center rounded-2xl border border-[color-mix(in_oklch,var(--tile-color)_30%,transparent)] bg-[color-mix(in_oklch,var(--tile-color)_8%,transparent)]'>
              <Shield
                className='size-7 text-[var(--tile-color)]'
                strokeWidth={1.5}
              />
            </div>
            <div className='absolute -top-1 -right-1 flex size-4 items-center justify-center rounded-full bg-[var(--tile-color)]'>
              <svg
                className='text-success-foreground size-2.5'
                fill='none'
                viewBox='0 0 24 24'
                stroke='currentColor'
                strokeWidth={3}
              >
                <path
                  strokeLinecap='round'
                  strokeLinejoin='round'
                  d='m4.5 12.75 6 6 9-13.5'
                />
              </svg>
            </div>
          </div>
        </div>
      ),
    },
    {
      id: 'global',
      num: '03',
      title: t('Global Coverage'),
      desc: t('Multi-region deployment for stable global access'),
      tone: '[--tile-color:var(--chart-3)]',
      icon: <Globe className='size-5' strokeWidth={1.75} />,
      visual: (
        <div className='mt-4 space-y-2'>
          {[t('Load Balancing'), t('Rate Limiting'), t('Cost Tracking')].map(
            (step, i) => (
              <div key={step} className='flex items-center gap-2'>
                <div
                  className={`flex size-6 items-center justify-center rounded-full text-[10px] font-bold ${
                    i === 1
                      ? 'border border-[color-mix(in_oklch,var(--tile-color)_35%,transparent)] bg-[color-mix(in_oklch,var(--tile-color)_14%,transparent)] text-[var(--tile-color)]'
                      : 'border-border/40 bg-muted text-muted-foreground border'
                  }`}
                >
                  {i + 1}
                </div>
                <div className='bg-hairline-strong h-px flex-1' />
                <span className='text-muted-foreground text-xs'>{step}</span>
              </div>
            )
          )}
        </div>
      ),
    },
    {
      id: 'developer',
      num: '04',
      title: t('Developer Friendly'),
      desc: t('Compatible API routes for common AI application workflows'),
      tone: '[--tile-color:var(--chart-2)]',
      icon: <Code className='size-5' strokeWidth={1.75} />,
      visual: (
        <div className='mt-4 flex items-center gap-3'>
          <div className='flex -space-x-2'>
            {['API', 'SDK', 'CLI', 'Docs'].map((n) => (
              <div
                key={n}
                className='border-background from-muted to-muted/60 text-muted-foreground flex size-8 items-center justify-center rounded-full border-2 bg-gradient-to-br text-[9px] font-bold'
              >
                {n}
              </div>
            ))}
          </div>
          <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
            <Code className='size-3.5 text-[var(--tile-color)]' />
            {t('Multi-protocol Compatible')}
          </div>
        </div>
      ),
    },
  ]

  const additionalFeatures = [
    {
      icon: <Gauge className='size-5' strokeWidth={1.5} />,
      title: t('High Performance'),
      desc: t('Support for high concurrency with automatic load balancing'),
    },
    {
      icon: <DollarSign className='size-5' strokeWidth={1.5} />,
      title: t('Transparent Billing'),
      desc: t('Pay-as-you-go with real-time usage monitoring'),
    },
    {
      icon: <Users className='size-5' strokeWidth={1.5} />,
      title: t('Team Collaboration'),
      desc: t('Multi-user management with flexible permission allocation'),
    },
    {
      icon: <HeartHandshake className='size-5' strokeWidth={1.5} />,
      title: t('Open Source'),
      desc: t('Community driven, self-hosted, and extensible'),
    },
  ]

  // Sticky editorial column (heading + capability list) beside a 2x2 grid
  // of equal feature panels.
  return (
    <section className='border-hairline relative z-10 border-t px-6 py-24 md:py-32'>
      <div
        aria-hidden='true'
        className='brand-dots pointer-events-none absolute inset-0 -z-10'
      />
      <div className='mx-auto grid max-w-6xl items-start gap-12 lg:grid-cols-12 lg:gap-10'>
        <div className='lg:sticky lg:top-28 lg:col-span-4'>
          <AnimateInView>
            <p className='brand-eyebrow mb-3'>{t('Core Features')}</p>
            <h2 className='brand-display text-3xl leading-[1.08] md:text-[2.75rem]'>
              {t('Built for developers,')}
              <br />
              <span className='text-muted-foreground'>
                {t('designed for scale')}
              </span>
            </h2>
          </AnimateInView>

          {/* Additional capabilities, listed under the heading */}
          <div className='border-hairline mt-10 grid gap-x-6 border-t sm:grid-cols-2 lg:grid-cols-1'>
            {additionalFeatures.map((f, i) => (
              <AnimateInView
                key={f.title}
                delay={i * 100}
                animation='fade-up'
                className='border-hairline flex items-start gap-4 border-b py-5'
              >
                <div className='brand-icon-tile size-10'>{f.icon}</div>
                <div className='min-w-0'>
                  <h3 className='mb-1 text-sm font-semibold'>{f.title}</h3>
                  <p className='text-muted-foreground text-xs leading-relaxed text-pretty wrap-break-word break-keep'>
                    {f.desc}
                  </p>
                </div>
              </AnimateInView>
            ))}
          </div>
        </div>

        <div className='grid gap-4 sm:grid-cols-2 lg:col-span-8'>
          {features.map((f, i) => (
            <AnimateInView
              key={f.id}
              delay={i * 100}
              animation='scale-in'
              className={`brand-bezel ${f.tone}`}
            >
              <div className='brand-surface brand-sheen flex h-full flex-col p-7'>
                <div className='mb-4 flex items-center justify-between gap-3'>
                  {/* The number stays first in DOM order (read before the
                      title, as before the rebrand) and is shown on the right. */}
                  <span className='text-muted-foreground order-last font-mono text-[11px] tracking-[0.2em] tabular-nums'>
                    {f.num}
                  </span>
                  <span aria-hidden='true' className='brand-icon-tile size-10'>
                    {f.icon}
                  </span>
                </div>
                <h3 className='mb-2 text-lg font-semibold'>{f.title}</h3>
                <p className='text-muted-foreground text-sm leading-relaxed'>
                  {f.desc}
                </p>
                <div className='mt-auto pt-2'>{f.visual}</div>
              </div>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
