import { useCallback, useEffect, useRef, useState } from 'react'
import { api, apiURL } from './api.js'
import { executionDuration, queueDuration, formatDuration, recordedDuration } from './timing.js'

const appTypes = { full_stack: '全栈应用', frontend: '前端应用', html: '创意应用' }
const statuses = {
  pending: '待执行', queued: '排队中', running: '进行中', success: '完成',
  failed: '失败', skipped: '已跳过', warning: '需检查', blocked: '未执行',
}

function Status({ status }) {
  return <span className={'pill ' + status}>{statuses[status] || status}</span>
}

function PackageDownload({ job, className }) {
  if (job.kind !== 'package' || job.status !== 'success') return null
  const href = apiURL('jobs/' + job.id + '/download', { profile: job.profile })
  return <a className={className} href={href}>下载安装包</a>
}

function formatTime(value) {
  return new Intl.DateTimeFormat('zh-CN', {
    month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit',
  }).format(new Date(value))
}

function formatSize(bytes) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

function queueRetry(job) {
  let resumeIndex = job.steps.findIndex(({ status }) => status === 'failed')
  // 兼容没有明确失败节点的中断任务。
  if (resumeIndex < 0) {
    resumeIndex = job.steps.findIndex(({ status }) => status === 'blocked' || status === 'pending')
  }
  const resumeFrom = resumeIndex >= 0 ? job.steps[resumeIndex].key : job.resumeFrom
  return {
    ...job,
    status: 'queued',
    message: '',
    failure: null,
    resumeFrom,
    timingIncomplete: job.timingIncomplete || (!job.queuedAt && !job.startedAt),
    queuedAt: new Date().toISOString(),
    startedAt: null,
    finishedAt: null,
    steps: resumeIndex < 0 ? job.steps
      : job.steps.map((step, index) => index < resumeIndex ? step : {
        ...step, status: 'pending', detail: '', startedAt: null, finishedAt: null,
      }),
  }
}

