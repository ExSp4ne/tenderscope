# Контракты обмена данными (API & Pipeline Contracts)

---

## 1. Внешний API: Ответ аудита закупки (Go -> React)
Эндпоинт: `GET /api/audits/:id`

Пример ответа:
```json
{
  "id": "demo-1",
  "tenderNumber": "0373200009324000123",
  "title": "Поставка компьютерного оборудования",
  "customer": "ГБУ Гормост",
  "price": 14500000,
  "lawType": "44-ФЗ",
  "status": "completed",
  "summary": {
    "redCount": 1,
    "yellowCount": 1,
    "greenCount": 1,
    "verdict": "Высокий риск: обнаружены кабальные условия"
  },
  "risks": [
    {
      "id": "r-1",
      "category": "payment",
      "level": "red",
      "title": "Отсрочка оплаты 60 рабочих дней",
      "description": "Срок оплаты превышает законные 7 рабочих дней по 44-ФЗ, что ведет к кассовому разрыву.",
      "recommendation": "Подать запрос на разъяснение положений документации.",
      "quote": "Оплата производится в течение 60 рабочих дней с даты подписания акта.",
      "pageNumber": 18,
      "clauseNumber": "4.3"
    }
  ]
}

---

## 2. Внешний API: Запуск проверки (React -> Go)
Эндпоинт: `POST /api/audits`

Тело запроса — одно из двух (оба поля сразу -> `400`):
```json
{"tenderNumber": "0373200009324000123"}
```
```json
{"filePath": "<путь из ответа POST /api/upload>", "lawType": "44-ФЗ"}
```
* `tenderNumber` — 19 цифр (44-ФЗ) или 11 цифр (223-ФЗ); файл и метаданные скачиваются из ЕИС (`eis.DownloadTenderFiles`).
* `lawType` — только кириллицей: `"44-ФЗ"` или `"223-ФЗ"`. `filePath` принимается только внутри папок загрузок/скачанных закупок.

Ответ `202 Accepted`:
```json
{"id": "<audit_id>", "status": "processing"}
```
Ошибки: `400` (неверное тело, номер, `lawType`, путь), `502` (ЕИС не вернул закупку/файл), `503` (сервер останавливается). Результат — через `GET /api/audits/:id`: `status` меняется `processing` -> `completed` | `failed`.

---

## 3. Внутренний контракт: Python-анализатор -> Go-воркер
Запуск: `python analyzer_bridge/run_analysis.py --file=<путь> --law=<44-ФЗ|223-ФЗ> --out=<tmp_result.json>`
(таймаут 5 минут; код выхода `0` — успех, иначе проверка получает статус `failed`).

Файл `--out`:
```json
{
  "risks": [
    {
      "category": "payment",
      "level": "red",
      "title": "Оплата через 60 дней",
      "description": "Два предложения.",
      "recommendation": "Действие поставщика.",
      "quote": "Дословная цитата из документа.",
      "pageNumber": 18,
      "clauseNumber": "4.3"
    }
  ]
}
```
* `level`: `red` | `yellow` | `green`; `category`, `title`, `description` обязательны; остальные поля необязательны.
* Допускается и голый массив `[...]`, а также `exact_quote`, `page_number`, `clause_number` (snake_case из ml-промпта).
* Пустой `risks` — валидный результат (проверка `completed` без рисков).
