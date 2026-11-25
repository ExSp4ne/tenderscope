"""Check the raw model response to the delayed-payment sample.

Run: python evaluate_sample.py
Set RISK_MODEL to the installed model name and RISK_API_URL to the full
Chat Completions endpoint. By default, use qwen2.5:7b on local Ollama.
Set RISK_API_KEY if the endpoint requires bearer authentication.
"""

import argparse
import json
import sys
from urllib.error import HTTPError

if __package__:
    from .inference.client import FIELDS, PROMPT_PATH, query_model, unique_keys
else:
    from inference.client import FIELDS, PROMPT_PATH, query_model, unique_keys


QUOTE = "Оплата производится в течение 60 рабочих дней с даты подписания акта."
FRAGMENT = f"4.3. {QUOTE}"


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
            result = validate(query_model(prompt, "44-ФЗ", FRAGMENT))
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
