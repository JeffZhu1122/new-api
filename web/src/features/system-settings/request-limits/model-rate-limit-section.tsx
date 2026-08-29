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

import { JsonCodeEditor } from '@/components/json-code-editor'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const RATE_LIMIT_MAX = 2147483647

const isPlainObject = (entry: unknown): entry is Record<string, unknown> =>
  typeof entry === 'object' && entry !== null && !Array.isArray(entry)

const isLimitValues = (entry: unknown) => {
  if (!isPlainObject(entry)) {
    return false
  }
  const { rpm, tpm, ...rest } = entry
  if (Object.keys(rest).length > 0) {
    return false
  }
  return [rpm, tpm].every(
    (value) =>
      value === undefined ||
      (typeof value === 'number' &&
        Number.isInteger(value) &&
        value >= 0 &&
        value <= RATE_LIMIT_MAX)
  )
}

const isLimitValuesMap = (entry: unknown) =>
  isPlainObject(entry) && Object.values(entry).every(isLimitValues)

const isValidRulesJSON = (value: string | undefined) => {
  if (!value || value.trim() === '') {
    return true
  }
  try {
    const parsed = JSON.parse(value)
    if (!isPlainObject(parsed)) {
      return false
    }
    const { default: defaults, models, groups, ...rest } = parsed
    if (Object.keys(rest).length > 0) {
      return false
    }
    if (defaults !== undefined && !isLimitValues(defaults)) {
      return false
    }
    if (models !== undefined && !isLimitValuesMap(models)) {
      return false
    }
    if (groups !== undefined) {
      if (!isPlainObject(groups)) {
        return false
      }
      for (const groupRules of Object.values(groups)) {
        if (!isPlainObject(groupRules)) {
          return false
        }
        const {
          default: groupDefaults,
          models: groupModels,
          ...groupRest
        } = groupRules
        if (Object.keys(groupRest).length > 0) {
          return false
        }
        if (groupDefaults !== undefined && !isLimitValues(groupDefaults)) {
          return false
        }
        if (groupModels !== undefined && !isLimitValuesMap(groupModels)) {
          return false
        }
      }
    }
    return true
  } catch {
    return false
  }
}

const createModelRateLimitSchema = (t: (key: string) => string) =>
  z.object({
    ModelRateLimitEnabled: z.boolean(),
    ModelRateLimitRules: z
      .string()
      .optional()
      .refine(isValidRulesJSON, {
        message: t('Invalid JSON format or values out of allowed range'),
      }),
  })

type ModelRateLimitFormValues = z.infer<
  ReturnType<typeof createModelRateLimitSchema>
>

type ModelRateLimitSectionProps = {
  defaultValues: ModelRateLimitFormValues
}

const RULES_PLACEHOLDER = `{
  "default": {"rpm": 60, "tpm": 200000},
  "models": {"claude-sonnet-5": {"rpm": 20, "tpm": 80000}},
  "groups": {
    "vip": {
      "default": {"rpm": 120, "tpm": 500000},
      "models": {"gpt-4o": {"rpm": 30, "tpm": 100000}}
    }
  }
}`

export function ModelRateLimitSection({
  defaultValues,
}: ModelRateLimitSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const schema = createModelRateLimitSchema(t)

  const form = useForm<ModelRateLimitFormValues>({
    resolver: zodResolver(schema),
    mode: 'onChange',
    defaultValues,
  })

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  const onSubmit = async (values: ModelRateLimitFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof ModelRateLimitFormValues]
    )

    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value: value ?? '' })
    }
  }

  return (
    <SettingsSection title={t('Model RPM/TPM Limits')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save RPM/TPM limits'
          />
          <FormField
            control={form.control}
            name='ModelRateLimitEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable RPM/TPM rate limiting')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Per-user RPM (requests/min) and TPM (tokens/min) limits by group and model. TPM is checked before the request and recorded after billing, so the last request in a window may overshoot.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='ModelRateLimitRules'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('RPM/TPM rules')}</FormLabel>
                <FormControl>
                  <JsonCodeEditor
                    value={field.value || ''}
                    onChange={field.onChange}
                    name={field.name}
                    onBlur={field.onBlur}
                    textareaRef={field.ref}
                    placeholder={RULES_PLACEHOLDER}
                    aria-invalid={Boolean(
                      form.formState.errors.ModelRateLimitRules
                    )}
                  />
                </FormControl>
                {/* Block-level help content stays outside FormDescription,
                    which renders a <p> and only allows phrasing content. */}
                <div className='text-muted-foreground space-y-1 text-xs'>
                  <p className='font-semibold'>{t('Format:')}</p>
                  <ul className='list-inside list-disc space-y-0.5 pl-2'>
                    <li>
                      {t(
                        'Priority: user override > group model > group default > global model > global default; RPM and TPM fall back independently'
                      )}
                    </li>
                    <li>
                      {t(
                        'Each limit counts per user; omit a field to inherit, 0 = unlimited'
                      )}
                    </li>
                    <li>
                      {t(
                        'Model names are the client-requested names (before channel model mapping); for "auto" group tokens use the literal "auto" key'
                      )}
                    </li>
                    <li>
                      {t(
                        'Per-user overrides are edited on the user management page'
                      )}
                    </li>
                  </ul>
                </div>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
