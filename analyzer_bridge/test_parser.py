"""Извлечение текста контракта и нарезка на фрагменты с сохранением в chunks.json.

Запуск: python test_parser.py --file=sample_contract.pdf
"""
import argparse
import json
from pathlib import Path
import sys
import time
from chunker.splitter import split_into_chunks
from parser.reader import read_document

FIELDS = {"page", "section", "clause", "text"}


def validate(chunks):
    if not isinstance(chunks, list) or not chunks:
        raise ValueError("Ожидался непустой список фрагментов")
    for item in chunks:
        if not isinstance(item, dict) or item.keys() != FIELDS:
            raise ValueError("Каждый фрагмент должен содержать поля page, section, clause и text")
        if not isinstance(item["page"], int) or isinstance(item["page"], bool) or item["page"] < 1:
            raise ValueError("Поле page должно быть положительным целым числом")
        for key in ("section", "clause", "text"):
            if not isinstance(item[key], str) or not item[key].strip():
                raise ValueError(f"Поле {key} должно быть непустой строкой")
    return chunks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--file", required=True, help="путь к файлу контракта (.pdf или .docx)")
    parser.add_argument("--out", default="chunks.json", help="путь для сохранения JSON с фрагментами")
    args = parser.parse_args()

    started = time.perf_counter()
    try:
        pages = read_document(args.file)
        chunks = validate(split_into_chunks(pages))
        output_path = Path(args.out)
        output_path.write_text(json.dumps(chunks, ensure_ascii=False, indent=2), encoding="utf-8")
    except (OSError, ValueError) as error:
        print(f"Ошибка парсера: {error}", file=sys.stderr)
        return 1

    elapsed = time.perf_counter() - started
    print(f"Страниц: {len(pages)}, фрагментов: {len(chunks)}, время: {elapsed:.2f} с -> {output_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())