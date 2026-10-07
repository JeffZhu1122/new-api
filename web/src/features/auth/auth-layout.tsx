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
import { useTranslation } from 'react-i18next'

import { BrandBackdrop } from '@/components/brand'
import { Skeleton } from '@/components/ui/skeleton'
import { useSystemConfig } from '@/hooks/use-system-config'

import { AuthBrandPanel } from './components/auth-brand-panel'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation()
  const { systemName, logo, loading } = useSystemConfig()

  // DOM order is home link -> form card -> brand panel (aria-hidden
  // decoration with no controls), so the form comes first in reading and
  // focus order; the grid shows the panel on the left at lg+ and hides it
  // below that (OAuth popups included).
  // From lg the page backdrop starts under the panel edge and fades in over
  // 80px (no seam in the margins around the panel), and its indigo peak sits
  // behind the card's top-right corner so the glass has colour to frost.
  // The 480px card keeps the original 416px text measure at sm:p-8.
  return (
    <div className='relative isolate grid min-h-svh lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]'>
      <BrandBackdrop
        variant='auth'
        className='lg:start-1/2 lg:[mask-image:linear-gradient(to_right,transparent,#000_80px)] lg:[--mesh-p1:82%_26%] lg:[--mesh-s1:46%_44%] lg:[&>.brand-mesh]:opacity-100'
      />
      <Link
        to='/'
        className='absolute start-4 top-4 z-20 flex items-center gap-2.5 transition-opacity hover:opacity-80 sm:start-8 sm:top-8'
      >
        <div className='relative size-8'>
          {loading ? (
            <Skeleton className='absolute inset-0 rounded-lg' />
          ) : (
            <img
              src={logo}
              alt={t('Logo')}
              className='size-8 rounded-lg object-contain'
            />
          )}
        </div>
        {loading ? (
          <Skeleton className='h-6 w-24' />
        ) : (
          <h1 className='font-display text-xl font-semibold tracking-tight'>
            {systemName}
          </h1>
        )}
      </Link>
      <div className='relative flex items-center justify-center px-4 pt-20 pb-10 sm:px-8 lg:col-start-2 lg:row-start-1 lg:py-12'>
        <div
          data-slot='auth-card'
          className='brand-glass brand-hairline relative flex w-full max-w-[480px] flex-col justify-center space-y-2 rounded-3xl p-4 py-6 sm:p-8 [:where(&_p)]:text-pretty'
        >
          {children}
        </div>
      </div>
      <AuthBrandPanel className='hidden lg:col-start-1 lg:row-start-1 lg:flex' />
    </div>
  )
}
