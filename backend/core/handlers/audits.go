package handlers

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type passport struct {
	TenderNumber string `json:"tender_number"`
	Customer     string `json:"customer"`
	Price        int64  `json:"price"`
	LawType      string `json:"law_type"`
}

type counters struct {
	Red    int `json:"red"`
	Yellow int `json:"yellow"`
	Green  int `json:"green"`
}

type riskItem struct {
	ID             string `json:"id"`
	Level          string `json:"level"`
	Category       string `json:"category"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
	Quote          string `json:"quote"`
	PageNumber     int    `json:"page_number"`
	ClauseNumber   string `json:"clause_number"`
}

type auditResponse struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Passport  passport   `json:"passport"`
	Counters  counters   `json:"counters"`
	RiskItems []riskItem `json:"risk_items"`
}

var demoAudit = auditResponse{
	ID:     "demo-1",
	Status: "completed",
	Passport: passport{
		TenderNumber: "0373200009324000123",
		Customer:     `ГБУ "Гормост"`,
		Price:        14500000,
		LawType:      "44-ФЗ",
	},
	Counters: counters{Red: 1, Yellow: 1, Green: 1},
	RiskItems: []riskItem{
		{
			ID: "risk-1", Level: "red", Category: "payment_terms",
			Title:          "Оплата в течение 60 рабочих дней",
			Description:    "Срок оплаты превышает допустимый и создаёт кассовый разрыв для исполнителя.",
			Recommendation: "Согласовать сокращение срока оплаты или предусмотреть аванс.",
			Quote:          "Оплата производится в течение 60 рабочих дней с даты подписания акта.",
			PageNumber:     18, ClauseNumber: "4.3",
		},
		{
			ID: "risk-2", Level: "yellow", Category: "acceptance_deadline",
			Title:          "Срок приемки заказчиком — 30 рабочих дней",
			Description:    "Длительный срок приёмки увеличивает период оборота средств.",
			Recommendation: "Зафиксировать промежуточные этапы приёмки.",
			Quote:          "Заказчик рассматривает результаты в течение 30 рабочих дней.",
			PageNumber:     25, ClauseNumber: "6.2",
		},
		{
			ID: "risk-3", Level: "green", Category: "termination",
			Title:          "Условия расторжения соответствуют ст. 95 44-ФЗ",
			Description:    "Пункт соответствует требованиям законодательства.",
			Recommendation: "Дополнительных действий не требуется.",
			Quote:          "Расторжение контракта допускается по основаниям, предусмотренным ст. 95 44-ФЗ.",
			PageNumber:     34, ClauseNumber: "10.1",
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Printf("write response: %v", err)
	}
}

// GetAuditByID — мок: отдаёт demo-1, для остальных id — 404.
func GetAuditByID(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "id") == "demo-1" {
		writeJSON(w, http.StatusOK, demoAudit)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "audit not found"})
}
