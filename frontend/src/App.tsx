import { useState } from "react"
import AuditDashboard from "./components/AuditDashboard"
import StartScreen from "./components/StartScreen"
import type { AuditResult } from "./types/audit"

export default function App() {
  const [result, setResult] = useState<AuditResult | null>(null)

  return (
    <div className="min-h-screen bg-slate-50 font-sans text-slate-900 antialiased">
      {result ? (
        <AuditDashboard result={result} onBack={() => setResult(null)} />
      ) : (
        <StartScreen onDemo={(demo) => setResult(demo)} />
      )}
    </div>
  )
}