export default function Workspace({ profile, page, onAccount }) {
  const selectionKey = 'miaoda-selected-job:' + page + ':' + profile.name
  const [jobs, setJobs] = useState([])
  const [jobID, setJobID] = useState(() => sessionStorage.getItem(selectionKey))
  const [error, setError] = useState('')
  const [pollError, setPollError] = useState('')
  const [retrying, setRetrying] = useState(false)
  const [revision, setRevision] = useState(0)
  const retryingRef = useRef(false)
  const isPackage = page === 'package'
  const current = jobID === '' ? undefined : (
    jobs.find(({ id }) => id === jobID)
    ?? jobs.find(({ status }) => status === 'running')
    ?? jobs.find(({ status }) => status === 'queued')
    ?? (isPackage ? undefined : jobs[0])
  )
  const currentID = current?.id

  const selectJob = useCallback((id) => {
    sessionStorage.setItem(selectionKey, id)
    setJobID(id)
  }, [selectionKey])

  useEffect(() => {
    if (currentID && currentID !== jobID) selectJob(currentID)
  }, [currentID, jobID, selectJob])

  useEffect(() => {
    const controller = new AbortController()
    let timer
    async function refresh() {
      try {
        const data = await api('jobs', { params: { profile: profile.name, kind: page }, signal: controller.signal })
        if (controller.signal.aborted) return
        // 重试请求期间保留即时状态，避免轮询回写旧数据。
        if (!retryingRef.current) setJobs(data)
        setPollError('')
      } catch (err) {
        if (!controller.signal.aborted) setPollError(err.message)
      }
      if (!controller.signal.aborted) timer = setTimeout(refresh, 1500)
    }
    void refresh()
    return () => {
      controller.abort()
      clearTimeout(timer)
    }
  }, [page, profile.name, revision])

  const showJob = useCallback((job) => {
    setJobs((items) => [job, ...items.filter(({ id }) => job.id !== id)])
    selectJob(job.id)
    setRevision((n) => n + 1)
  }, [selectJob])

  async function retry() {
    const previous = current
    setError('')
    setRetrying(true)
    retryingRef.current = true
    setJobs((items) => items.map((item) => item.id === current.id ? queueRetry(item) : item))
    try {
      const job = await api('jobs/' + current.id + '/retry', { params: { profile: profile.name }, data: {} })
      showJob(job)
    } catch (err) {
      setJobs((items) => items.map((item) => item.id === previous.id ? previous : item))
      setError(err.message)
    } finally {
      retryingRef.current = false
      setRetrying(false)
    }
  }

  return (
    <section className="workspace">
      <div className="page-heading">
        <h1>{isPackage ? '应用打包' : '应用部署'}</h1>
        <p>{isPackage ? '按名称搜索当前租户可见的全栈应用，生成可导入的 ZIP 安装包。'
          : '导入 ZIP 安装包，确认应用名称后部署到当前租户。'}</p>
      </div>
      <div className={'work-grid' + (current ? ' has-progress' : '')}>
        <div className="work-primary">
          {isPackage ? <PackagePanel profile={profile.name} onJob={showJob} onAccount={onAccount} />
            : <DeployPanel profile={profile.name} onJob={showJob} />}
          <section className="history">
            <div className="section-heading"><h2>{isPackage ? '打包记录' : '部署记录'}</h2></div>
            {(error || pollError) && <p className="notice danger" role="alert">{error || pollError}</p>}
            {jobs.length === 0 ? <p className="muted empty-history">暂无任务</p> : (
              <ul className="history-list">
                {jobs.map((job) => (
                  <li key={job.id} className={currentID === job.id ? 'selected-job' : undefined}>
                    <button type="button" onClick={() => selectJob(job.id)}>
                      <span className="history-name">{job.name}</span>
                      <time dateTime={job.createdAt}>{formatTime(job.createdAt)}</time>
                      <span className="history-duration" title="累计执行耗时，不含排队">
                        {recordedDuration(job, executionDuration(job))}
                      </span>
                      <Status status={job.status} />
                    </button>
                    <PackageDownload job={job} className="button secondary history-download" />
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>
        {current && <JobProgress job={current} onRetry={retry} retrying={retrying} onClose={() => selectJob('')} />}
      </div>
    </section>
  )
}

function PackagePanel({ profile, onJob, onAccount }) {
  const [keyword, setKeyword] = useState('')
  const [apps, setApps] = useState([])
  const [cursor, setCursor] = useState('')
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)
  const request = useRef(null)
  const query = keyword.trim()

  useEffect(() => {
    if (!query) return
    const controller = new AbortController()
    request.current?.abort()
    request.current = controller
    setLoading(true)
    setError('')
    const timer = setTimeout(async () => {
      try {
        const data = await api('apps', { params: { profile, keyword: query }, signal: controller.signal })
        if (controller.signal.aborted) return
        setApps(data.items || [])
        setCursor(data.cursor)
      } catch (err) {
        if (!controller.signal.aborted) {
          setApps([])
          setCursor('')
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }, 300)
    return () => {
      clearTimeout(timer)
      request.current?.abort()
    }
  }, [query, profile, revision])

  function changeKeyword(event) {
    const value = event.target.value
    const nextQuery = value.trim()
    if (nextQuery !== query) {
      request.current?.abort()
      setApps([])
      setCursor('')
      setError('')
      setLoading(Boolean(nextQuery))
    }
    setKeyword(value)
  }

  async function loadMore() {
    const controller = new AbortController()
    request.current?.abort()
    request.current = controller
    setLoading(true)
    setError('')
    try {
      const data = await api('apps', {
        params: { profile, keyword: query, cursor }, signal: controller.signal,
      })
      if (controller.signal.aborted) return
      setApps((prev) => {
        const seen = new Set(prev.map(({ app_id }) => app_id))
        return [...prev, ...(data.items || []).filter(({ app_id }) => !seen.has(app_id))]
      })
      setCursor(data.cursor)
    } catch (err) {
      if (!controller.signal.aborted) setError(err.message)
    } finally {
      if (!controller.signal.aborted) setLoading(false)
    }
  }

  async function packageApp(appId) {
    setBusy(appId)
    setError('')
    try {
      onJob(await api('package', { data: { profile, appId } }))
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy('')
    }
  }

  return (
    <section className="panel" aria-labelledby="app-list">
      <div className="panel-header app-search-header">
        <h2 id="app-list">搜索全栈应用</h2>
        <label className="search">
          <span className="search-icon" aria-hidden="true" />
          <input type="search" aria-label="搜索应用名称" placeholder="输入应用名称" value={keyword}
            onChange={changeKeyword} />
        </label>
      </div>
      {error && (
        <div className="list-notice notice danger" role="alert">
          <p>{error}</p>
          <div className="button-row">
            <button className="button secondary" onClick={() => setRevision((n) => n + 1)}>重试</button>
            <button className="button secondary" onClick={onAccount}>切换或授权账号</button>
          </div>
        </div>
      )}
      {apps.length > 0 && <div className="table-wrap" aria-busy={loading}>
        <table>
          <thead><tr><th>应用名称</th><th className="align-right">操作</th></tr></thead>
          <tbody>
            {apps.map((app) => (
              <tr key={app.app_id}>
                <td><div className="app-cell">
                  <span className="app-icon" aria-hidden="true">{(app.name || '妙').slice(0, 1)}</span>
                  <span><strong>{app.name || app.app_id}</strong><small>{app.app_id}</small></span>
                </div></td>
                <td className="align-right">
                  <button className="button table-button" disabled={!!busy || loading} onClick={() => packageApp(app.app_id)}>
                    {busy === app.app_id ? '正在创建…' : '打包'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>}
      {query && apps.length === 0 && !error && <p className="list-empty muted" role="status">
        {loading ? '正在搜索…' : '未找到匹配的全栈应用'}
      </p>}
      {cursor && <div className="panel-footer"><button className="button secondary" onClick={loadMore} disabled={loading}>
        {loading ? '正在加载…' : '加载更多'}
      </button></div>}
    </section>
  )
}

function DeployPanel({ profile, onJob }) {
  const [attachment, setAttachment] = useState(null)
  const [name, setName] = useState('')
  const [targetUrl, setTargetUrl] = useState('')
  const [overwritePreview, setOverwritePreview] = useState(null)
  const [confirmedId, setConfirmedId] = useState('')
  const [uploading, setUploading] = useState(false)
  const [deploying, setDeploying] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState('')
  const fileInput = useRef(null)
  const overwrite = targetUrl.trim() !== ''

  function clearOverwritePreview() {
    setOverwritePreview(null)
    setConfirmedId('')
  }

  async function upload(file) {
    if (!file || uploading || deploying) return
    setError('')
    if (!file.name.toLowerCase().endsWith('.zip') || file.size > 256 * 1024 * 1024) {
      setError('请选择不超过 256 MB 的 ZIP 安装包。')
      return
    }
    setUploading(true)
    try {
      const data = new FormData()
      data.append('file', file)
      const result = await api('imports', { data })
      setAttachment(result)
      clearOverwritePreview()
      // 保留用户已输入的目标名称。
      setName((current) => current.trim() ? current : (result.meta.app.name || ''))
    } catch (err) {
      setError(err.message)
    } finally {
      setUploading(false)
    }
  }

  async function deploy(event) {
    event.preventDefault()
    if (uploading || deploying) return
    setError('')
    // 未上传文件时阻止空任务提交。
    if (!attachment) {
      setError('请先选择 ZIP 安装包。')
      return
    }
    setDeploying(true)
    try {
      const data = { profile, importId: attachment.id, name: name.trim() }
      if (overwrite) {
        data.targetUrl = targetUrl.trim()
        if (!overwritePreview) {
          setOverwritePreview(await api('deploy/overwrite-preview', { data }))
          return
        }
        if (confirmedId.trim() !== overwritePreview.plan.app.app_id) return
        data.confirmation = overwritePreview.confirmation
        data.confirmedId = confirmedId.trim()
      }
      onJob(await api('deploy', { data }))
      setAttachment(null)
      setName('')
      setTargetUrl('')
      clearOverwritePreview()
    } catch (err) {
      setError(err.message)
      if (overwrite) clearOverwritePreview()
    } finally {
      setDeploying(false)
    }
  }

  return (
    <section className="panel import-panel" aria-labelledby="import-heading">
      <div className="panel-header"><h2 id="import-heading">导入安装包</h2></div>
      <div className="import-body">
        <input ref={fileInput} type="file" accept=".zip,application/zip" hidden onChange={(event) => {
          const file = event.target.files[0]
          event.target.value = ''
          void upload(file)
        }} />
        <form onSubmit={deploy}>
          <label className="field deploy-name">应用名称
            <input value={overwrite ? overwritePreview?.plan.app.name || '' : name}
              onChange={(event) => setName(event.target.value)} maxLength={100} required={!overwrite}
              readOnly={overwrite} placeholder={overwrite ? '检查后显示目标应用名称' : '输入部署后的应用名称'} />
          </label>
          <label className="field deploy-target">妙搭应用地址（可选）
            <input type="url" value={targetUrl} maxLength={2048} disabled={deploying} onChange={(event) => {
              setTargetUrl(event.target.value)
              clearOverwritePreview()
            }} placeholder="https://miaoda.feishu.cn/app/app_…" />
            <small>填写已有应用地址可覆盖该应用。留空时创建新应用。</small>
          </label>
          {error && <p className="notice danger" role="alert">{error}</p>}
          {attachment ? (
            <>
            <div className="imported">
              <span className="file-icon" aria-hidden="true">ZIP</span>
              <span className="imported-name"><strong>{attachment.name}</strong>
                <small>{formatSize(attachment.size)} · {appTypes[attachment.meta.app.app_type] || '妙搭应用'}</small>
              </span>
              <button type="button" className="button secondary" disabled={uploading || deploying} onClick={() => fileInput.current.click()}>
                更换文件
              </button>
            </div>
            {overwritePreview && <OverwriteReview profile={profile} plan={overwritePreview.plan}
              confirmedId={confirmedId} onConfirm={setConfirmedId} />}
            {!overwrite && <p className="deploy-summary">确认后将在 <strong>{profile}</strong> 创建应用并发布。</p>}
            <button className="button primary" disabled={uploading || deploying || (
              overwrite ? Boolean(overwritePreview && confirmedId.trim() !== overwritePreview.plan.app.app_id) : !name.trim()
            )}>
              {overwrite && !overwritePreview
                ? (deploying ? '正在检查目标…' : '检查目标应用')
                : (deploying ? '正在创建任务…' : overwrite ? '确认覆盖并部署' : '确认部署')}
            </button>
            </>
          ) : (
            <div className={'dropzone' + (dragging ? ' dragging' : '')}
              onDragOver={(event) => { event.preventDefault(); setDragging(true) }}
              onDragLeave={() => setDragging(false)}
              onDrop={(event) => {
                event.preventDefault()
                setDragging(false)
                void upload(event.dataTransfer.files[0])
              }}>
              <span className="upload-mark" aria-hidden="true">↑</span>
              <strong>{uploading ? '正在上传并校验…' : '拖放 ZIP 安装包到这里'}</strong>
              <p className="muted">支持标准妙搭应用 ZIP，最大 256 MB</p>
              <button type="button" className="button secondary" disabled={uploading}
                onClick={() => fileInput.current.click()}>选择文件</button>
            </div>
          )}
        </form>
      </div>
    </section>
  )
}

function OverwriteReview({ profile, plan, confirmedId, onConfirm }) {
  const { app, devTables, onlineTables, devEnums, roles, automations, devEnv, onlineEnv } = plan
  const tableNames = (tables) => tables?.map(({ name, estimatedRows }) => (
    estimatedRows >= 0 ? name + '（约 ' + estimatedRows + ' 行）' : name
  )) || []
  const rows = [
    ['删除开发库业务表', tableNames(devTables)],
    ['删除开发库枚举', devEnums],
    ['清空生产库业务表数据', tableNames(onlineTables)],
    ['删除自定义角色及成员关系', roles?.map((role) => (
      role.name + '（用户 ' + role.users + '、部门 ' + role.departments + '、群 ' + role.chats + '）'
    ))],
    ['停用旧自动化', automations],
    ['删除开发环境变量', devEnv],
    ['删除生产环境变量', onlineEnv],
  ].filter(([, items]) => items?.length)

  return (
    <section className="overwrite-review" aria-labelledby="overwrite-review-heading">
      <h3 id="overwrite-review-heading">覆盖 {app.name}</h3>
      <p>当前账号 {profile} · 目标应用 {app.app_id}</p>
      <p>确认后会先初始化目标应用，再覆盖代码并发布。表数据、角色和变量的清理不可撤销。</p>
      <ul>
        {rows.map(([label, items]) => (
          <li key={label}><strong>{label}（{items.length}）</strong><span>{items.join('、')}</span></li>
        ))}
      </ul>
      <label className="field">输入目标应用 ID 确认覆盖
        <input value={confirmedId} onChange={(event) => onConfirm(event.target.value)}
          placeholder={app.app_id} autoComplete="off" />
      </label>
    </section>
  )
}

function ProductionLink({ url }) {
  const [copyResult, setCopyResult] = useState('')

  async function copyLink() {
    try {
      await navigator.clipboard.writeText(url)
      setCopyResult('链接已复制')
    } catch {
      setCopyResult('复制失败，请选中地址后手动复制。')
    }
  }

  return (
    <section className="production-link" aria-labelledby="production-link-heading">
      <h3 id="production-link-heading">生产环境地址</h3>
      {url ? (
        <div className="production-link-row">
          <a href={url} target="_blank" rel="noreferrer">{url}</a>
          <button type="button" className="button secondary" onClick={copyLink}>复制链接</button>
        </div>
      ) : <p className="muted">本次发布记录暂无访问地址。</p>}
      <p className="production-copy-result" role="status">{copyResult}</p>
    </section>
  )
}

function JobProgress({ job, onRetry, retrying, onClose }) {
  const active = job.status === 'running' || job.status === 'queued'
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    // 仅活动任务更新本地时钟，不增加接口轮询。
    if (!active) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [active, job.id])
  const timestamp = active ? Math.max(now, Date.parse(job.startedAt || job.queuedAt) || 0) : Date.now()
  const totalDuration = executionDuration(job, timestamp)
  const waitDuration = queueDuration(job, timestamp)
  const durations = job.steps.map((step) => executionDuration(step, timestamp))
  const slowestIndex = durations.reduce((best, duration, index) => (
    duration != null && duration > 0 && (best < 0 || duration > durations[best]) ? index : best
  ), -1)
  const advanced = job.steps.filter(({ status }) => ['success', 'warning', 'skipped'].includes(status)).length
  const failed = job.status === 'failed'
  const deployed = job.kind === 'deploy' && job.status === 'success'
  const failedStep = job.failure?.step || job.steps.find(({ status }) => status === 'failed')?.label
  const message = job.message || (failed ? job.failure?.summary : '')
  const retryStep = failedStep || job.resumeFrom
  const targetURL = job.targetId && 'https://miaoda.feishu.cn/app/' + job.targetId
  return (
    <aside className="panel progress-panel" aria-label="任务进度">
      <div className="panel-header">
        <h2>{job.kind === 'package' ? '打包进度' : '部署进度'}</h2>
        <button className="icon-button" onClick={onClose} aria-label="关闭任务进度">×</button>
      </div>
      <div className="progress-body">
        <div className="progress-title"><strong>{job.name}</strong><Status status={job.status} /></div>
        <div className={'progress-track' + (failed ? ' failed' : '')} aria-label="已完成步骤" role="progressbar"
          aria-valuemin={0} aria-valuemax={job.steps.length} aria-valuenow={advanced}>
          {job.steps.map((step) => <span className={step.status} key={step.key} />)}
        </div>
        <p className="progress-count muted" aria-live="polite">已处理 {advanced} / {job.steps.length} 步</p>
        <div className="timing-summary">
          <span title="累计执行时间，包含重试，不含排队和失败后的等待">总耗时 <strong>{recordedDuration(job, totalDuration)}</strong></span>
          <span title="历次执行前的累计排队时间">排队 <strong>{waitDuration == null ? '未记录' : (job.timingEstimated ? '≈ ' : '') + formatDuration(waitDuration)}</strong></span>
        </div>
        {slowestIndex >= 0 && (
          <p className="timing-bottleneck">最耗时：{job.steps[slowestIndex].label}
            <span>{recordedDuration(job.steps[slowestIndex], durations[slowestIndex])}</span>
          </p>
        )}
        {job.timingIncomplete && <p className="timing-note">≥ 仅包含启用统计后的耗时，早期执行未记录。</p>}
        {job.timingEstimated && <p className="timing-note">≈ 中断执行按最后保存时间估算。</p>}
        {deployed && (
          <ProductionLink key={job.id + ':' + (job.onlineUrl || '')} url={job.onlineUrl} />
        )}
        {job.resumeFrom && !failed && <p className="resume-status">断点续跑：从“{job.resumeFrom}”继续</p>}
        {message && <p className={'notice ' + (failed ? 'danger' : 'info')} role={failed ? 'alert' : 'status'}>{message}</p>}
        <ol className="step-list">
          {job.steps.map((step, index) => (
            <li key={step.key}>
              <span className={'step-index ' + step.status}>{step.status === 'success' ? '✓' : index + 1}</span>
              <span className="step-text"><strong>{step.label}</strong>{step.detail && step.detail !== message && <small>{step.detail}</small>}</span>
              <span className="step-timing">
                <span>{recordedDuration(step, durations[index])}</span>
                {totalDuration > 0 && durations[index] > 0 && (
                  <small>{Math.min(100, durations[index] / totalDuration * 100).toFixed(1)}%</small>
                )}
              </span>
              <Status status={step.status} />
            </li>
          ))}
        </ol>
        <div className="button-row progress-actions">
          <PackageDownload job={job} className="button primary" />
          {deployed && job.onlineUrl && (
            <a className="button primary" href={job.onlineUrl} target="_blank" rel="noreferrer">打开生产环境</a>
          )}
          {targetURL && <a className="button secondary" href={targetURL} target="_blank" rel="noreferrer">打开妙搭后台</a>}
          {job.kind === 'deploy' && failed && <button className="button secondary" disabled={retrying} onClick={onRetry}>
            {retrying ? '正在继续…' : retryStep ? `从“${retryStep}”继续` : '继续部署'}
          </button>}
        </div>
      </div>
    </aside>
  )
}
