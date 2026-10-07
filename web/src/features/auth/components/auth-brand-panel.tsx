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
import { useTranslation } from 'react-i18next'

import { BrandBackdrop, BrandGlow, BrandPulse } from '@/components/brand'
import { Skeleton } from '@/components/ui/skeleton'
import { useSystemConfig } from '@/hooks/use-system-config'
import { cn } from '@/lib/utils'

type AuthBrandPanelProps = {
  className?: string
}

/**
 * Decorative brand panel beside the auth card (lg+ only). It reuses the
 * landing copy keys and holds no headings or controls, and its root is
 * aria-hidden (a plain div, not a landmark), so every auth screen keeps the
 * accessibility tree it had before: the site name as its only h1, its own h2
 * and nothing read after the form.
 */
export function AuthBrandPanel(props: AuthBrandPanelProps) {
  const { t } = useTranslation()
  const { logo, loading } = useSystemConfig()

  return (
    <div
      aria-hidden='true'
      data-slot='auth-brand-panel'
      className={cn(
        'brand-surface relative isolate m-3 flex-col justify-end overflow-hidden rounded-4xl p-10 pt-28 xl:p-14',
        props.className
      )}
    >
      {/* The sonar follows the hub, whose centre is the middle of the space
          above the copy: h/2 + (pt - pb - copy height)/2, i.e. about 60px (lg)
          and 108px (xl) above the panel middle for the en/zh copy. */}
      <BrandBackdrop
        variant='auth-panel'
        rings='sonar'
        flutes
        className='[--sonar-y:calc(50%-60px)] xl:[--sonar-y:calc(50%-108px)]'
      />
      {/* Gateway hub: protocol names orbit the admin logo (decorative). It
          fills the space above the copy and is sized by both axes, keeping
          28px (half a chip plus a gap) clear above and below, so the orbit
          and its chips never reach the copy; it is dropped when that space
          is too short for a legible orbit. */}
      <div
        aria-hidden='true'
        data-slot='auth-brand-hub'
        className='@container-size relative min-h-0 flex-1'
      >
        <div
          data-slot='auth-brand-orbit'
          className='absolute top-1/2 left-1/2 size-[min(30vw,420px,100cqh-56px)] -translate-x-1/2 -translate-y-1/2 [@container(max-height:255px)]:hidden'
        >
          <div className='brand-orbit'>
            <span className='brand-orbit-chip brand-chip top-0 left-1/2 font-mono text-[11px]'>
              Chat
            </span>
            <span className='brand-orbit-chip brand-chip top-1/2 left-full font-mono text-[11px]'>
              Claude
            </span>
            <span className='brand-orbit-chip brand-chip top-full left-1/2 font-mono text-[11px]'>
              Gemini
            </span>
            <span className='brand-orbit-chip brand-chip top-1/2 left-0 font-mono text-[11px]'>
              Responses
            </span>
          </div>
          <div className='absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2'>
            <BrandGlow className='[--glow-inset:-70%]' />
            <div className='brand-glass grid size-24 place-items-center rounded-full'>
              {loading ? (
                <Skeleton className='size-12 rounded-full' />
              ) : (
                <img src={logo} alt='' className='size-12 object-contain' />
              )}
            </div>
          </div>
        </div>
      </div>
      {/* The copy column is wide enough for each display line to stay on one
          line; the description keeps its own reading measure and only breaks
          CJK text at punctuation and spaces. */}
      <div data-slot='auth-brand-copy' className='relative max-w-xl space-y-5'>
        <p className='brand-chip flex w-fit'>
          <BrandPulse />
          {t('AI Application Infrastructure Foundation')}
        </p>
        <p className='brand-display text-[clamp(2rem,3vw,2.75rem)]'>
          {t('Unified API Gateway for')}
          <br />
          <span className='brand-text-aurora'>
            {t('Vast Range of AI Models')}
          </span>
        </p>
        <p className='text-muted-foreground max-w-md text-[15px] leading-relaxed text-pretty wrap-anywhere break-keep'>
          {t(
            'Access a vast selection of models via a standard, unified API protocol. Power AI applications, manage digital assets, and connect the Future.'
          )}
        </p>
      </div>
    </div>
  )
}
