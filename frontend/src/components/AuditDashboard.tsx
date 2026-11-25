import {useMemo, useState} from "react"
import {
  ArrowLeft,
  Building,
  Flag,
  Hash,
  Lightbulb,
  Quote,
  Scale,
  ShieldCheck,
  ShieldExclamationPoint,
  ShieldX,
  TriangleExclamationPoint,
  Wallet,
} from "lucide-react"
import type {AuditResult, RiskFilter, RiskItem, RiskLevel} from "../types/audit"

interface AuditDashboardProps {
  result: AuditResult
  onBack: () => void
}

const LEVEL_META: Record<
  RiskLevel,
  {
    label: string
    Icon: typeof ShieldX
    bar: string
    cardTint: string
    badge: string
    counter: string
    counterActive: string
  }
> = {
  red: {
    label: "Критические",
    Icon: ShieldX,
    bar: "bg-red-500",
    cardTint: "border-red-100",
    badge: "bg-red-100 text-red-700",
    counter: "border-red-200 bg-red-50 text-red-700",
    counterActive: "border-red-500 bg-red-100 ring-2 ring-red-300",
  },
  yellow: {
    label: "Предупреждения",
    Icon: TriangleExclamationPoint,
    bar: "bg-amber-400",
    cardTint: "border-amber-100",
    badge: "bg-amber-100 text-amber-700",
    counter: "border-amber-200 bg-amber-50 text-amber-700",
    counterActive: "border-amber-500 bg-amber-100 ring-2 ring-amber-300",
  },
  green: {
    label: "Безопасно",
    Icon: ShieldCheck,
    bar: "bg-emerald-500",
    cardTint: "border-emerald-100",
    badge: "bg-emerald-100 text-emerald-700",
    counter: "border-emerald-200 bg-emerald-50 text-emerald-700",
    counterActive: "border-emerald-500 bg-emerald-100 ring-2 ring-emerald-300",
  },
}

const FILTERS: { key: RiskFilter; label: string }[] = [
  { key: "all", label: "Все" },
  { key: "red", label: "Только красные" },
  { key: "yellow", label: "Жёлтые" },
  { key: "green", label: "Зелёные" },
]

const formatPrice = (price: number) => `${new Intl.NumberFormat("ru-RU").format(price)} ₽`

