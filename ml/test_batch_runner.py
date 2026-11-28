import json
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from ml.evaluate_dataset import score_document
from ml.inference import analyzer


QUOTE = "Покупатель оплачивает поставленный товар в течение 90 дней после подписания акта."
CHUNK = {"page": 3, "section": "Оплата", "clause": "4.1", "text": QUOTE}
CARD = {"category": "payment", "level": "red", "title": "Отсрочка оплаты", "description": "Риск.",
        "recommendation": "Проверить срок.", "exact_quote": QUOTE}


class BatchTests(unittest.TestCase):
    def test_filter_boundaries_and_mixed_text(self):
        self.assertFalse(analyzer.should_analyze("я" * 59))
        self.assertTrue(analyzer.should_analyze("я" * 60))
        footer = "Страница 12 из 45\n" * 4
        signature = "Реквизиты и подписи сторон\nЗаказчик: __________ | ИНН 1234567890; КПП 987654321"
        for text in (None, " " * 100, footer, signature):
            self.assertFalse(analyzer.should_analyze(text))
        self.assertTrue(analyzer.should_analyze(signature + "\n" + QUOTE))
        form = "Директор УФПС Краснодарского ______ края (должность) ____/Е.В. Бабак/ (подпись, фамилия и инициалы)"
        self.assertFalse(analyzer.should_analyze(form))
        self.assertTrue(analyzer.should_analyze(form + " " + QUOTE))

    @patch.object(analyzer, "query_model")
    def test_retry_skip_and_continue(self, query):
        valid = json.dumps(CARD)
        query.side_effect = [None, "[]", "```json", valid,
                             json.dumps({**CARD, "level": "green", "exact_quote": ""})]
        with self.assertLogs(analyzer.LOGGER, level="INFO") as logs:
            cards = analyzer.analyze_chunks([{"text": ""}, CHUNK, CHUNK, CHUNK], "223-ФЗ")
        self.assertIn("отсеяно фильтром: 1", logs.output[0])
        self.assertIn("[3/3] чанк 4", logs.output[-1])
        self.assertIn("red=1 yellow=0 green=1 errors=1", logs.output[-1])
        self.assertEqual(cards, [{**CARD, "page": 3, "section": "Оплата", "clause": "4.1", "chunk_index": 2}])
        self.assertEqual(query.call_count, 5)
        prompts = [call.args[0] for call in query.call_args_list]
        self.assertEqual(prompts[1], prompts[0] + analyzer.RETRY_NOTE)
        self.assertEqual(prompts[2], prompts[0])
        self.assertTrue(all(call.args[1:] == ("223-ФЗ", QUOTE) for call in query.call_args_list))

    def test_metrics_do_not_normalize_exact_quotes_or_double_count(self):
        source = {**CHUNK, "text": QUOTE.replace("90 дней", "90\nдней")}
        prediction = {**CARD, "exact_quote": source["text"], "chunk_index": 0}
        matches, literal = score_document([source], [prediction], [CARD, CARD])
        self.assertEqual((matches, literal), (["found", "missed"], 1))
        matches, literal = score_document([CHUNK], [{**prediction, "exact_quote": QUOTE}], [CARD])
        self.assertEqual((matches, literal), (["exact"], 1))
        matches, literal = score_document([CHUNK], [{**prediction, "exact_quote": "Другая цитата"}], [CARD])
        self.assertEqual((matches, literal), (["missed"], 0))

    def test_risk_card_requires_quote(self):
        for level in ("yellow", "red"):
            with self.subTest(level=level), self.assertRaisesRegex(ValueError, "Empty risk field: exact_quote"):
                analyzer.parse_card(json.dumps({**CARD, "level": level, "exact_quote": ""}))

    def test_parse_card_unwraps_markdown_fence(self):
        payload = json.dumps(CARD)
        self.assertEqual(analyzer.parse_card(f" \r\n```json\r\n{payload}\r\n```\n"), CARD)
        with self.assertRaises(ValueError):
            analyzer.parse_card(f"```json\n{payload}\n```\nExtra text")

    @patch.object(analyzer, "query_model", return_value=json.dumps(CARD))
    def test_analyzer_recovers_literal_source_quote(self, query):
        source = QUOTE.replace("90 дней", "90\nдней").replace("после подписания", "после  подписания")
        cards = analyzer.analyze_chunks([{**CHUNK, "text": source}], "223-ФЗ")
        self.assertEqual(cards[0]["exact_quote"], source)
        query.assert_called_once()


if __name__ == "__main__":
    unittest.main(module=__name__)
