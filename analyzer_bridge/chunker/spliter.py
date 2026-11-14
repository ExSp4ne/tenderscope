"""Нарезка текста контракта на смысловые фрагменты по разделам и пунктам без разрыва предложений."""
import re

MIN_CHUNK_CHARS = 800
MAX_CHUNK_CHARS = 2000
DEFAULT_SECTION = "Общие положения"
DEFAULT_CLAUSE = "Преамбула"

SECTION_RE = re.compile(
    r"^(?:(?:Раздел|РАЗДЕЛ|Статья|СТАТЬЯ|Глава|ГЛАВА|Приложение|ПРИЛОЖЕНИЕ)"
    r"\s*[№#]?\s*[\dIVXLCDM]+|[IVXLCDM]+\.\s+[А-ЯЁA-Z]).*"
)
CLAUSE_RE = re.compile(r"^(\d+(?:\.\d+)+)\.?(?:\s+|$)")
SENTENCE_RE = re.compile(
    r"(?<!\bп)(?<!\bг)(?<!\bч)(?<!\bст)(?<!\bул)(?<!\bд)(?<!\bруб)(?<!\bкоп)"
    r"(?<=[.!?])\s+(?=[А-ЯЁA-Z0-9«\"(\[])"
)


def split_sentences(text):
    sentences = []
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        parts = SENTENCE_RE.split(line)
        sentences.extend(part.strip() for part in parts if part.strip())
    return sentences


def split_long_clause(item, max_chars=MAX_CHUNK_CHARS):
    chunks = []
    buffer = []
    length = 0
    for sentence in split_sentences(item["text"]):
        if buffer and length + len(sentence) + 1 > max_chars:
            chunks.append({
                "page": item["page"],
                "section": item["section"],
                "clause": item["clause"],
                "text": " ".join(buffer),
            })
            buffer = [sentence]
            length = len(sentence)
        else:
            buffer.append(sentence)
            length += len(sentence) + 1
    if buffer:
        chunks.append({
            "page": item["page"],
            "section": item["section"],
            "clause": item["clause"],
            "text": " ".join(buffer),
        })
    return chunks


def extract_clauses(pages):
    items = []
    section = DEFAULT_SECTION
    clause = DEFAULT_CLAUSE
    start_page = 1
    lines = []

    def flush():
        nonlocal lines
        if not lines:
            return
        text = "\n".join(lines).strip()
        if text:
            items.append({
                "page": start_page,
                "section": section,
                "clause": clause,
                "text": text,
            })
        lines = []

    for page in pages:
        page_number = page["page"]
        for raw_line in page["text"].splitlines():
            line = raw_line.strip()
            if not line:
                continue
            if SECTION_RE.match(line) and len(line) < 160:
                flush()
                section = line
                start_page = page_number
                continue
            match = CLAUSE_RE.match(line)
            if match:
                flush()
                clause = match.group(1)
                start_page = page_number
                lines.append(line)
                continue
            if not lines:
                start_page = page_number
            lines.append(line)

    flush()
    return items


def split_into_chunks(pages, min_chars=MIN_CHUNK_CHARS, max_chars=MAX_CHUNK_CHARS):
    items = []
    for item in extract_clauses(pages):
        if len(item["text"]) > max_chars:
            items.extend(split_long_clause(item, max_chars))
        else:
            items.append(item)

    chunks = []
    buffer = []
    length = 0
    chunk_page = 1
    chunk_section = DEFAULT_SECTION
    chunk_clause = DEFAULT_CLAUSE

    def flush():
        nonlocal buffer, length
        if not buffer:
            return
        text = "\n".join(buffer).strip()
        if (
            len(text) < min_chars
            and chunks
            and chunks[-1]["section"] == chunk_section
            and len(chunks[-1]["text"]) + len(text) + 1 <= max_chars
        ):
            chunks[-1]["text"] += "\n" + text
        else:
            chunks.append({
                "page": chunk_page,
                "section": chunk_section,
                "clause": chunk_clause,
                "text": text,
            })
        buffer = []
        length = 0

    for item in items:
        item_len = len(item["text"])
        if buffer and item["section"] != chunk_section and length >= min_chars:
            flush()
        if buffer and length + item_len + 1 > max_chars:
            flush()
        if not buffer:
            chunk_page = item["page"]
            chunk_section = item["section"]
            chunk_clause = item["clause"]
        buffer.append(item["text"])
        length += item_len + 1
        if length >= min_chars:
            flush()

    flush()
    return chunks