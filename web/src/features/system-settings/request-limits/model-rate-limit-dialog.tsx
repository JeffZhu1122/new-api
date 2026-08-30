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
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

const RATE_LIMIT_MAX = 2147483647

const isEmptyOrLimitNumber = (value: string) => {
  if (value.trim() === '') return true
  const trimmed = value.trim()
  if (!/^\d+$/.test(trimmed)) return false
  return Number(trimmed) <= RATE_LIMIT_MAX
}

const modelRateLimitDialogSchema = z
  .object({
    group: z.string(),
    model: z.string(),
    rpm: z
      .string()
      .refine(isEmptyOrLimitNumber, 'Must be an integer within [0, 2147483647]'),
    tpm: z
      .string()
      .refine(isEmptyOrLimitNumber, 'Must be an integer within [0, 2147483647]'),
  })
  .refine((values) => values.rpm.trim() !== '' || values.tpm.trim() !== '', {
    message: 'At least one of RPM or TPM must be set',
    path: ['rpm'],
  })

type ModelRateLimitDialogFormValues = z.infer<typeof modelRateLimitDialogSchema>

const MODEL_RATE_LIMIT_FORM_ID = 'model-rate-limit-form'

export type ModelRateLimitRuleData = {
  group: string // '' = global
  model: string // '' = scope default
  rpm?: number
  tpm?: number
}

type ModelRateLimitDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: ModelRateLimitRuleData) => void
  editData?: ModelRateLimitRuleData | null
}

const toFormValues = (
  data: ModelRateLimitRuleData | null | undefined
): ModelRateLimitDialogFormValues => ({
  group: data?.group ?? '',
  model: data?.model ?? '',
  rpm: data?.rpm !== undefined ? String(data.rpm) : '',
  tpm: data?.tpm !== undefined ? String(data.tpm) : '',
})

export function ModelRateLimitDialog({
  open,
  onOpenChange,
  onSave,
  editData,
}: ModelRateLimitDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!editData

  const form = useForm<ModelRateLimitDialogFormValues>({
    resolver: zodResolver(modelRateLimitDialogSchema),
    defaultValues: toFormValues(editData),
  })

  useEffect(() => {
    form.reset(toFormValues(editData))
  }, [editData, form, open])

  const handleSubmit = (values: ModelRateLimitDialogFormValues) => {
    onSave({
      group: values.group.trim(),
      model: values.model.trim(),
      rpm: values.rpm.trim() === '' ? undefined : Number(values.rpm.trim()),
      tpm: values.tpm.trim() === '' ? undefined : Number(values.tpm.trim()),
    })
    form.reset()
    onOpenChange(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={isEditMode ? t('Edit RPM/TPM rule') : t('Add RPM/TPM rule')}
      description={t(
        'One row per scope: leave group empty for a global rule, leave model empty for the scope default.'
      )}
      contentClassName='sm:max-w-[500px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={MODEL_RATE_LIMIT_FORM_ID}>
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={MODEL_RATE_LIMIT_FORM_ID}
          onSubmit={form.handleSubmit(handleSubmit)}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='group'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Group')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('Global (all groups)')}
                    {...field}
                    disabled={isEditMode}
                  />
                </FormControl>
                <FormDescription>
                  {isEditMode
                    ? t('Scope cannot be changed when editing.')
                    : t(
                        'Leave empty to apply to all groups; use "auto" for auto-group tokens.'
                      )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='model'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Model')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('All models (default)')}
                    {...field}
                    disabled={isEditMode}
                  />
                </FormControl>
                <FormDescription>
                  {isEditMode
                    ? t('Scope cannot be changed when editing.')
                    : t(
                        'Leave empty for the scope default (applies to all models).'
                      )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='rpm'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('RPM (requests/min)')}</FormLabel>
                  <FormControl>
                    <Input
                      inputMode='numeric'
                      placeholder={t('Inherit')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Empty = inherit, 0 = unlimited.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='tpm'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('TPM (tokens/min)')}</FormLabel>
                  <FormControl>
                    <Input
                      inputMode='numeric'
                      placeholder={t('Inherit')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Empty = inherit, 0 = unlimited.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </form>
      </Form>
    </Dialog>
  )
}
