import {useEffect, useRef, useState} from "react"
import {AlertTriangle, ArrowLeft, Check, LoaderCircle} from "lucide-react"
import {getAudit} from "../api/audits"
import type {AuditResult} from "../types/audit"

const POLL_INTERVAL_MS = 2000
const POLL_DEADLINE_MS = 5 * 60 * 1000
const PENDING_STATUSES = ["created", "processing"]

interface ProcessingScreenProps {
  auditId: string
  onComplete: (result: AuditResult) => void
  onBack: () => void
}

const STEPS = [
  { title: "Скачиваем документацию из ЕИС...", hint: "Ищем закупку и загружаем файлы контракта" },
  { title: "Анализируем пункты контракта...", hint: "Ищем опасные условия по 44-ФЗ и 223-ФЗ" },
]

export default function ProcessingScreen({ auditId, onComplete, onBack }: ProcessingScreenProps) {
  const [status, setStatus] = useState("created")
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)

  const onCompleteRef = useRef(onComplete)

  useEffect(() => {
    onCompleteRef.current = onComplete
  }, [onComplete])

  useEffect(() => {
    let cancelled = false
    let timer: number | null = null
    const deadline = Date.now() + POLL_DEADLINE_MS

    const poll = async () => {
      try {
        const audit = await getAudit(auditId)
        if (cancelled) return
        if (audit.status === "failed") {
          setError("Сервер не смог завершить проверку. Попробуйте ещё раз.")
          return
        }
        if (PENDING_STATUSES.includes(audit.status)) {
          if (Date.now() >= deadline) {
            setError("Проверка идёт слишком долго - возможно, сервер завис. Попробуйте ещё раз.")
            return
          }
          setStatus(audit.status)
          timer = window.setTimeout(poll, POLL_INTERVAL_MS)
          return
        }
        onCompleteRef.current(audit)
      } catch (e) {
        if (cancelled) return
        setError(e instanceof Error ? e.message : "Не удалось получить статус проверки.")
      }
    }

    void poll()
    return () => {
      cancelled = true
      if (timer !== null) window.clearTimeout(timer)
    }
  }, [auditId, attempt])

  const retry = () => {
    setError(null)
    setStatus("created")
    setAttempt((a) => a + 1)
  }

  const activeStep = status === "created" ? 0 : 1

  return (
    <div className="mx-auto flex min-h-screen w-full max-w-md flex-col items-center justify-center px-4 py-12">
      <div className="w-full rounded-3xl border border-slate-200/80 bg-white/80 p-8 shadow-xl shadow-slate-900/5 ring-1 ring-white/60 backdrop-blur">
        {error ? (
          <div className="flex flex-col items-center text-center">
            <span className="flex size-14 items-center justify-center rounded-full bg-red-100 text-red-600">
              <AlertTriangle className="size-7" />
            </span>
            <h2 className="mt-5 text-lg font-bold text-slate-900">Проверка прервалась</h2>
            <p className="mt-2 text-sm text-slate-500">{error}</p>
            <div className="mt-6 flex w-full gap-3">
              <button
                type="button"
                onClick={onBack}
                className="flex-1 rounded-xl border border-slate-300 bg-white px-4 py-3 text-sm font-semibold text-slate-700 transition hover:bg-slate-50 active:scale-[.98]"
              >
                Назад
              </button>
              <button
                type="button"
                onClick={retry}
                className="flex-1 rounded-xl bg-gradient-to-br from-indigo-600 to-violet-600 px-4 py-3 text-sm font-semibold text-white shadow-lg shadow-indigo-500/25 transition hover:from-indigo-700 hover:to-violet-700 active:scale-[.98]"
              >
                Попробовать снова
              </button>
            </div>
          </div>
        ) : (
          <div className="flex flex-col items-center text-center">
            <span className="flex size-14 items-center justify-center rounded-full bg-indigo-50 text-indigo-600">
              <LoaderCircle className="size-7 animate-spin" />
            </span>
            <h2 className="mt-5 text-lg font-bold text-slate-900">Проверяем контракт</h2>
            <p className="mt-1 text-sm text-slate-400">Это займёт пару минут</p>

            <ul className="mt-7 w-full space-y-3 text-left">
              {STEPS.map((step, i) => {
                const done = i < activeStep
                const active = i === activeStep
                return (
                  <li
                    key={step.title}
                    className={`flex items-start gap-3 rounded-2xl border px-4 py-3.5 transition ${
                      active
                        ? "border-indigo-200 bg-indigo-50/70"
                        : done
                          ? "border-emerald-100 bg-emerald-50/50"
                          : "border-slate-200 bg-slate-50/60"
                    }`}
                  >
                    <span
                      className={`mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full ${
                        done
                          ? "bg-emerald-500 text-white"
                          : active
                            ? "bg-indigo-600 text-white"
                            : "bg-slate-200 text-slate-400"
                      }`}
                    >
                      {done ? (
                        <Check className="size-3.5" />
                      ) : active ? (
                        <LoaderCircle className="size-3.5 animate-spin" />
                      ) : (
                        <span className="text-xs font-bold">{i + 1}</span>
                      )}
                    </span>
                    <span>
                      <span
                        className={`block text-sm font-semibold ${
                          done
                            ? "text-emerald-700"
                            : active
                              ? "text-indigo-800"
                              : "text-slate-400"
                        }`}
                      >
                        {step.title}
                      </span>
                      <span className="mt-0.5 block text-xs text-slate-400">{step.hint}</span>
                    </span>
                  </li>
                )
              })}
            </ul>

            <button
              type="button"
              onClick={onBack}
              className="mt-7 inline-flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-medium text-slate-500 transition hover:bg-slate-100 hover:text-slate-700"
            >
              <ArrowLeft className="size-4" />
              Отменить и выйти
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
