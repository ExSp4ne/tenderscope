import argparse
import json
import sys
import urllib.error
import urllib.request
from pathlib import Path

from chunker.splitter import split_into_chunks
from parser.reader import read_document
from verifier.matcher import build_summary, verify_and_format_risk, verify_quote

ROOT_DIR = Path(__file__).resolve().parent.parent
PROMPT_PATH = ROOT_DIR / "ml" / "prompts" / "risk_detector.txt"
ANNOTATIONS_PATH = ROOT_DIR / "ml" / "dataset" / "annotations.json"

OLLAMA_URL = "http://localhost:11434/api/chat"
MODEL_NAME = "qwen2.5:7b"


def call_ollama(system_prompt: str, chunk: dict, law: str) -> list[dict]:
    user_payload = json.dumps(
        {
            "law": law,
            "page": chunk["page"],
            "clause": chunk["clause"],
            "text": chunk["text"],
        },
        ensure_ascii=False,
    )
    body = json.dumps(
        {
            "model": MODEL_NAME,
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_payload},
            ],
            "stream": False,
            "format": "json",
            "options": {"temperature": 0.0},
        }
    ).encode("utf-8")

    req = urllib.request.Request(
        OLLAMA_URL,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=90) as resp:
            data = json.loads(resp.read().decode("utf-8"))
        content = data.get("message", {}).get("content", "").strip()
        parsed = json.loads(content)
        if isinstance(parsed, dict):
            if "risks" in parsed and isinstance(parsed["risks"], list):
                return parsed["risks"]
            return [parsed]
        if isinstance(parsed, list):
            return parsed
    except (urllib.error.URLError, json.JSONDecodeError, KeyError, TimeoutError) as exc:
        print(f"[run_analysis] Ошибка LLM на чанке (стр. {chunk['page']}, п. {chunk['clause']}): {exc}", file=sys.stderr)
    return []


def get_mock_candidates(file_path: Path, chunk: dict) -> list[dict]:
    candidates = []
    if ANNOTATIONS_PATH.exists():
        annotations = json.loads(ANNOTATIONS_PATH.read_text(encoding="utf-8"))
        for doc in annotations.get("documents", []):
            if doc.get("file") == file_path.name:
                for risk in doc.get("annotations", []):
                    risk_clause = str(risk.get("clause", ""))
                    chunk_clause = str(chunk.get("clause", ""))
                    exact_quote = risk.get("exact_quote", "")
                    if (
                        risk_clause == chunk_clause
                        or risk_clause.endswith(f"п. {chunk_clause}")
                        or verify_quote(exact_quote, chunk.get("text", ""))
                    ):
                        candidates.append(
                            {
                                "category": risk.get("category"),
                                "level": risk.get("level"),
                                "title": risk.get("title", f"Риск по пункту {risk_clause or chunk_clause}"),
                                "description": risk.get("description", doc.get("source_note", "Обнаружено потенциально опасное условие в проекте договора.")),
                                "recommendation": risk.get("recommendation", "Направить заказчику запрос на разъяснение положений документации и приведение пункта в соответствие с законом."),
                                "exact_quote": exact_quote,
                                "page": chunk["page"],
                                "clause": chunk["clause"],
                            }
                        )

    if chunk.get("page") == 1 and not candidates:
        candidates.append(
            {
                "category": "payment",
                "level": "red",
                "title": "Тестовая галлюцинация модели",
                "description": "Проверка того, что верификатор отбрасывает выдуманные цитаты.",
                "recommendation": "Не должно попасть в итоговый JSON.",
                "exact_quote": "Исполнитель обязан выплатить Заказчику штраф в размере 999% стоимости.",
                "page": chunk["page"],
                "clause": chunk["clause"],
            }
        )
    return candidates


def run_pipeline(
    file_path: str,
    law: str,
    use_mock_llm: bool = False,
    audit_id: str = "audit-local",
    tender_number: str = "0000000000000000000",
    title: str = "Проект государственного контракта",
    customer: str = "Государственный заказчик",
    price: float = 0.0,
) -> dict:
    path = Path(file_path).resolve()
    pages = read_document(str(path))
    chunks = split_into_chunks(pages)

    system_prompt = ""
    if not use_mock_llm and PROMPT_PATH.exists():
        system_prompt = PROMPT_PATH.read_text(encoding="utf-8")

    verified_risks: list[dict] = []
    seen_quotes: set[tuple[str, str]] = set()

    for chunk in chunks:
        if len(chunk.get("text", "").strip()) < 40:
            continue

        if use_mock_llm:
            raw_risks = get_mock_candidates(path, chunk)
        else:
            raw_risks = call_ollama(system_prompt, chunk, law)

        for raw in raw_risks:
            formatted = verify_and_format_risk(raw, chunk, len(verified_risks) + 1)
            if formatted is None:
                continue
            dedup_key = (formatted["clauseNumber"], formatted["quote"])
            if dedup_key in seen_quotes:
                continue
            seen_quotes.add(dedup_key)
            verified_risks.append(formatted)

    return {
        "id": audit_id,
        "tenderNumber": tender_number,
        "title": title,
        "customer": customer,
        "price": price,
        "lawType": law,
        "status": "completed",
        "summary": build_summary(verified_risks),
        "risks": verified_risks,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Сквозной анализ контракта с верификацией цитат")
    parser.add_argument("--file", required=True, help="Путь к .pdf или .docx файлу контракта")
    parser.add_argument("--law", default="44-ФЗ", choices=["44-ФЗ", "223-ФЗ"], help="Закон закупки (44-ФЗ или 223-ФЗ)")
    parser.add_argument("--out", required=True, help="Путь к выходному JSON-файлу")
    parser.add_argument("--mock-llm", action="store_true", help="Использовать эталонные ответы вместо вызова Ollama")
    parser.add_argument("--id", default="audit-local", help="ID проверки")
    parser.add_argument("--tender-number", default="0000000000000000000", help="Номер закупки")
    parser.add_argument("--title", default="Проект контракта", help="Название закупки")
    parser.add_argument("--customer", default="Заказчик", help="Наименование заказчика")
    parser.add_argument("--price", type=float, default=0.0, help="Начальная цена контракта")
    args = parser.parse_args()

    try:
        result = run_pipeline(
            file_path=args.file,
            law=args.law,
            use_mock_llm=args.mock_llm,
            audit_id=args.id,
            tender_number=args.tender_number,
            title=args.title,
            customer=args.customer,
            price=args.price,
        )
    except Exception as exc:
        print(f"Ошибка анализа документа: {exc}", file=sys.stderr)
        return 1

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    print(
        f"Готово. Подтверждено рисков: {len(result['risks'])} "
        f"(red={result['summary']['redCount']}, yellow={result['summary']['yellowCount']}, green={result['summary']['greenCount']}). "
        f"Результат: {out_path}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())