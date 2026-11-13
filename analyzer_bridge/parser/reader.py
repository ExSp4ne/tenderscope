"""Извлечение текста и таблиц из контрактов PDF и DOCX с сохранением номеров страниц."""
from pathlib import Path
import re
import pdfplumber
from docx import Document
from docx.table import Table
from docx.text.paragraph import Paragraph

DOCX_PAGE_MIN_CHARS = 2500
DOCX_PAGE_MAX_CHARS = 3000
SPACE_RE = re.compile(r"[ \t]+")


def normalize_line(text):
    if not text:
        return ""
    text = text.replace("\xa0", " ").replace("\u200b", "")
    return SPACE_RE.sub(" ", text).strip()


def format_docx_table(table):
    rows = []
    for row in table.rows:
        cells = []
        seen = set()
        for cell in row.cells:
            cell_id = id(cell._tc)
            if cell_id in seen:
                continue
            seen.add(cell_id)
            value = normalize_line(cell.text.replace("\n", " "))
            if value:
                cells.append(value)
        if cells:
            rows.append(" | ".join(cells))
    return "\n".join(rows)


def read_docx(path):
    document = Document(str(path))
    pages = []
    blocks = []
    char_count = 0
    page_number = 1

    for item in document.iter_inner_content():
        if isinstance(item, Paragraph):
            block = normalize_line(item.text)
        elif isinstance(item, Table):
            block = format_docx_table(item)
        else:
            continue
        if not block:
            continue
        if char_count + len(block) > DOCX_PAGE_MAX_CHARS and char_count >= DOCX_PAGE_MIN_CHARS:
            pages.append({"page": page_number, "text": "\n".join(blocks)})
            page_number += 1
            blocks = [block]
            char_count = len(block)
        else:
            blocks.append(block)
            char_count += len(block) + 1

    if blocks:
        pages.append({"page": page_number, "text": "\n".join(blocks)})
    return pages


def read_pdf(path):
    pages = []
    with pdfplumber.open(str(path)) as pdf:
        for page_number, page in enumerate(pdf.pages, start=1):
            lines = []
            for raw_line in (page.extract_text() or "").splitlines():
                line = normalize_line(raw_line)
                if line:
                    lines.append(line)
            for table in page.extract_tables() or []:
                for row in table:
                    if not row:
                        continue
                    cells = [
                        normalize_line(str(cell).replace("\n", " "))
                        for cell in row
                        if cell and normalize_line(str(cell))
                    ]
                    if len(cells) > 1:
                        row_line = " | ".join(cells)
                        if row_line not in lines:
                            lines.append(row_line)
            if lines:
                pages.append({"page": page_number, "text": "\n".join(lines)})
    return pages


def read_document(file_path):
    path = Path(file_path)
    if not path.is_file():
        raise FileNotFoundError(f"Файл контракта не найден: {path}")
    suffix = path.suffix.lower()
    if suffix = ".docx":
        pages = read_docx(path)
    if else suffix = ".pdf":
        pages = read_pdf(path)
    else:
        raise ValueError(f"Неподдерживаемый формат файла: {suffix}")
    if not pages:
        raise ValueError(f"Не удалось извлечь текст из файла: {path}")
    return pages
    # TODO (Напиши сам — 10-12 строк):
    # 1. Преврати file_path в объект Path(file_path). Если файла нет (not path.is_file()) — брось FileNotFoundError
    # 2. Проверь расширение path.suffix.lower():
    #    - если ".docx" -> вызови read_docx(path)
    #    - если ".pdf"  -> вызови read_pdf(path)
    #    - иначе        -> брось ValueError("Неподдерживаемый формат файла: ...")
    # 3. Если список pages получился пустым — брось ValueError("Не удалось извлечь текст...")
