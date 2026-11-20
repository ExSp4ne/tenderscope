import re
import sys

HYPHEN_BREAK_RE = re.compile(r"([А-ЯЁа-яёA-Za-z])-\s*\n\s*([А-ЯЁа-яёA-Za-z])")
DASH_RE = re.compile(r"[–—−]")
SPACE_RE = re.compile(r"\s+")

VALID_CATEGORIES = {"payment", "fines", "licenses", "delivery", "termination"}
VALID_LEVELS = {"red", "yellow", "green"}


def normalize_for_match(text: str) -> str:
    if not text:
        return ""
    text = text.replace("\xa0", " ").replace("\u200b", "")
    text = HYPHEN_BREAK_RE.sub(r"\1\2", text)
    text = DASH_RE.sub("-", text)
    return SPACE_RE.sub(" ", text).strip()


def verify_quote(quote: str, chunk_text: str) -> bool:
    if not isinstance(quote, str) or not isinstance(chunk_text, str):
        return False

    clean_quote = quote.strip()
    if not clean_quote or not chunk_text.strip():
        return False

    if clean_quote in chunk_text:
        return True

    norm_quote = normalize_for_match(clean_quote)
    norm_chunk = normalize_for_match(chunk_text)
    return bool(norm_quote) and norm_quote in norm_chunk


def verify_and_format_risk(raw_risk: dict, chunk: dict, risk_index: int) -> dict | None:
    if not isinstance(raw_risk, dict):
        return None

    category = raw_risk.get("category")
    level = raw_risk.get("level")
    if category not in VALID_CATEGORIES or level not in VALID_LEVELS:
        print(f"[verifier] Пропущен риск с некорректными category/level: {raw_risk}", file=sys.stderr)
        return None

    quote = str(raw_risk.get("exact_quote", "")).strip()
    if not verify_quote(quote, chunk.get("text", "")):
        print(
            f"[verifier] Отброшена неподтвержденная цитата (стр. {chunk.get('page')}, п. {chunk.get('clause')}): {quote!r}",
            file=sys.stderr,
        )
        return None

    return {
        "id": f"r-{risk_index}",
        "category": category,
        "level": level,
        "title": str(raw_risk.get("title", "")).strip(),
        "description": str(raw_risk.get("description", "")).strip(),
        "recommendation": str(raw_risk.get("recommendation", "")).strip(),
        "quote": quote,
        "pageNumber": int(chunk["page"]),
        "clauseNumber": str(chunk["clause"]),
    }


def build_summary(risks: list[dict]) -> dict:
    red_count = sum(1 for r in risks if r.get("level") == "red")
    yellow_count = sum(1 for r in risks if r.get("level") == "yellow")
    green_count = sum(1 for r in risks if r.get("level") == "green")

    if red_count > 0:
        verdict = "Высокий риск: обнаружены кабальные условия"
    elif yellow_count > 0:
        verdict = "Умеренный риск: отдельные условия требуют проверки"
    else:
        verdict = "Низкий риск: существенных нарушений не выявлено"

    return {
        "redCount": red_count,
        "yellowCount": yellow_count,
        "greenCount": green_count,
        "verdict": verdict,
    }