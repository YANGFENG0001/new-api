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
import { Loader2, RefreshCw, XCircle } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { handleServerError } from '@/lib/handle-server-error'

import {
  cancelBatchJob,
  getBatchJob,
  listBatchJobs,
  retryBatchJob,
} from '../batch/api'
import type { BatchJob, BatchJobItem } from '../batch/types'

const PAGE_SIZE = 20

function statusVariant(status: string) {
  switch (status) {
    case 'completed':
    case 'partial':
      return 'secondary' as const
    case 'failed':
      return 'destructive' as const
    case 'running':
      return 'default' as const
    case 'cancelled':
      return 'outline' as const
    default:
      return 'outline' as const
  }
}

export function BatchJobsSheet({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [jobs, setJobs] = useState<BatchJob[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [activeJob, setActiveJob] = useState<BatchJob | null>(null)
  const [items, setItems] = useState<BatchJobItem[]>([])
  const [busyId, setBusyId] = useState('')

  const load = useCallback(
    async (nextPage = page) => {
      setLoading(true)
      try {
        const result = await listBatchJobs({
          page: nextPage,
          page_size: PAGE_SIZE,
        })
        if (result.success && result.data) {
          setJobs(result.data.items)
          setTotal(result.data.total)
          setPage(result.data.page)
        } else if (result.message) {
          toast.error(result.message)
        }
      } catch (err) {
        handleServerError(err, t('Failed to load batch jobs'))
      } finally {
        setLoading(false)
      }
    },
    [page, t]
  )

  useEffect(() => {
    if (open) void load(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const openJob = async (job: BatchJob) => {
    setActiveJob(job)
    setItems([])
    try {
      const result = await getBatchJob(job.id)
      if (result.success && result.data) {
        setActiveJob(result.data.job)
        setItems(result.data.items)
      }
    } catch (err) {
      handleServerError(err, t('Failed to load batch job'))
    }
  }

  const handleRetry = async (job: BatchJob) => {
    setBusyId(job.id)
    try {
      const result = await retryBatchJob(job.id)
      if (result.success) {
        toast.success(t('Retry started'))
        await load()
        await openJob(job)
      } else {
        handleServerError(result, t('Retry failed'))
      }
    } catch (err) {
      handleServerError(err, t('Retry failed'))
    } finally {
      setBusyId('')
    }
  }

  const handleCancel = async (job: BatchJob) => {
    setBusyId(job.id)
    try {
      const result = await cancelBatchJob(job.id)
      if (result.success) {
        toast.success(t('Cancellation requested'))
        await load()
      } else {
        handleServerError(result, t('Cancel failed'))
      }
    } catch (err) {
      handleServerError(err, t('Cancel failed'))
    } finally {
      setBusyId('')
    }
  }

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className='flex w-full flex-col sm:max-w-2xl'>
        <SheetHeader>
          <SheetTitle>{t('Bulk action jobs')}</SheetTitle>
          <SheetDescription>
            {t(
              'Preview hash, per-user results, retry and cancellation are recorded here.'
            )}
          </SheetDescription>
        </SheetHeader>

        <div className='flex items-center gap-2'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => void load()}
            disabled={loading}
          >
            {loading ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <RefreshCw className='size-4' />
            )}
            {t('Refresh')}
          </Button>
          <span className='text-muted-foreground text-sm'>
            {t('{{count}} jobs', { count: total })}
          </span>
        </div>

        <ScrollArea className='min-h-0 flex-1 rounded-md border'>
          <div className='divide-y'>
            {jobs.length === 0 && !loading && (
              <p className='text-muted-foreground p-4 text-sm'>
                {t('No batch jobs yet')}
              </p>
            )}
            {jobs.map((job) => (
              <div key={job.id} className='space-y-2 p-3'>
                <div className='flex flex-wrap items-center gap-2'>
                  <Badge variant={statusVariant(job.status)}>
                    {job.status}
                  </Badge>
                  <span className='font-medium'>{job.action}</span>
                  <span className='text-muted-foreground text-xs'>
                    {job.id}
                  </span>
                  <div className='ml-auto flex gap-1'>
                    {job.status === 'running' || job.status === 'pending' ? (
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => void handleCancel(job)}
                        disabled={busyId === job.id}
                      >
                        <XCircle className='size-4' />
                        {t('Cancel')}
                      </Button>
                    ) : null}
                    {job.failed > 0 && job.status !== 'running' ? (
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => void handleRetry(job)}
                        disabled={busyId === job.id}
                      >
                        <RefreshCw className='size-4' />
                        {t('Retry failed')}
                      </Button>
                    ) : null}
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => void openJob(job)}
                    >
                      {t('Details')}
                    </Button>
                  </div>
                </div>
                <div className='text-muted-foreground flex flex-wrap gap-3 text-xs'>
                  <span>
                    {t('Total')}: {job.total}
                  </span>
                  <span className='text-emerald-600'>
                    {t('Succeeded')}: {job.succeeded}
                  </span>
                  <span className='text-destructive'>
                    {t('Failed')}: {job.failed}
                  </span>
                  <span>
                    {t('Skipped')}: {job.skipped}
                  </span>
                  <span>
                    {new Date(job.created_at * 1000).toLocaleString()}
                  </span>
                </div>
                {activeJob?.id === job.id && (
                  <div className='bg-muted/40 rounded-md p-2'>
                    {items.length === 0 ? (
                      <p className='text-muted-foreground text-xs'>
                        {t('No per-user results')}
                      </p>
                    ) : (
                      <ul className='space-y-1 text-xs'>
                        {items.map((item) => (
                          <li key={item.id} className='flex gap-2'>
                            <span className='w-16 shrink-0'>{item.status}</span>
                            <span className='shrink-0'>
                              {item.username} #{item.user_id}
                            </span>
                            <span className='text-muted-foreground'>
                              {item.error_message || ''}
                            </span>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </ScrollArea>

        <div className='flex items-center justify-between'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1 || loading}
            onClick={() => void load(page - 1)}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground text-sm'>
            {page} / {pages}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= pages || loading}
            onClick={() => void load(page + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  )
}
