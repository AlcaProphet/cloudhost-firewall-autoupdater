// 统一请求封装：res.ok 检查 + error 提取 + 可选超时（AbortController）
// 全站经本模块发起请求，任何非 2xx 响应统一抛 RequestError，杜绝"失败误报成功"

// 配置导入的必填结构诊断；仅接收与 100 项展示上限一致的 HTTP 400 详情。
export interface MissingFieldsDetails {
  fields: string[]
  total: number
  truncated: boolean
}
function missingFieldsDetails(status: number, data: any): MissingFieldsDetails | undefined {
  if (status !== 400 || !Array.isArray(data?.missing_fields)) return undefined
  const fields = data.missing_fields
  const total = data.missing_fields_total
  const truncated = data.missing_fields_truncated
  if (fields.length < 1 || fields.length > 100 || !fields.every((p: unknown) => typeof p === 'string' && p.length > 0) ||
      !Number.isSafeInteger(total) || total < fields.length || typeof truncated !== 'boolean' ||
      truncated !== (total > fields.length) || (truncated && fields.length !== 100)) return undefined
  return { fields: [...fields], total, truncated }
}
// RequestError 请求失败错误（非 2xx 响应）
export class RequestError extends Error {
  status: number
  missingFields?: MissingFieldsDetails
  constructor(status: number, message: string, missingFields?: MissingFieldsDetails) {
    super(message)
    this.name = 'RequestError'
    this.status = status
    this.missingFields = missingFields
  }
}

// request 统一请求封装
// - 非 2xx 响应抛 RequestError（error 字段取自后端 {"error": ...}）
// - timeoutMs 可选：连接测试传 15000，超时抛 "请求超时"
// - 未知 API 路径返回 HTML 时 res.json() 解析失败进 catch
export async function request<T>(url: string, opts: RequestInit = {}, timeoutMs?: number): Promise<T> {
  const controller = new AbortController()
  const timer = timeoutMs ? setTimeout(() => controller.abort(), timeoutMs) : null
  try {
    const res = await fetch(url, { ...opts, signal: controller.signal })
    let data: any = null
    try {
      data = await res.json()
    } catch {
      /* 非 JSON 响应（如 SPA 兜底 HTML） */
    }
    if (!res.ok) {
      throw new RequestError(res.status, data?.error || `请求失败 (${res.status})`, missingFieldsDetails(res.status, data))
    }
    return data as T
  } catch (e: any) {
    if (e instanceof RequestError) throw e
    if (e?.name === 'AbortError') throw new Error('请求超时')
    throw e
  } finally {
    if (timer) clearTimeout(timer)
  }
}