function RiskCard({ risk }: { risk: RiskItem }) {
  const meta = LEVEL_META[risk.level]
  return (
    <article
      className={`flex overflow-hidden rounded-xl border ${meta.cardTint} border-slate-200 bg-white shadow-sm`}
    >
      <div className={`w-1.5 shrink-0 ${meta.bar}`} />
      <div className="flex-1 p-5">
        <div className="flex flex-wrap items-center gap-2">
          <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${meta.badge}`}>
            {meta.label}
          </span>
          <span className="rounded-full bg-slate-100 px-2.5 py-0.5 text-xs font-medium text-slate-600">
            {risk.category}
          </span>
        </div>
        <h3 className="mt-3 text-base font-semibold text-slate-900">{risk.title}</h3>
        <p className="mt-1.5 text-sm leading-relaxed text-slate-600">{risk.description}</p>
        <p className="mt-3 flex items-start gap-2 text-sm leading-relaxed text-slate-700">
          <Lightbulb className="mt-0.5 size-4 shrink-0 text-indigo-500" />
          <span>
            <span className="font-semibold">Рекомендация: </span>
            {risk.recommendation}
          </span>
        </p>
        <blockquote className="mt-4 rounded-lg bg-slate-100 p-3.5">
          <Quote className="mb-1 inline size-3.5 text-slate-400" />
          <p className="inline text-sm leading-relaxed text-slate-600 italic">«{risk.quote}»</p>
          <footer className="mt-2 text-xs font-medium text-slate-500">
            стр. {risk.pageNumber}, п. {risk.clauseNumber}
          </footer>
        </blockquote>
      </div>
    </article>
  )
}

export default function AuditDashboard({ result, onBack }: AuditDashboardProps) {
  const [filter, setFilter] = useState<RiskFilter>("all")

  const counts = useMemo(
    () => ({
      red: result.risks.filter((r) => r.level === "red").length,
      yellow: result.risks.filter((r) => r.level === "yellow").length,
      green: result.risks.filter((r) => r.level === "green").length,
    }),
    [result],
  )

  const verdict =
    counts.red > 0
      ? {
          title: "Высокий риск — контракт требует доработки",
          box: "border-red-200 bg-red-50 text-red-900",
          icon: "text-red-500",
          Icon: ShieldExclamationPoint,
        }
      : counts.yellow > 0
        ? {
            title: "Средний риск — есть вопросы к отдельным пунктам",
            box: "border-amber-200 bg-amber-50 text-amber-900",
            icon: "text-amber-500",
            Icon: TriangleExclamationPoint,
          }
        : {
            title: "Низкий риск — контракт выглядит безопасно",
            box: "border-emerald-200 bg-emerald-50 text-emerald-900",
            icon: "text-emerald-500",
            Icon: ShieldCheck,
          }

  const visibleRisks =
    filter === "all" ? result.risks : result.risks.filter((r) => r.level === filter)

  const facts = [
    { Icon: Hash, label: "Номер закупки", value: result.tenderNumber },
    { Icon: Building, label: "Заказчик", value: result.customer },
    { Icon: Wallet, label: "Начальная цена", value: formatPrice(result.price) },
    { Icon: Scale, label: "Закон", value: result.lawType },
    { Icon: Flag, label: "Статус", value: result.status },
  ]

  return (
    <div className="min-h-screen">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-5xl items-center justify-between px-4 py-4">
          <button
            type="button"
            onClick={onBack}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-slate-600 transition hover:bg-slate-100 hover:text-slate-900"
          >
            <ArrowLeft className="size-4" />
            Новый аудит
          </button>
          <span className="text-sm font-semibold text-slate-900">TenderScope</span>
        </div>
      </header>

      <main className="mx-auto max-w-5xl px-4 py-8">
        <h1 className="text-2xl font-bold tracking-tight text-slate-900">{result.title}</h1>

        <dl className="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3 lg:grid-cols-5">
          {facts.map(({ Icon, label, value }) => (
            <div key={label}>
              <dt className="flex items-center gap-1.5 text-xs font-medium text-slate-500">
                <Icon className="size-3.5" />
                {label}
              </dt>
              <dd className="mt-0.5 truncate text-sm font-semibold text-slate-900" title={value}>
                {value}
              </dd>
            </div>
          ))}
        </dl>

        <div className={`mt-6 flex items-start gap-3 rounded-xl border p-4 ${verdict.box}`}>
          <verdict.Icon className={`mt-0.5 size-5 shrink-0 ${verdict.icon}`} />
          <div>
            <p className="text-sm font-semibold">{verdict.title}</p>
            <p className="mt-1 text-sm leading-relaxed opacity-90">{result.summary.verdict}</p>
          </div>
        </div>

        <div className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-3">
          {(Object.keys(LEVEL_META) as RiskLevel[]).map((level) => {
            const meta = LEVEL_META[level]
            const active = filter === level
            return (
              <button
                key={level}
                type="button"
                onClick={() => setFilter(active ? "all" : level)}
                className={`flex items-center gap-3 rounded-xl border p-4 text-left transition ${
                  active ? meta.counterActive : `${meta.counter} hover:brightness-95`
                }`}
              >
                <meta.Icon className="size-8" />
                <span>
                  <span className="block text-2xl leading-none font-bold">{counts[level]}</span>
                  <span className="mt-1 block text-xs font-medium">{meta.label}</span>
                </span>
              </button>
            )
          })}
        </div>

        <div className="mt-6 flex flex-wrap items-center gap-2">
          {FILTERS.map(({ key, label }) => {
            const active = filter === key
            const count = key === "all" ? result.risks.length : counts[key]
            return (
              <button
                key={key}
                type="button"
                onClick={() => setFilter(key)}
                className={`rounded-full px-4 py-1.5 text-sm font-medium transition ${
                  active
                    ? "bg-slate-900 text-white shadow-sm"
                    : "border border-slate-200 bg-white text-slate-600 hover:bg-slate-100"
                }`}
              >
                {label}
                <span className={`ml-1.5 text-xs ${active ? "text-slate-300" : "text-slate-400"}`}>
                  {count}
                </span>
              </button>
            )
          })}
        </div>

        <div className="mt-5 space-y-4">
          {visibleRisks.length > 0 ? (
            visibleRisks.map((risk) => <RiskCard key={risk.id} risk={risk} />)
          ) : (
            <div className="rounded-xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500">
              Нет карточек с этим уровнем риска.
            </div>
          )}
        </div>
      </main>
    </div>
  )
}
