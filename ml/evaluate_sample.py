"""Check the raw model response to the delayed-payment sample.

Run: python evaluate_sample.py
Set RISK_MODEL to the installed model name and RISK_API_URL to the full
Chat Completions endpoint. By default, use qwen2.5:7b on local Ollama.
Set RISK_API_KEY if the endpoint requires bearer authentication.
"""

import argparse
import json
import os
from pathlib import Path
import sys
from urllib.error import HTTPError
from urllib.request import Request, urlopen


PROMPT_PATH = Path(__file__).resolve().parent / "prompts" / "risk_detector.txt"
QUOTE = "Оплата производится в течение 60 рабочих дней с даты подписания акта."
FRAGMENT = f"4.3. {QUOTE}"
FIELDS = {"category", "level", "title", "description", "recommendation", "exact_quote"}


def unique_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"Duplicate JSON key: {key}")
        result[key] = value
    return result


def validate(raw):
    result = json.loads(raw, object_pairs_hook=unique_keys)
    if not isinstance(result, dict) or result.keys() != FIELDS:
        raise ValueError("Expected one JSON object with exactly the six risk fields")
    if any(not isinstance(value, str) or not value.strip() for value in result.values()):
        raise ValueError("Expected nonempty string values in all fields")
    if result["category"] != "payment" or result["level"] != "red":
        raise ValueError("Expected category=payment and level=red")
    if len(result["title"].split()) > 6:
        raise ValueError("Expected a title of at most six words")
    if result["exact_quote"] != QUOTE:
        raise ValueError("exact_quote differs from the expected source sentence")
    return result


def query_model(prompt):
    url = os.getenv("RISK_API_URL", "http://localhost:11434/v1/chat/completions")
    headers = {"Content-Type": "application/json"}
    api_key = os.getenv("RISK_API_KEY")
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    payload = {
        "model": os.getenv("RISK_MODEL", "qwen2.5:7b"),
        "messages": [
            {"role": "system", "content": prompt},
            {
                "role": "user",
                "content": json.dumps({"law": "44-ФЗ", "fragment": FRAGMENT}, ensure_ascii=False),
            },
        ],
        "temperature": 0,
        "stream": False,
    }
    request = Request(url, data=json.dumps(payload).encode("utf-8"), headers=headers)
    with urlopen(request, timeout=120) as response:
        completion = json.load(response)
    return completion["choices"][0]["message"]["content"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runs", type=int, default=1, help="number of independent model calls")
    args = parser.parse_args()
    if args.runs < 1:
        parser.error("--runs must be at least 1")

    try:
        prompt = PROMPT_PATH.read_text(encoding="utf-8")
    except OSError as error:
        print(f"Cannot read the system prompt: {error}", file=sys.stderr)
        return 1

    for run in range(1, args.runs + 1):
        try:
            result = validate(query_model(prompt))
        except HTTPError as error:
            print(f"Run {run}: model endpoint returned HTTP {error.code}", file=sys.stderr)
            return 1
        except (OSError, ValueError, KeyError, IndexError, TypeError) as error:
            print(f"Run {run}: {error}", file=sys.stderr)
            return 1
        print(json.dumps(result, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
