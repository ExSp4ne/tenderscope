package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"tenderscope/backend/core/storage"
	"tenderscope/backend/eis"
)

type summary struct {
	RedCount    int    `json:"redCount"`
	YellowCount int    `json:"yellowCount"`
	GreenCount  int    `json:"greenCount"`
	Verdict     string `json:"verdict"`
}

type riskItem struct {
	ID             string `json:"id"`
	Category       string `json:"category"`
	Level          string `json:"level"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
	Quote          string `json:"quote"`
	PageNumber     int    `json:"pageNumber"`
	ClauseNumber   string `json:"clauseNumber"`
}

type auditResponse struct {
	ID           string     `json:"id"`
	TenderNumber string     `json:"tenderNumber"`
	Title        string     `json:"title"`
	Customer     string     `json:"customer"`
	Price        float64    `json:"price"`
	LawType      string     `json:"lawType"`
	Status       string     `json:"status"`
	Summary      summary    `json:"summary"`
	Risks        []riskItem `json:"risks"`
}

var demoAudit = auditResponse{
	ID:           "demo-1",
	TenderNumber: "0373200009324000123",
	Title:        "Поставка компьютерного оборудования",
	Customer:     `ГБУ "Гормост"`,
	Price:        14500000,
	LawType:      "44-ФЗ",
	Status:       "completed",
	Summary: summary{
		RedCount:    1,
		YellowCount: 1,
		GreenCount:  1,
		Verdict:     "Высокий риск: обнаружены кабальные условия",
	},
	Risks: []riskItem{
		{
			ID:             "r-1",
			Category:       "payment",
			Level:          "red",
			Title:          "Отсрочка оплаты 60 рабочих дней",
			Description:    "Срок оплаты превышает законные 7 рабочих дней по 44-ФЗ, что ведет к кассовому разрыву.",
			Recommendation: "Подать запрос на разъяснение положений документации.",
			Quote:          "Оплата производится в течение 60 рабочих дней с даты подписания акта.",
			PageNumber:     18,
			ClauseNumber:   "4.3",
		},
		{
			ID:             "r-2",
			Category:       "delivery",
			Level:          "yellow",
			Title:          "Срок приемки заказчиком — 30 рабочих дней",
			Description:    "Длительный срок приёмки увеличивает период оборота средств. Задержка подписания акта откладывает момент начала отсчёта оплаты.",
			Recommendation: "Зафиксировать промежуточные этапы приёмки или подать запрос на сокращение срока экспертизы.",
			Quote:          "Заказчик рассматривает результаты в течение 30 рабочих дней.",
			PageNumber:     25,
			ClauseNumber:   "6.2",
		},
		{
			ID:             "r-3",
			Category:       "termination",
			Level:          "green",
			Title:          "Условия расторжения соответствуют ст. 95 44-ФЗ",
			Description:    "Пункт предусматривает стандартный порядок расторжения по закону. Дополнительных рисков внесудебного отказа без оснований не выявлено.",
			Recommendation: "Дополнительных действий не требуется.",
			Quote:          "Расторжение контракта допускается по основаниям, предусмотренным ст. 95 44-ФЗ.",
			PageNumber:     34,
			ClauseNumber:   "10.1",
		},
	},
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("encode json: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Printf("write response: %v", err)
	}
}

// Submitter ставит проверку в очередь фонового воркера (реализует worker.Runner).
type Submitter interface {
	Submit(auditID, filePath, lawType string) error
}

// Audits — хендлеры /api/audits. Download подменяется в тестах.
type Audits struct {
	Repos    *storage.Repositories
	Runner   Submitter
	Download func(tenderNumber string) (filePath string, meta eis.TenderMeta, err error)
}

// NewAudits создаёт хендлеры с боевой загрузкой из ЕИС.
func NewAudits(repos *storage.Repositories, runner Submitter) *Audits {
	return &Audits{Repos: repos, Runner: runner, Download: eis.DownloadTenderFiles}
}

// Get — GET /api/audits/{id}: demo-1 отдаётся моком, остальные — из БД.
func (h *Audits) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "demo-1" {
		writeJSON(w, http.StatusOK, demoAudit)
		return
	}
	ctx := r.Context()
	audit, err := h.Repos.Audits.GetByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "audit not found"})
		return
	}
	if err != nil {
		log.Printf("get audit %s: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	tender, err := h.Repos.Tenders.GetByID(ctx, audit.TenderID)
	if err != nil {
		log.Printf("get tender %s for audit %s: %v", audit.TenderID, id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	items, err := h.Repos.RiskItems.ListByAudit(ctx, id)
	if err != nil {
		log.Printf("list risks for audit %s: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	risks := make([]riskItem, 0, len(items))
	for _, it := range items {
		ri := riskItem{
			ID: it.ID, Category: it.Category, Level: it.Level,
			Title: it.Title, Description: it.Description,
		}
		if it.Recommendation != nil {
			ri.Recommendation = *it.Recommendation
		}
		if it.Quote != nil {
			ri.Quote = *it.Quote
		}
		if it.PageNumber != nil {
			ri.PageNumber = *it.PageNumber
		}
		if it.ClauseNumber != nil {
			ri.ClauseNumber = *it.ClauseNumber
		}
		risks = append(risks, ri)
	}
	writeJSON(w, http.StatusOK, auditResponse{
		ID:           audit.ID,
		TenderNumber: tender.TenderNumber,
		Title:        tender.Title,
		Customer:     tender.Customer,
		Price:        tender.Price,
		LawType:      tender.LawType,
		Status:       audit.Status,
		Summary: summary{
			RedCount:    audit.RedCount,
			YellowCount: audit.YellowCount,
			GreenCount:  audit.GreenCount,
			Verdict:     verdict(audit),
		},
		Risks: risks,
	})
}

func verdict(a *storage.Audit) string {
	if a.Status != "completed" {
		return ""
	}
	switch {
	case a.RedCount > 0:
		return "Высокий риск: обнаружены кабальные условия"
	case a.YellowCount > 0:
		return "Средний риск: есть условия, требующие проверки"
	default:
		return "Существенных рисков не обнаружено"
	}
}
