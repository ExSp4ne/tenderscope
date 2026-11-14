# analyzer_bridge — парсер и чанкер контрактов

Модуль извлечения текста и таблиц из закупочной документации (`.pdf`, `.docx`) с разбиением на смысловые фрагменты без разрыва предложений.

## Структура

* `parser/reader.py` — постраничное чтение `.pdf` (`pdfplumber`) и `.docx` (`python-docx`), извлечение таблиц с разделителем `|`.
* `chunker/splitter.py` — нарезка текста по разделам и пунктам контракта (800–2000 символов) с привязкой к странице.
* `test_parser.py` — CLI-утилита для локальной проверки и валидации выходного JSON.

## Установка и запуск

```bash
cd analyzer_bridge
pip install -r requirements.txt
python test_parser.py --file=sample_contract.pdf --out=chunks.json