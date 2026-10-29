import { useState } from 'react'
import type { MarketEntry, PluginInfo } from './api'
import { Button } from '@/framework/ui/button'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Input } from '@/framework/ui/input'
import { Textarea } from '@/framework/ui/textarea'
import { useTranslation } from 'react-i18next'
import { Label } from '@/framework/ui/label'

interface Props {
  entry: MarketEntry | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onComplete?: () => void
}

/** 插件安装向导分步流程 */
export function PluginInstallWizard({ entry, open, onOpenChange, onComplete }: Props) {
  const { t } = useTranslation()
  const [step, setStep] = useState<'download' | 'build' | 'activate'>('download')
  const [progress, setProgress] = useState(0)
  const [licenseContent, setLicenseContent] = useState('')
  const [isPro, setIsPro] = useState(entry?.edition === 'pro' || false)

  // 免费版跳过授权步骤
  if (!isPro && step === 'activate') {
    setStep('build')
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(o) => !o && onOpenChange(false)}
      title={entry?.name ?? '插件安装'}
      footer={
        <>
          {step !== 'download' ? (
            <Button variant="outline" onClick={() => setStep(step === 'activate' ? 'build' : 'download')}>
              {t('common.back')}
            </Button>
          ) : null}
          <Button
            disabled={step !== 'download'}
            onClick={() => {
              // TODO: 实际调用后端 API
              setStep('build')
              // 模拟构建过程
              setTimeout(() => {
                setProgress(50)
                setTimeout(() => {
                  setIsPro(true)
                  setStep('activate')
                }, 2000)
              }, 2000)
            }}
          >
            {t('pluginMgr.market.install')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {step === 'download' && (
          <>
            <div className="text-sm text-muted-foreground">{t('pluginMgr.market.install.desc', { name: entry?.name })}</div>
            {isPro && (
              <div className="rounded-lg border bg-accent p-3 text-sm">
                <p>⚠️ Pro 版插件需要先购买授权：</p>
                <ul className="mt-2 list-disc pl-4 space-y-1">
                  <li>联系发行方获取 license 文件</li>
                  <li>或点击下方「购买授权」按钮</li>
                </ul>
              </div>
            )}
            {!isPro && (
              <div className="rounded-lg border bg-green-50 p-3 text-sm">
                <p>✅ 此插件为免费版本，无需授权，直接安装即可。</p>
              </div>
            )}
          </>
        )}

        {step === 'build' && (
          <>
            <div className="text-sm text-muted-foreground">正在构建插件（自动写入源码树并重新编译）...</div>
            <div className="h-2 w-full rounded-full bg-muted">
              <div className="h-2 rounded-full bg-primary transition-all" style={{ width: `${progress}%` }} />
            </div>
            <div className="text-xs text-muted-foreground text-center">{progress}%</div>
            {progress >= 100 && (
              <div className="text-sm text-green-600">
                ✅ 构建完成，即将激活插件...
              </div>
            )}
          </>
        )}

        {step === 'activate' && (
          <>
            {isPro ? (
              <>
                <Label htmlFor="license">导入授权文件 (`.lic`)</Label>
                <Textarea
                  id="license"
                  value={licenseContent}
                  onChange={(e) => setLicenseContent(e.target.value)}
                  placeholder="粘贴 .lic 授权内容..."
                  rows={5}
                  className="font-mono text-xs"
                />
                <div className="text-xs text-muted-foreground">
                  <p>如果不知道如何获取授权，请联系发行方。</p>
                  <p className="mt-1">注意：授权可能绑定域名或有效期，请确保与实际情况匹配。</p>
                </div>
              </>
            ) : (
              <div className="text-sm text-green-600">
                ✅ 免费版插件无需授权，已自动激活！
              </div>
            )}
          </>
        )}
      </div>
    </ResponsiveDialog>
  )
}
