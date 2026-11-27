import json
import logging
import re
from time import perf_counter

from .client import FIELDS, PROMPT_PATH, query_model, unique_keys


LOGGER = logging.getLogger(__name__)
CATEGORIES = {"payment", "fines", "licenses", "delivery", "termination"}
RETRY_NOTE = "\nВерни только чистый JSON: один объект по указанной схеме, без Markdown и пояснений."
TECH_FIELD = re.compile(
    r"[\W\d_]+|(?:стр(?:аница|ан\.)?\.?|лист)\s*\d+(?:\s*(?:из|/)\s*\d+)?|"
    r"(?:(?:государственный )?(?:контракт|договор)|приложение)\s*№\s*[\w/-]+|"
    r"(?:банковские реквизиты|реквизиты и подписи сторон|подписи сторон|м\.\s*п\.)\s*:?|"
    r"(?:инн|кпп|бик|огрн|[рк]/с)\s*[:№]?\s*[\d _-]+|"
    r"(?:заказчик|исполнитель|поставщик|подрядчик|директор|подпись)\s*:?[\s_/.]*|"
    r"[_/\s]+[А-ЯЁ][а-яё-]+\s+[А-ЯЁ]\.\s*[А-ЯЁ]\.[/\s]*",
    re.IGNORECASE,
)
SIGNATURE_LABEL = re.compile(
    r"\((?:должность|подпись(?:, фамилия и инициалы)?|при наличии печати)\)|"
    r"\b[Мм]\.\s*[Пп]\.|\b20__\s*г\.|/[А-ЯЁ]\.\s*[А-ЯЁ]\.\s*[А-ЯЁ][а-яё-]+/|"
    r"\b(?:[Дд]иректор|[Рр]уководитель)(?:\s+[А-ЯЁ][А-Яа-яЁё-]+){0,4}(?=\s*_{2,})|"
    r"(?<=_)\s*(?:края|области|филиала)\b"
)


def should_analyze(text):
    if not isinstance(text, str) or len(text.strip()) < 60:
        return False
    if "__" in text:
        text = SIGNATURE_LABEL.sub(" ", text)
    fields = (field.strip() for field in re.split(r"[\n|;]", text))
    return any(field and not TECH_FIELD.fullmatch(field) for field in fields)


def parse_card(raw):
    card = json.loads(raw, object_pairs_hook=unique_keys)
    if not isinstance(card, dict) or card.keys() != FIELDS:
        raise ValueError("Expected one risk object with six fields")
    for field, value in card.items():
        if not isinstance(value, str):
            raise ValueError(f"Non-string risk field: {field}")
        if not value.strip() and not (field == "exact_quote" and card["level"] == "green"):
            raise ValueError(f"Empty risk field: {field}")
    if card["category"] not in CATEGORIES or card["level"] not in {"green", "red", "yellow"}:
        raise ValueError("Unknown category or level")
    return card


def analyze_chunks(chunks: list[dict], law: str) -> list[dict]:
    prompt = PROMPT_PATH.read_text(encoding="utf-8")
    risks = []
    eligible = [(index, chunk) for index, chunk in enumerate(chunks) if should_analyze(chunk.get("text"))]
    LOGGER.info("К анализу: %s из %s чанков, отсеяно фильтром: %s",
                len(eligible), len(chunks), len(chunks) - len(eligible))
    counts = {"red": 0, "yellow": 0, "green": 0, "error": 0}
    for position, (index, chunk) in enumerate(eligible, start=1):
        started = perf_counter()
        outcome = "error"
        detail = "пропущен после двух невалидных ответов"
        for attempt in range(2):
            try:
                card = parse_card(query_model(prompt + (RETRY_NOTE if attempt else ""), law, chunk["text"]))
            except (ValueError, TypeError, KeyError, IndexError) as error:
                LOGGER.warning("Chunk %s (page %s): %s: %s", index, chunk.get("page"),
                               "skipping" if attempt else "retrying", error)
                continue
            outcome = card["level"]
            detail = f"{outcome}/{card['category']}" if outcome != "green" else "green, риск не выявлен"
            if outcome != "green":
                location = {key: chunk[key] for key in ("page", "section", "clause") if key in chunk}
                risks.append({**card, **location, "chunk_index": index})
            break
        counts[outcome] += 1
        LOGGER.info("[%s/%s] чанк %s, стр. %s: %s (%.1f с) | red=%s yellow=%s green=%s errors=%s",
                    position, len(eligible), index + 1, chunk.get("page", "–"), detail,
                    perf_counter() - started, counts["red"], counts["yellow"], counts["green"], counts["error"])
    return risks
