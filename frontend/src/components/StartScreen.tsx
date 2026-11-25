import type {ChangeEvent, DragEvent, SubmitEvent} from "react"
import {useState} from "react"
import {
  ArrowRight,
  CloudUpload,
  FileText,
  LoaderCircle,
  Scale,
  ScanSearch,
  ShieldCheck,
  Sparkles,
  X,
} from "lucide-react"
import auditMockJson from "../mocks/audit_mock.json"
import type {LawType} from "../api/audits"
import {startAuditByFile, startAuditByNumber, uploadContract} from "../api/audits"
import type {AuditResult} from "../types/audit"

const auditMock = auditMockJson as unknown as AuditResult

const ALLOWED_EXTENSIONS = [".pdf", ".docx"]
const LAW_TYPES: LawType[] = ["44-ФЗ", "223-ФЗ"]

interface StartScreenProps {
  onDemo: (result: AuditResult) => void
  onAudit: (id: string) => void
}

function isAllowed(name: string) {
  return ALLOWED_EXTENSIONS.some((ext) => name.toLowerCase().endsWith(ext))
}

const FEATURES = [
  { Icon: Scale, text: "Проверка по 44-ФЗ и 223-ФЗ" },
  { Icon: ShieldCheck, text: "Поиск опасных пунктов контракта" },
  { Icon: ScanSearch, text: "Светофор рисков за секунды" },
]

