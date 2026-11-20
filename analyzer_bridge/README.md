# Analyzer Bridge (`analyzer_bridge`)

Модуль извлечения текста из закупочных контрактов (`.pdf` и `.docx`), смысловой нарезки по пунктам и верификации цитат LLM.

## Структура
- `parser/reader.py` — постраничное чтение `.pdf` (`pdfplumber`) и `.docx` (`python-docx`) с сохранением таблиц.
- `chunker/splitter.py` — нарезка текста на фрагменты по разделам и пунктам (`1.1.`, `4.2.1` и т.д.) с лимитом `max_chars=2500`.
- `verifier/matcher.py` — двухуровневая сверка цитат (`exact_quote`) с исходным текстом фрагмента (точное совпадение + нормализация пробелов, тире и переносов строк). Отбрасывает галлюцинации модели.
- `test_parser.py` — CLI-утилита только для парсинга и нарезки документа в `chunks.json`.
- `run_analysis.py` — полный конвейер анализа документа и сборки отчета по схеме `docs/contracts.md`.

## Установка
```bash
pip install -r requirements.txt
```

## Запуск полного конвейера с верификацией цитат
Оффлайн-проверка (на эталонных аннотациях с проверкой отсева галлюцинаций):
```bash
python run_analysis.py --file=../ml/dataset/01_delayed_payment.docx --law=223-ФЗ --mock-llm --out=result.json
```

Запуск с локальной моделью Ollama (`qwen2.5:7b`):
```bash
python run_analysis.py --file=../ml/dataset/01_delayed_payment.docx --law=223-ФЗ --out=result.json
```