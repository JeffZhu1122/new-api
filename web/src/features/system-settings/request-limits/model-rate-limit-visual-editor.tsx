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
import { Plus, Search } from 'lucide-react'
import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isObjectRecord } from '../utils/json-validators'
import {
  ModelRateLimitDialog,
  type ModelRateLimitRuleData,
} from './model-rate-limit-dialog'

type LimitValues = { rpm?: number; tpm?: number }
type GroupRules = { default?: LimitValues; models?: Record<string, LimitValues> }
type RulesConfig = {
  default?: LimitValues
  models?: Record<string, LimitValues>
  groups?: Record<string, GroupRules>
}

type ModelRateLimitVisualEditorProps = {
  value: string
  onChange: (value: string) => void
}

const hasLimit = (values: LimitValues | undefined): values is LimitValues =>
  !!values && (values.rpm !== undefined || values.tpm !== undefined)

const parseRules = (value: string): RulesConfig => {
  if (!value || value.trim() === '') return {}
  return safeJsonParseWithValidation<RulesConfig>(value, {
    fallback: {},
    validator: isObjectRecord,
    silent: true,
  })
}

const rulesToRows = (config: RulesConfig): ModelRateLimitRuleData[] => {
  const rows: ModelRateLimitRuleData[] = []
  if (hasLimit(config.default)) {
    rows.push({ group: '', model: '', ...config.default })
  }
  for (const [model, values] of Object.entries(config.models ?? {})) {
    if (hasLimit(values)) {
      rows.push({ group: '', model, ...values })
    }
  }
  const groupNames = Object.keys(config.groups ?? {}).sort()
  for (const group of groupNames) {
    const groupRules = config.groups?.[group]
    if (!groupRules) continue
    if (hasLimit(groupRules.default)) {
      rows.push({ group, model: '', ...groupRules.default })
    }
    for (const [model, values] of Object.entries(groupRules.models ?? {}).sort(
      ([a], [b]) => a.localeCompare(b)
    )) {
      if (hasLimit(values)) {
        rows.push({ group, model, ...values })
      }
    }
  }
  return rows
}

const toLimitValues = (rule: ModelRateLimitRuleData): LimitValues => {
  const values: LimitValues = {}
  if (rule.rpm !== undefined) values.rpm = rule.rpm
  if (rule.tpm !== undefined) values.tpm = rule.tpm
  return values
}

const setRule = (config: RulesConfig, rule: ModelRateLimitRuleData) => {
  const values = toLimitValues(rule)
  if (rule.group === '') {
    if (rule.model === '') {
      config.default = values
      return
    }
    config.models = { ...config.models, [rule.model]: values }
    return
  }
  const groupRules: GroupRules = config.groups?.[rule.group] ?? {}
  if (rule.model === '') {
    groupRules.default = values
  } else {
    groupRules.models = { ...groupRules.models, [rule.model]: values }
  }
  config.groups = { ...config.groups, [rule.group]: groupRules }
}

const deleteRule = (config: RulesConfig, group: string, model: string) => {
  if (group === '') {
    if (model === '') {
      delete config.default
    } else if (config.models) {
      delete config.models[model]
      if (Object.keys(config.models).length === 0) delete config.models
    }
    return
  }
  const groupRules = config.groups?.[group]
  if (!groupRules) return
  if (model === '') {
    delete groupRules.default
  } else if (groupRules.models) {
    delete groupRules.models[model]
    if (Object.keys(groupRules.models).length === 0) delete groupRules.models
  }
  if (groupRules.default === undefined && groupRules.models === undefined) {
    delete config.groups?.[group]
  }
  if (config.groups && Object.keys(config.groups).length === 0) {
    delete config.groups
  }
}

const serializeRules = (config: RulesConfig): string => {
  if (
    config.default === undefined &&
    config.models === undefined &&
    config.groups === undefined
  ) {
    return ''
  }
  return JSON.stringify(config, null, 2)
}

const ruleKey = (rule: ModelRateLimitRuleData) =>
  JSON.stringify([rule.group, rule.model])

export function ModelRateLimitVisualEditor({
  value,
  onChange,
}: ModelRateLimitVisualEditorProps) {
  const { t } = useTranslation()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<ModelRateLimitRuleData | null>(null)

  const rules = useMemo(() => rulesToRows(parseRules(value)), [value])

  const filteredRules = useMemo(() => {
    if (!searchText) return rules
    const lowerSearch = searchText.toLowerCase()
    return rules.filter(
      (rule) =>
        rule.group.toLowerCase().includes(lowerSearch) ||
        rule.model.toLowerCase().includes(lowerSearch)
    )
  }, [rules, searchText])

  const handleSave = (data: ModelRateLimitRuleData) => {
    const config = parseRules(value)
    if (
      editData &&
      (editData.group !== data.group || editData.model !== data.model)
    ) {
      deleteRule(config, editData.group, editData.model)
    }
    setRule(config, data)
    onChange(serializeRules(config))
  }

  const handleDelete = (rule: ModelRateLimitRuleData) => {
    const config = parseRules(value)
    deleteRule(config, rule.group, rule.model)
    onChange(serializeRules(config))
  }

  const handleEdit = (rule: ModelRateLimitRuleData) => {
    setEditData(rule)
    setDialogOpen(true)
  }

  const handleAdd = () => {
    setEditData(null)
    setDialogOpen(true)
  }

  const renderLimit = (limit: number | undefined) => {
    if (limit === undefined) {
      return <span className='text-muted-foreground'>{t('Inherit')}</span>
    }
    return (
      <span className='font-mono'>
        {limit === 0 ? t('Unlimited') : limit.toLocaleString()}
      </span>
    )
  }

  return (
    <div className='space-y-4'>
      <div className='flex items-center gap-4'>
        <div className='relative flex-1'>
          <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' />
          <Input
            placeholder={t('Search groups or models...')}
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            className='pl-9'
          />
        </div>
        <Button onClick={handleAdd}>
          <Plus className='mr-2 h-4 w-4' />
          {t('Add rule')}
        </Button>
      </div>

      <StaticDataTable
        data={filteredRules}
        getRowKey={ruleKey}
        emptyContent={
          searchText
            ? t('No rules match your search')
            : t('No RPM/TPM rules configured. Click "Add rule" to get started.')
        }
        columns={[
          {
            id: 'group',
            header: t('Group'),
            cellClassName: 'font-medium',
            cell: (rule) =>
              rule.group === '' ? (
                <Badge variant='secondary'>{t('Global (all groups)')}</Badge>
              ) : (
                rule.group
              ),
          },
          {
            id: 'model',
            header: t('Model'),
            cell: (rule) =>
              rule.model === '' ? (
                <Badge variant='outline'>{t('All models (default)')}</Badge>
              ) : (
                <span className='font-mono'>{rule.model}</span>
              ),
          },
          {
            id: 'rpm',
            header: t('RPM (requests/min)'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (rule) => renderLimit(rule.rpm),
          },
          {
            id: 'tpm',
            header: t('TPM (tokens/min)'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (rule) => renderLimit(rule.tpm),
          },
          {
            id: 'actions',
            header: t('Actions'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (rule) => (
              <StaticRowActions
                editLabel={t('Edit')}
                deleteLabel={t('Delete')}
                menuLabel={t('Open menu')}
                onEdit={() => handleEdit(rule)}
                onDelete={() => handleDelete(rule)}
              />
            ),
          },
        ]}
      />

      <ModelRateLimitDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
      />
    </div>
  )
}
