package handlers

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
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

func GetAuditByID(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "id") == "demo-1" {
		writeJSON(w, http.StatusOK, demoAudit)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "audit not found"})
}