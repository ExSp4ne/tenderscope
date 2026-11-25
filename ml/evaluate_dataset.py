"""Run the real chunk analyzer on every annotated document."""
import json
import logging
import sys
from pathlib import Path


ML_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(ML_DIR.parent))

from analyzer_bridge.chunker.splitter import split_into_chunks
from analyzer_bridge.parser.reader import read_document
from analyzer_bridge.verifier.matcher import normalize_for_match
from ml.inference.analyzer import analyze_chunks


DATASET = ML_DIR / "dataset"
SAFE_FILE = "05_safe_standard.docx"
REPORT = ML_DIR / "benchmark_results.md"


def score_document(chunks, cards, annotations):
    sources = [normalize_for_match(chunk["text"]) for chunk in chunks]
    unused = set(range(len(cards)))
    matches = []
    for gold in annotations:
        reference = normalize_for_match(gold["exact_quote"])
        candidates = []
        for index in sorted(unused):
            card = cards[index]
            if (card["category"], card["level"]) != (gold["category"], gold["level"]):
                continue
            quote = normalize_for_match(card["exact_quote"])
            source = sources[card["chunk_index"]]
            if not quote or quote not in source or reference not in source:
                continue
            if reference in quote or (quote in reference and len(quote) * 2 >= len(reference)):
                candidates.append((card["exact_quote"] == gold["exact_quote"], index))
        if candidates:
            exact, index = max(candidates)
            unused.remove(index)
            matches.append("exact" if exact else "found")
        else:
            matches.append("missed")
    literal = sum(card["exact_quote"] in chunks[card["chunk_index"]]["text"] for card in cards)
    return matches, literal


def ratio(numerator, denominator):
    return f"{numerator}/{denominator}" if denominator else "n/a"


def append_document_report(name, chunks, cards, metrics):
    with REPORT.open("a", encoding="utf-8") as report:
        report.write(f"\n## {name}\n\n{metrics}\n\nЧанков: {len(chunks)}. Карточек: {len(cards)}.\n")
        for index, card in enumerate(cards, start=1):
            chunk_index = card["chunk_index"]
            chunk = chunks[chunk_index]
            quote = card["exact_quote"]
            verified = "да" if quote in chunk["text"] else "нет"
            report.write(f"\n### {index}. {card['title']}\n\n")
            report.write(f"- Уровень: `{card['level']}`. Категория: `{card['category']}`.\n")
            report.write(f"- Чанк: {chunk_index + 1}. Страница парсера: {chunk['page']}. Пункт: {chunk['clause']}.\n")
            report.write(f"- Цитата дословно присутствует в чанке: {verified}.\n\n")
            report.write(f"{card['description']}\n\n**Рекомендация.** {card['recommendation']}\n\n**Цитата.**\n\n")
            report.writelines(f"> {line}\n" for line in quote.splitlines())


def main():
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    documents = json.loads((DATASET / "annotations.json").read_text(encoding="utf-8"))["documents"]
    REPORT.write_text("# Карточки рисков по датасету\n\nОтчёт пополняется после каждого документа.\n", encoding="utf-8")
    print(f"Markdown-отчёт: {REPORT}", flush=True)
    total = found = exact = literal = predictions = 0
    safe_fp = None
    for doc in documents:
        name = doc["file"]
        try:
            print(f"{name}: извлекаю текст и разбиваю на чанки", flush=True)
            chunks = split_into_chunks(read_document(DATASET / name))
            if not chunks:
                raise ValueError("Парсер не вернул чанки")
            print(f"{name}: {len(chunks)} chunks", flush=True)
            cards = analyze_chunks(chunks, doc["law"])
        except (OSError, ValueError) as error:
            print(f"{name}: прогон не завершён: {error}", file=sys.stderr)
            return 1
        matches, verified = score_document(chunks, cards, doc["annotations"])
        hits = len(matches) - matches.count("missed")
        metrics = (f"recall={ratio(hits, len(matches))}; exact_quote (gold)={ratio(matches.count('exact'), len(matches))}; "
                   f"exact_quote (chunk)={ratio(verified, len(cards))}")
        print(f"  {metrics}", flush=True)
        if name == SAFE_FILE:
            safe_fp = len(cards)
            print(f"  false positives={safe_fp}", flush=True)
            metrics += f"; false positives={safe_fp}"
        append_document_report(name, chunks, cards, metrics)
        total += len(matches)
        found += hits
        exact += matches.count("exact")
        literal += verified
        predictions += len(cards)
    summary = (f"recall={ratio(found, total)}; false positives ({SAFE_FILE})={safe_fp}; "
               f"exact_quote (gold)={ratio(exact, total)}; exact_quote (chunk)={ratio(literal, predictions)}")
    print(f"Итого: {summary}")
    with REPORT.open("a", encoding="utf-8") as report:
        report.write(f"\n## Итоги прогона\n\n{summary}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