export default function StartScreen({ onDemo, onAudit }: StartScreenProps) {
  const [tenderNumber, setTenderNumber] = useState("")
  const [touched, setTouched] = useState(false)
  const [file, setFile] = useState<File | null>(null)
  const [rejected, setRejected] = useState<string[]>([])
  const [dragging, setDragging] = useState(false)
  const [lawType, setLawType] = useState<LawType>("44-ФЗ")
  const [busy, setBusy] = useState<"number" | "file" | null>(null)
  const [error, setError] = useState<string | null>(null)

  const isValid = /^\d{11}$|^\d{19}$/.test(tenderNumber)

  const handleNumberChange = (e: ChangeEvent<HTMLInputElement>) => {
    setTenderNumber(e.target.value.replace(/\D/g, "").slice(0, 19))
  }

  const handleSubmit = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()
    setTouched(true)
    if (!isValid || busy) return
    setBusy("number")
    setError(null)
    try {
      const { id } = await startAuditByNumber(tenderNumber)
      onAudit(id)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось запустить проверку.")
    } finally {
      setBusy(null)
    }
  }

  const handleStartFile = async () => {
    if (!file || busy) return
    setBusy("file")
    setError(null)
    try {
      const { path } = await uploadContract(file)
      const { id } = await startAuditByFile(path, lawType)
      onAudit(id)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось запустить проверку файла.")
    } finally {
      setBusy(null)
    }
  }

  const acceptFiles = (list: FileList) => {
    const incoming = Array.from(list)
    const allowed = incoming.filter((f) => isAllowed(f.name))
    if (allowed.length > 0) setFile(allowed[allowed.length - 1])
    setRejected(incoming.filter((f) => !isAllowed(f.name)).map((f) => f.name))
  }

  const handleDrop = (e: DragEvent<HTMLLabelElement>) => {
    e.preventDefault()
    setDragging(false)
    if (e.dataTransfer.files.length > 0) acceptFiles(e.dataTransfer.files)
  }

  return (
    <div className="relative isolate overflow-hidden">
      {/* фоновые пятна */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 bg-gradient-to-b from-indigo-50 via-white to-white"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 -right-32 -z-10 size-96 rounded-full bg-indigo-300/40 blur-3xl"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -bottom-40 -left-32 -z-10 size-96 rounded-full bg-violet-300/40 blur-3xl"
      />

      <div className="mx-auto flex min-h-screen w-full max-w-2xl flex-col items-center px-4 py-12">
        <div className="flex items-center gap-2.5">
          <span className="flex size-10 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 to-violet-600 text-white shadow-lg shadow-indigo-500/30">
            <ScanSearch className="size-5" />
          </span>
          <span className="text-lg font-bold tracking-tight text-slate-900">TenderScope</span>
        </div>

        <h1 className="mt-12 bg-gradient-to-br from-slate-900 via-slate-800 to-indigo-700 bg-clip-text text-center text-4xl font-extrabold tracking-tight text-transparent sm:text-5xl">
          Светофор рисков
          <br />
          для госзакупок
        </h1>
        <p className="mt-4 max-w-lg text-center text-base text-slate-500">
          Проверьте контракт на опасные пункты и получите наглядный разбор за секунды.
        </p>

        <div className="mt-10 w-full rounded-3xl border border-slate-200/80 bg-white/80 p-6 shadow-xl shadow-slate-900/5 ring-1 ring-white/60 backdrop-blur sm:p-8">
          <form onSubmit={handleSubmit}>
            <label htmlFor="tender-number" className="block text-sm font-semibold text-slate-700">
              Номер закупки в ЕИС
            </label>
            <div className="mt-2.5 flex flex-col gap-3 sm:flex-row">
              <input
                id="tender-number"
                inputMode="numeric"
                placeholder="0173200001723000045"
                value={tenderNumber}
                onChange={handleNumberChange}
                onBlur={() => setTouched(true)}
                className={`min-w-0 flex-1 rounded-xl border bg-white px-4 py-3 font-mono text-sm tracking-widest text-slate-900 shadow-sm outline-none transition placeholder:font-sans placeholder:tracking-normal placeholder:text-slate-400 focus:ring-4 ${
                  touched && !isValid
                    ? "border-red-300 focus:border-red-400 focus:ring-red-100"
                    : "border-slate-300 focus:border-indigo-400 focus:ring-indigo-100"
                }`}
              />
              <button
                type="submit"
                disabled={!isValid || busy !== null}
                className="group inline-flex items-center justify-center gap-2 rounded-xl bg-gradient-to-br from-indigo-600 to-violet-600 px-6 py-3 text-sm font-semibold text-white shadow-lg shadow-indigo-500/25 transition hover:from-indigo-700 hover:to-violet-700 hover:shadow-indigo-500/40 active:scale-[.98] disabled:cursor-not-allowed disabled:from-slate-200 disabled:to-slate-200 disabled:text-slate-400 disabled:shadow-none"
              >
                {busy === "number" ? (
                  <>
                    <LoaderCircle className="size-4 animate-spin" />
                    Запускаем...
                  </>
                ) : (
                  <>
                    Проверить контракт
                    <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
                  </>
                )}
              </button>
            </div>
            <div className="mt-2 min-h-5 text-sm">
              {touched && !isValid && (
                <p className="text-red-600">Укажите номер закупки — 11 или 19 цифр.</p>
              )}
            </div>
          </form>

          <div className="my-6 flex items-center gap-4">
            <span className="h-px flex-1 bg-slate-200" />
            <span className="text-xs font-medium tracking-wide text-slate-400 uppercase">или</span>
            <span className="h-px flex-1 bg-slate-200" />
          </div>

          <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
            <span className="text-sm font-semibold text-slate-700">
              Закон для загруженного файла
            </span>
            <div
              role="radiogroup"
              aria-label="Закон для загруженного файла"
              className="flex rounded-xl bg-slate-100 p-1"
            >
              {LAW_TYPES.map((law) => (
                <button
                  key={law}
                  type="button"
                  role="radio"
                  aria-checked={lawType === law}
                  onClick={() => setLawType(law)}
                  className={`rounded-lg px-4 py-1.5 text-sm font-semibold transition ${
                    lawType === law
                      ? "bg-white text-indigo-700 shadow-sm"
                      : "text-slate-500 hover:text-slate-700"
                  }`}
                >
                  {law}
                </button>
              ))}
            </div>
          </div>

          <label
            htmlFor="contract-files"
            onDragOver={(e) => {
              e.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={handleDrop}
            className={`flex cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed px-6 py-10 text-center transition ${
              dragging
                ? "border-indigo-400 bg-indigo-50/70"
                : "border-slate-300 bg-slate-50/60 hover:border-indigo-300 hover:bg-indigo-50/40"
            }`}
          >
            <input
              id="contract-files"
              type="file"
              accept=".pdf,.docx"
              className="hidden"
              onChange={(e: ChangeEvent<HTMLInputElement>) => {
                if (e.target.files && e.target.files.length > 0) acceptFiles(e.target.files)
                e.target.value = ""
              }}
            />
            <span
              className={`flex size-12 items-center justify-center rounded-full transition ${
                dragging ? "bg-indigo-100 text-indigo-600" : "bg-slate-100 text-slate-400"
              }`}
            >
              <CloudUpload className="size-6" />
            </span>
            <span className="mt-3 text-sm font-semibold text-slate-700">
              Перетащите контракт сюда или нажмите для выбора
            </span>
            <span className="mt-1 text-xs text-slate-400">
              PDF, DOCX — до 30 МБ. Закон: {lawType}
            </span>
          </label>

          {(file !== null || rejected.length > 0) && (
            <div className="mt-3 space-y-2">
              {file !== null && (
                <div className="flex items-center justify-between rounded-xl border border-slate-200 bg-white px-3.5 py-2.5 text-sm text-slate-700 shadow-sm">
                  <span className="flex min-w-0 items-center gap-2.5">
                    <FileText className="size-4 shrink-0 text-indigo-500" />
                    <span className="truncate">{file.name}</span>
                  </span>
                  <button
                    type="button"
                    onClick={() => setFile(null)}
                    className="rounded-lg p-1 text-slate-400 transition hover:bg-slate-100 hover:text-slate-600"
                    aria-label={`Убрать ${file.name}`}
                  >
                    <X className="size-4" />
                  </button>
                </div>
              )}
              {rejected.length > 0 && (
                <p className="text-sm text-red-600">
                  Не поддерживается: {rejected.join(", ")} — нужны файлы PDF или DOCX.
                </p>
              )}
            </div>
          )}

          {file !== null && (
            <button
              type="button"
              onClick={handleStartFile}
              disabled={busy !== null}
              className="mt-3 flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-br from-indigo-600 to-violet-600 px-4 py-3 text-sm font-semibold text-white shadow-lg shadow-indigo-500/25 transition hover:from-indigo-700 hover:to-violet-700 active:scale-[.98] disabled:cursor-not-allowed disabled:from-slate-200 disabled:to-slate-200 disabled:text-slate-400 disabled:shadow-none"
            >
              {busy === "file" ? (
                <>
                  <LoaderCircle className="size-4 animate-spin" />
                  Загружаем файл...
                </>
              ) : (
                <>
                  Проверить файл ({lawType})
                  <ArrowRight className="size-4" />
                </>
              )}
            </button>
          )}

          {error && (
            <div className="mt-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {error}
            </div>
          )}

          <button
            type="button"
            onClick={() => onDemo(auditMock)}
            className="mt-6 flex w-full items-center justify-center gap-2 rounded-xl border border-indigo-200 bg-indigo-50/70 px-4 py-3 text-sm font-semibold text-indigo-700 transition hover:border-indigo-300 hover:bg-indigo-100/80"
          >
            <Sparkles className="size-4" />
            Демо: Контракт с отсрочкой 60 дней
          </button>
        </div>

        <ul className="mt-10 grid w-full gap-3 sm:grid-cols-3">
          {FEATURES.map(({ Icon, text }) => (
            <li
              key={text}
              className="flex flex-col items-center gap-2 rounded-2xl border border-slate-200/70 bg-white/60 px-3 py-4 text-center text-xs font-medium text-slate-600 backdrop-blur"
            >
              <span className="flex size-9 items-center justify-center rounded-full bg-indigo-50 text-indigo-600">
                <Icon className="size-4.5" />
              </span>
              {text}
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
