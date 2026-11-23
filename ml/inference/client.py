import json
import os
from pathlib import Path
from urllib.request import Request, urlopen


PROMPT_PATH = Path(__file__).resolve().parents[1] / "prompts" / "risk_detector.txt"
FIELDS = {"category", "level", "title", "description", "recommendation", "exact_quote"}


def unique_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"Duplicate JSON key: {key}")
        result[key] = value
    return result


def query_model(prompt, law, fragment):
    url = os.getenv("RISK_API_URL", "http://localhost:11434/v1/chat/completions")
    headers = {"Content-Type": "application/json"}
    api_key = os.getenv("RISK_API_KEY")
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    payload = {
        "model": os.getenv("RISK_MODEL", "qwen2.5:7b"),
        "messages": [
            {"role": "system", "content": prompt},
            {"role": "user", "content": json.dumps({"law": law, "fragment": fragment}, ensure_ascii=False)},
        ],
        "temperature": 0,
        "stream": False,
    }
    request = Request(url, data=json.dumps(payload).encode("utf-8"), headers=headers)
    with urlopen(request, timeout=120) as response:
        completion = json.load(response)
    return completion["choices"][0]["message"]["content"]
