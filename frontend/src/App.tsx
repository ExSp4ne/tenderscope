import {useCallback, useState} from "react"
import AuditDashboard from "./components/AuditDashboard"
import ProcessingScreen from "./components/ProcessingScreen"
import StartScreen from "./components/StartScreen"
import type {AuditResult} from "./types/audit"

export default function App() {
  const [result, setResult] = useState<AuditResult | null>(null)
  const [auditId, setAuditId] = useState<string | null>(null)

  const handleComplete = useCallback((res: AuditResult) => {
    setAuditId(null)
    setResult(res)
  }, [])

  const handleBack = useCallback(() => setAuditId(null), [])

  return (
    <div className="min-h-screen bg-slate-50 font-sans text-slate-900 antialiased">
      {result ? (
        <AuditDashboard result={result} onBack={() => setResult(null)} />
      ) : auditId ? (
        <ProcessingScreen auditId={auditId} onComplete={handleComplete} onBack={handleBack} />
      ) : (
        <StartScreen onDemo={(demo) => setResult(demo)} onAudit={(id) => setAuditId(id)} />
      )}
    </div>
  )
}
