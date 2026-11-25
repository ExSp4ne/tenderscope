import type {AuditResult} from "../types/audit"

export type LawType = "44-ФЗ" | "223-ФЗ"

const rawBaseUrl = import.meta.env.VITE_API_URL
export const API_BASE_URL = (rawBaseUrl && rawBaseUrl.trim()) || "http://localhost:8080"

const JSON_HEADERS = { "Content-Type": "application/json" }

const DEFAULT_TIMEOUT_MS = 15000
const UPLOAD_TIMEOUT_MS = 120000

async function parseError(res: Response): Promise<string> {
  try {
    const data = (await res.json()) as { error?: string }
    if (data?.error) return data.error
  } catch {}
  return `Ошибка запроса (${res.status})`
}

async function request<T>(path: string, init: RequestInit = {}, timeoutMs = DEFAULT_TIMEOUT_MS): Promise<T> {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), timeoutMs)
  let res: Response
  try {
    res = await fetch(`${API_BASE_URL}${path}`, { ...init, signal: controller.signal })
  } catch {
    window.clearTimeout(timer)
    throw new Error(
      controller.signal.aborted
        ? "Сервер не отвечает. Попробуйте ещё раз."
        : "Не удалось связаться с сервером. Проверьте подключение.",
    )
  }
  try {
    if (!res.ok) throw new Error(await parseError(res))
    return (await res.json()) as T
  } catch (err) {
    if (controller.signal.aborted) throw new Error("Сервер не отвечает. Попробуйте ещё раз.")
    throw err
  } finally {
    window.clearTimeout(timer)
  }
}

export function uploadContract(file: File): Promise<{ path: string }> {
  const form = new FormData()
  form.append("file", file)
  return request<{ path: string }>("/api/upload", { method: "POST", body: form }, UPLOAD_TIMEOUT_MS)
}

export function startAuditByNumber(tenderNumber: string): Promise<{ id: string }> {
  return request<{ id: string }>("/api/audits", {
    method: "POST",
    headers: JSON_HEADERS,
    body: JSON.stringify({ tenderNumber }),
  })
}

export function startAuditByFile(filePath: string, lawType: LawType): Promise<{ id: string }> {
  return request<{ id: string }>("/api/audits", {
    method: "POST",
    headers: JSON_HEADERS,
    body: JSON.stringify({ filePath, lawType }),
  })
}

export function getAudit(id: string): Promise<AuditResult> {
  return request<AuditResult>(`/api/audits/${encodeURIComponent(id)}`)
}
