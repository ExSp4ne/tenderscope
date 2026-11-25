export type RiskLevel = "red" | "yellow" | "green"

export type RiskFilter = "all" | RiskLevel

export interface RiskItem {
  id: string
  category: string
  level: RiskLevel
  title: string
  description: string
  recommendation: string
  quote: string
  pageNumber: number
  clauseNumber: string
}

export interface AuditResult {
  id?: string
  tenderNumber: string
  title: string
  customer: string
  price: number
  lawType: string
  status: string
  summary: string
  risks: RiskItem[]
}
