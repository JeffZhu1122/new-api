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
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { PasswordInput } from '@/components/password-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { accountPasswordSchema } from '@/lib/password-policy'
import { AuthOperationError } from '@/lib/secure-verification'
import { createServerError } from '@/lib/server-error-message'

import { AuthLayout } from '../auth-layout'

export type ResetPasswordSearchParams = {
  email?: string
  token?: string
}

const resetPasswordSchema = z
  .object({
    password: accountPasswordSchema,
    confirmPassword: z.string(),
  })
  .refine((data) => data.password === data.confirmPassword, {
    message: 'Passwords do not match',
    path: ['confirmPassword'],
  })
type ResetPasswordValues = z.infer<typeof resetPasswordSchema>

// The owner of the mailbox chooses the new password here; the server neither
// generates one nor sends one back.
export function ResetPasswordConfirm(props: ResetPasswordSearchParams) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [completed, setCompleted] = useState(false)
  const form = useForm<ResetPasswordValues>({
    resolver: zodResolver(resetPasswordSchema),
    defaultValues: { password: '', confirmPassword: '' },
  })
  const isValidResetLink = Boolean(props.email && props.token)
  const submitting = form.formState.isSubmitting
  const backToLogin = () => navigate({ to: '/sign-in', replace: true })

  async function submit(values: ResetPasswordValues) {
    if (!props.email || !props.token) {
      toast.error(t('Invalid reset link, please request a new password reset'))
      return
    }
    try {
      const res = await api.post(
        '/api/user/reset',
        {
          email: props.email,
          token: props.token,
          password: values.password,
        },
        { skipBusinessError: true } as Record<string, unknown>
      )
      if (res?.data?.success) {
        form.reset()
        setCompleted(true)
        return
      }
      handleServerError(createServerError(res.data, t('Request failed')))
    } catch (error) {
      handleServerError(AuthOperationError.from(error))
    }
  }

  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-2'>
          <h2 className='text-center text-2xl font-semibold tracking-tight sm:text-left'>
            {t('Reset password')}
          </h2>
          <p className='text-muted-foreground text-left text-sm sm:text-base'>
            {completed
              ? t('auth.resetPasswordConfirm.success')
              : t('auth.resetPasswordConfirm.description')}
          </p>
        </div>

        {completed ? (
          <Button className='w-full' onClick={backToLogin}>
            {t('auth.resetPasswordConfirm.backToLogin')}
          </Button>
        ) : (
          <Form {...form}>
            <form
              onSubmit={form.handleSubmit(submit)}
              className='space-y-4'
              noValidate
            >
              {!isValidResetLink && (
                <Alert variant='destructive'>
                  <AlertDescription>
                    {t(
                      'Invalid reset link, please request a new password reset.'
                    )}
                  </AlertDescription>
                </Alert>
              )}

              <div className='space-y-2'>
                <Label htmlFor='email'>{t('Email')}</Label>
                <Input
                  id='email'
                  type='email'
                  autoComplete='username'
                  value={props.email || ''}
                  readOnly
                  disabled
                  placeholder={t('Waiting for email...')}
                />
              </div>

              <FormField
                control={form.control}
                name='password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('New Password')}</FormLabel>
                    <FormControl>
                      <PasswordInput
                        {...field}
                        autoComplete='new-password'
                        disabled={submitting || !isValidResetLink}
                      />
                    </FormControl>
                    <p className='text-muted-foreground text-xs'>
                      {t('Use 8–128 characters.')}
                    </p>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='confirmPassword'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Confirm New Password')}</FormLabel>
                    <FormControl>
                      <PasswordInput
                        {...field}
                        autoComplete='new-password'
                        disabled={submitting || !isValidResetLink}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <Button
                type='submit'
                className='w-full'
                disabled={submitting || !isValidResetLink}
              >
                {submitting && (
                  <Loader2 className='size-4 animate-spin' aria-hidden='true' />
                )}
                {t('auth.resetPasswordConfirm.confirm')}
              </Button>
              <Button
                type='button'
                variant='link'
                className='w-full'
                onClick={backToLogin}
              >
                {t('Back to login')}
              </Button>
            </form>
          </Form>
        )}
      </div>
    </AuthLayout>
  )
}
